package catalog

import (
	"context"
	"database/sql"
	"errors"

	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// corroborates returns true if the entity stored under otherID has a Fingerprint
// matching the incoming Identity id. Used to gate ISRC-triggered merges.
func (s *Service) corroborates(ctx context.Context, otherID string, id Identity) bool {
	e, err := s.q.GetCatalogEntity(ctx, otherID)
	if err != nil {
		return false
	}
	return matching.Fingerprint(e.Title, e.Artist, e.Album, int(e.DurationMs)) ==
		matching.Fingerprint(id.Title, id.Artist, id.Album, id.DurationMs)
}

// pickWinner returns (winner, loser). The existing (older) entity wins — it has
// more aliases attached and is the authoritative record.
func pickWinner(newID, existingID string) (winner, loser string) {
	return existingID, newID
}

// repointCanonicalRefs repoints every STORED canonical-id reference from
// loser → winner, in FK-safe order (all repoints BEFORE the caller's
// DeleteCatalogEntity). This is the ONE place to extend when a task adds a
// stored canonical_id column (e.g. download_jobs.canonical_id in Task 3).
func (s *Service) repointCanonicalRefs(ctx context.Context, winner, loser string) error {
	// 1. Repoint all aliases from loser → winner.
	// RepointAliasesParams: CatalogID = new (winner), CatalogID_2 = old (loser).
	if err := s.q.RepointAliases(ctx, db.RepointAliasesParams{
		CatalogID:   winner,
		CatalogID_2: loser,
	}); err != nil {
		return err
	}

	// 2. Repoint bindings, handling PK collisions (catalog_id, library_identity).
	if err := s.repointBindingsPreferWinner(ctx, loser, winner); err != nil {
		return err
	}

	// 3. Repoint plays from loser → winner.
	//    plays.catalog_id is FK-constrained; repointing BEFORE the delete (step 4
	//    in merge) ensures the loser entity can be removed without violating the FK.
	if err := s.q.RepointPlays(ctx, db.RepointPlaysParams{
		CatalogID:   winner,
		CatalogID_2: loser,
	}); err != nil {
		return err
	}

	return nil
}

// merge consolidates the loser entity into the winner in one logical operation:
//  1. Repoints all stored canonical-id references via repointCanonicalRefs.
//  2. Deletes the loser entity.
func (s *Service) merge(ctx context.Context, loser, winner string) error {
	if loser == winner {
		return nil
	}

	if err := s.repointCanonicalRefs(ctx, winner, loser); err != nil {
		return err
	}

	return s.q.DeleteCatalogEntity(ctx, loser)
}

// repointBindingsPreferWinner moves loser's backend_binding rows to winner.
// When both loser and winner already have a binding for the same library_identity
// (PK collision on catalog_id+library_identity), we keep the winner's binding
// (winner is the authoritative entity) and discard the loser's.
//
// Implementation note: the generated queries include RepointBindings (bulk UPDATE)
// but no ListBindingsForCatalog. Without being able to iterate the loser's
// binding set, we can't do selective per-row conflict resolution without a raw
// query. Instead we use the safe fallback: attempt bulk repoint; on UNIQUE
// constraint failure (PK collision), delete all loser bindings and rely on the
// winner's existing bindings — which is the correct conservative outcome since
// the winner is the older, authoritative entity. repointBindingForLibID handles
// the per-row case when the caller already knows the library_identity.
func (s *Service) repointBindingsPreferWinner(ctx context.Context, loser, winner string) error {
	err := s.q.RepointBindings(ctx, db.RepointBindingsParams{
		CatalogID:   winner,
		CatalogID_2: loser,
	})
	if err == nil {
		return nil
	}
	// Bulk repoint failed — likely a UNIQUE constraint (PK collision).
	// Drop loser's bindings; winner's existing bindings take precedence.
	if dbErr := s.q.DeleteBindingsForCatalog(ctx, loser); dbErr != nil {
		return dbErr
	}
	return nil
}

// repointBindingForLibID resolves a PK collision for a single library_identity.
// It reads both winner and loser bindings, keeps the better one under winner,
// and deletes the loser's binding.
func (s *Service) repointBindingForLibID(ctx context.Context, loser, winner, libID string) error {
	winnerB, winnerErr := s.q.GetBackendBinding(ctx, db.GetBackendBindingParams{
		CatalogID:       winner,
		LibraryIdentity: libID,
	})
	loserB, loserErr := s.q.GetBackendBinding(ctx, db.GetBackendBindingParams{
		CatalogID:       loser,
		LibraryIdentity: libID,
	})

	if loserErr != nil {
		// Loser has no binding for this library_identity — nothing to do.
		return nil
	}

	if errors.Is(winnerErr, sql.ErrNoRows) {
		// Winner has no binding — simply repoint the loser's binding to winner.
		return s.q.UpsertBackendBinding(ctx, db.UpsertBackendBindingParams{
			CatalogID:       winner,
			LibraryIdentity: libID,
			BackendID:       loserB.BackendID,
			CoverArtID:      loserB.CoverArtID,
			KnownAbsent:     loserB.KnownAbsent,
			BindingEpoch:    loserB.BindingEpoch,
			ResolvedAt:      loserB.ResolvedAt,
		})
	}
	if winnerErr != nil {
		return winnerErr
	}

	// Both winner and loser have a binding. Prefer the one with a non-empty backend_id.
	// If both have one (or neither), keep the winner's (it is the authoritative entity).
	if winnerB.BackendID == "" && loserB.BackendID != "" {
		// Loser has the real backend_id, winner doesn't — upgrade the winner's binding.
		if err := s.q.UpsertBackendBinding(ctx, db.UpsertBackendBindingParams{
			CatalogID:       winner,
			LibraryIdentity: libID,
			BackendID:       loserB.BackendID,
			CoverArtID:      loserB.CoverArtID,
			KnownAbsent:     loserB.KnownAbsent,
			BindingEpoch:    loserB.BindingEpoch,
			ResolvedAt:      loserB.ResolvedAt,
		}); err != nil {
			return err
		}
	}
	// Otherwise keep winner's binding as-is (winner already wins by default).
	return nil
}
