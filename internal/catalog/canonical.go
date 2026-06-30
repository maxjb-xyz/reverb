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
			// Found an existing entity via this alias.
			// For ISRC hits, require corroboration before accepting the match:
			// duplicate/re-used ISRCs exist in the wild and must not fuse distinct tracks.
			if a.kind == "isrc" && !s.corroborates(ctx, cid, id) {
				// ISRC hit but metadata disagrees — this is a different track sharing
				// an ISRC. Skip this alias and continue to the next one.
				continue
			}
			// Attach any newly-supplied aliases and return.
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

// Lookup resolves an identity to an existing catalog ID WITHOUT minting. It runs
// the SAME alias lookup as CanonicalFor step 1 (isrc → external → norm, with the
// ISRC-corroboration skip), but on a miss returns ("", false, nil): it never
// inserts a catalog_entity and never writes aliases. Used by read-only callers
// (e.g. per-track play counts) that must not create entities for novel tracks.
func (s *Service) Lookup(ctx context.Context, id Identity) (catalogID string, found bool, err error) {
	for _, a := range aliasesFor(id) {
		cid, err := s.q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{
			AliasKind:  a.kind,
			AliasValue: a.value,
		})
		if err == nil {
			// For ISRC hits, require corroboration: duplicate/re-used ISRCs exist
			// in the wild and must not fuse distinct tracks. Skip on disagreement.
			if a.kind == "isrc" && !s.corroborates(ctx, cid, id) {
				continue
			}
			return cid, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
	}
	return "", false, nil
}

// attachAliases inserts any newly-supplied aliases onto cid; if an alias already
// points at a DIFFERENT entity, that observed collision fires a merge.
// For an isrc collision, the merge is only done if the two entities corroborate
// (same fingerprint). For non-isrc collisions (e.g. norm, external), merge unconditionally.
func (s *Service) attachAliases(ctx context.Context, cid string, id Identity, aliases []aliasKV) (string, error) {
	winner := cid
	now := s.now().Unix()
	for _, a := range aliases {
		existing, err := s.q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{AliasKind: a.kind, AliasValue: a.value})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if err := s.q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{
				AliasKind:  a.kind,
				AliasValue: a.value,
				CatalogID:  winner,
				CreatedAt:  now,
			}); err != nil {
				return "", err
			}
		case err != nil:
			return "", err
		case existing != winner:
			// Collision detected: an alias already points at a different entity.
			// For isrc collisions, corroborate before merging (duplicate ISRCs exist in the wild).
			if a.kind == "isrc" && !s.corroborates(ctx, existing, id) {
				// Duplicate/re-used ISRC across genuinely distinct tracks — do not merge.
				continue
			}
			// Winner = the existing (older) entity.
			w, l := pickWinner(winner, existing)
			if err := s.merge(ctx, l, w); err != nil {
				return "", err
			}
			winner = w
		}
	}
	return winner, nil
}
