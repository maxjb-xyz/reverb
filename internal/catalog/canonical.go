package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type aliasKV struct{ kind, value string }

// aliasesFor builds the alias list for id in lookup priority order:
// isrc → external → norm (norm is always present).
func aliasesFor(id Identity) []aliasKV {
	var out []aliasKV
	if id.ISRC != "" {
		out = append(out, aliasKV{"isrc", id.ISRC})
	}
	if id.Source != "" && id.ExternalID != "" {
		out = append(out, aliasKV{"external", id.Source + ":" + id.ExternalID})
	}
	out = append(out, aliasKV{"norm", matching.Fingerprint(id.Title, id.Artist, id.Album, id.DurationMs)})
	return out
}

// prefixFor returns the id prefix for a given entity kind.
func prefixFor(kind string) string {
	switch kind {
	case "album":
		return "alb_"
	case "artist":
		return "art_"
	default:
		return "trk_"
	}
}

// CanonicalFor resolves or mints a catalog ID. It is backend-independent
// (pure DB — no live library call). On a hit it calls attachAliases to
// record any newly-supplied aliases. On a miss it mints a new entity and
// writes all aliases.
func (s *Service) CanonicalFor(ctx context.Context, id Identity) (string, error) {
	aliases := aliasesFor(id)

	// 1. Lookup in priority order (isrc → external → norm).
	for _, a := range aliases {
		cid, err := s.q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{
			AliasKind:  a.kind,
			AliasValue: a.value,
		})
		if err == nil {
			// Found an existing entity. Attach any newly-supplied aliases and return.
			return s.attachAliases(ctx, cid, id, aliases)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}

	// 2. Mint a new entity.
	cid := prefixFor(id.Kind) + s.idgen()
	now := s.now().Unix()

	if err := s.q.InsertCatalogEntity(ctx, db.InsertCatalogEntityParams{
		ID:         cid,
		Kind:       id.Kind,
		Title:      id.Title,
		Artist:     id.Artist,
		Album:      id.Album,
		DurationMs: int64(id.DurationMs),
		Isrc:       id.ISRC,
		Mbid:       id.MBID,
		Source:     id.Source,
		ExternalID: id.ExternalID,
		CreatedAt:  now,
	}); err != nil {
		return "", err
	}

	for _, a := range aliases {
		if err := s.q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{
			AliasKind:  a.kind,
			AliasValue: a.value,
			CatalogID:  cid,
			CreatedAt:  now,
		}); err != nil {
			return "", err
		}
	}

	return cid, nil
}

// attachAliases is a stub for Task 3: insert the supplied aliases with
// ON CONFLICT DO NOTHING and return the existing catalog ID unchanged.
// Task 4 replaces the body with collision-detection and merge logic.
func (s *Service) attachAliases(ctx context.Context, cid string, _ Identity, aliases []aliasKV) (string, error) {
	now := s.now().Unix()
	for _, a := range aliases {
		if err := s.q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{
			AliasKind:  a.kind,
			AliasValue: a.value,
			CatalogID:  cid,
			CreatedAt:  now,
		}); err != nil {
			return "", err
		}
	}
	return cid, nil
}
