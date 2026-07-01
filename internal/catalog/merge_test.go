package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// newTestServiceWithQueries is like newTestService but also returns the
// underlying *db.Queries so tests can call play/stats methods directly.
func newTestServiceWithQueries(t *testing.T) (*Service, *db.Queries) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	var counter int
	idgen := func() string {
		counter++
		return fmt.Sprintf("%08d-0000-0000-0000-000000000000", counter)
	}
	fixed := time.Unix(1_700_000_000, 0)
	q := st.Q()
	return NewService(q, func() time.Time { return fixed }, idgen), q
}

// upsertBindingParams builds UpsertBackendBindingParams for test use.
func upsertBindingParams(catalogID, libID, backendID string, epoch int64) db.UpsertBackendBindingParams {
	return db.UpsertBackendBindingParams{
		CatalogID:       catalogID,
		LibraryIdentity: libID,
		BackendID:       backendID,
		BindingEpoch:    epoch,
	}
}

// getBindingParams builds GetBackendBindingParams for test use.
func getBindingParams(catalogID, libID string) db.GetBackendBindingParams {
	return db.GetBackendBindingParams{
		CatalogID:       catalogID,
		LibraryIdentity: libID,
	}
}

func TestCanonicalFor_MergesWhenISRCArrivesLater(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	// Path A: mint via spotify external id, no ISRC yet.
	a := Identity{Kind: "track", Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 200000, Source: "spotify", ExternalID: "SPOTIFY_A"}
	ca, _ := s.CanonicalFor(ctx, a)
	// Path B: SAME track now carries an ISRC AND a different external id; norm matches A.
	b := a
	b.ISRC = "GBAAA0000001"
	b.ExternalID = "SPOTIFY_B"
	cb, _ := s.CanonicalFor(ctx, b)
	if ca != cb {
		t.Fatalf("expected merge to a single id, got %s vs %s", ca, cb)
	}
	// Re-resolving A's original identity now returns the merged (winner) id.
	again, _ := s.CanonicalFor(ctx, a)
	if again != cb {
		t.Fatalf("post-merge A should resolve to winner: %s vs %s", again, cb)
	}
}

func TestCanonicalFor_NoMergeWhenISRCCollidesButMetadataDisagrees(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	x := Identity{Kind: "track", Title: "Completely Different", Artist: "Other", Album: "X", DurationMs: 100000, ISRC: "GBDUP0000001"}
	cx, _ := s.CanonicalFor(ctx, x)
	y := Identity{Kind: "track", Title: "Unrelated Song", Artist: "Nobody", Album: "Y", DurationMs: 300000, ISRC: "GBDUP0000001"}
	cy, _ := s.CanonicalFor(ctx, y)
	if cx == cy {
		t.Fatal("duplicate ISRC with disagreeing metadata must NOT merge")
	}
}

// TestMerge_BindingCollisionPrefersWinner verifies that when both loser and winner
// have a binding for the same library_identity, the winner's binding is preserved.
func TestMerge_BindingCollisionPrefersWinner(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	// Mint two distinct entities (different titles so norm doesn't collide).
	winner, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Winner Track", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Loser Track", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}

	libID := "navidrome:abc123"

	// Insert winner binding with a real backend_id.
	if err := s.q.UpsertBackendBinding(ctx, upsertBindingParams(winner, libID, "navidrome-song-999", 1_700_000_100)); err != nil {
		t.Fatal(err)
	}
	// Insert loser binding for the same library_identity (PK collision scenario).
	if err := s.q.UpsertBackendBinding(ctx, upsertBindingParams(loser, libID, "navidrome-song-777", 1_700_000_050)); err != nil {
		t.Fatal(err)
	}

	// Merge loser into winner.
	if err := s.merge(ctx, loser, winner); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// After merge, the winner's binding should survive with winner's backend_id.
	b, err := s.q.GetBackendBinding(ctx, getBindingParams(winner, libID))
	if err != nil {
		t.Fatalf("winner binding missing after merge: %v", err)
	}
	if b.BackendID != "navidrome-song-999" {
		t.Fatalf("expected winner's backend_id navidrome-song-999, got %q", b.BackendID)
	}
	// Loser entity should be gone.
	if _, err := s.q.GetCatalogEntity(ctx, loser); err == nil {
		t.Fatal("loser entity should have been deleted")
	}
}

// TestMerge_AllThreeRefTypesConsolidate verifies that a merge repoints all three
// stored canonical-id reference types (alias, backend_binding, play) from loser
// to winner and then deletes the loser entity — end-to-end coverage for Task 2.
func TestMerge_AllThreeRefTypesConsolidate(t *testing.T) {
	s, q := newTestServiceWithQueries(t)
	ctx := context.Background()

	winner, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Winner Song", Artist: "X", Album: "Y", DurationMs: 240000})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Loser Song", Artist: "X", Album: "Y", DurationMs: 240000})
	if err != nil {
		t.Fatal(err)
	}

	// Plant an explicit alias on the loser.
	if err := q.InsertCatalogAlias(ctx, db.InsertCatalogAliasParams{
		AliasKind: "isrc", AliasValue: "TEST123456789", CatalogID: loser, CreatedAt: 1_700_000_000,
	}); err != nil {
		t.Fatalf("InsertCatalogAlias: %v", err)
	}

	// Plant a backend binding on the loser (unique lib_id so no collision).
	libID := "navidrome:all-three-test"
	if err := q.UpsertBackendBinding(ctx, upsertBindingParams(loser, libID, "backend-id-loser", 1_700_000_100)); err != nil {
		t.Fatalf("UpsertBackendBinding: %v", err)
	}

	// Plant a play on the loser.
	const userID = "user-all-three-test"
	if err := q.InsertPlay(ctx, db.InsertPlayParams{
		ID: "play-all-three-0001", UserID: userID, CatalogID: loser,
		PlayedAt: 1_700_000_500, MsPlayed: 240000, Completed: 1, CreatedAt: 1_700_000_500,
	}); err != nil {
		t.Fatalf("InsertPlay: %v", err)
	}

	// Merge.
	if err := s.merge(ctx, loser, winner); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// 1. Alias now points at winner.
	aliasTarget, err := q.GetAliasCatalogID(ctx, db.GetAliasCatalogIDParams{AliasKind: "isrc", AliasValue: "TEST123456789"})
	if err != nil {
		t.Fatalf("GetAliasCatalogID after merge: %v", err)
	}
	if aliasTarget != winner {
		t.Errorf("alias target = %q; want winner %q", aliasTarget, winner)
	}

	// 2. Backend binding now owned by winner.
	b, err := q.GetBackendBinding(ctx, getBindingParams(winner, libID))
	if err != nil {
		t.Fatalf("winner binding missing after merge: %v", err)
	}
	if b.BackendID != "backend-id-loser" {
		t.Errorf("winner binding backend_id = %q; want %q", b.BackendID, "backend-id-loser")
	}

	// 3. Play now references winner.
	plays, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{UserID: userID, PlayedAt: 1_700_001_000, Limit: 10})
	if err != nil {
		t.Fatalf("ListRecentPlays: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play after merge, got %d", len(plays))
	}
	if plays[0].CatalogID != winner {
		t.Errorf("play.catalog_id = %q; want winner %q", plays[0].CatalogID, winner)
	}

	// 4. Loser entity is gone.
	if _, err := q.GetCatalogEntity(ctx, loser); err == nil {
		t.Fatal("loser entity should have been deleted after merge")
	}
}

// TestRepointCanonicalRefs_AllRefsMovedLoserIntact verifies repointCanonicalRefs
// in isolation: it repoints aliases, bindings, and plays from loser to winner
// but does NOT delete the loser — that is the caller's (merge's) responsibility.
// This test would fail to compile before the helper is extracted (method undefined).
func TestRepointCanonicalRefs_AllRefsMovedLoserIntact(t *testing.T) {
	s, q := newTestServiceWithQueries(t)
	ctx := context.Background()

	winner, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Helper Winner", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Helper Loser", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}

	// Plant a play on the loser.
	if err := q.InsertPlay(ctx, db.InsertPlayParams{
		ID: "play-repoint-direct-0001", UserID: "user-repoint-test", CatalogID: loser,
		PlayedAt: 1_700_001_000, MsPlayed: 180000, Completed: 1, CreatedAt: 1_700_001_000,
	}); err != nil {
		t.Fatalf("InsertPlay: %v", err)
	}

	// Call repointCanonicalRefs directly — does NOT delete the loser.
	if err := s.repointCanonicalRefs(ctx, winner, loser); err != nil {
		t.Fatalf("repointCanonicalRefs: %v", err)
	}

	// Play must now reference the winner.
	plays, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID: "user-repoint-test", PlayedAt: 1_700_002_000, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentPlays: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play, got %d", len(plays))
	}
	if plays[0].CatalogID != winner {
		t.Errorf("play.catalog_id = %q; want winner %q", plays[0].CatalogID, winner)
	}

	// Loser entity must still exist (repointCanonicalRefs doesn't delete it).
	if _, err := q.GetCatalogEntity(ctx, loser); err != nil {
		t.Fatalf("loser entity must NOT be deleted by repointCanonicalRefs: %v", err)
	}
}

// TestRepointCanonicalRefs_PlaysRepointedBeforeDelete verifies FK safety:
// a loser with a play can be merged cleanly — plays are repointed BEFORE
// the loser is deleted, so no FK constraint violation occurs.
func TestRepointCanonicalRefs_PlaysRepointedBeforeDelete(t *testing.T) {
	s, q := newTestServiceWithQueries(t)
	ctx := context.Background()

	winner, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "FK Winner", Artist: "C", Album: "D", DurationMs: 120000})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "FK Loser", Artist: "C", Album: "D", DurationMs: 120000})
	if err != nil {
		t.Fatal(err)
	}

	// A play FK-constrains the loser: without repoint-before-delete, the delete
	// would fail (FK violation) and this test would error rather than pass.
	if err := q.InsertPlay(ctx, db.InsertPlayParams{
		ID: "play-fk-safe-0001", UserID: "user-fk-test", CatalogID: loser,
		PlayedAt: 1_700_002_000, MsPlayed: 120000, Completed: 1, CreatedAt: 1_700_002_000,
	}); err != nil {
		t.Fatalf("InsertPlay: %v", err)
	}

	// merge must succeed — repointCanonicalRefs fires first, then DeleteCatalogEntity.
	if err := s.merge(ctx, loser, winner); err != nil {
		t.Fatalf("merge with play on loser must succeed (FK-safe order): %v", err)
	}

	// Sanity: play now points at winner, loser is gone.
	plays, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID: "user-fk-test", PlayedAt: 1_700_003_000, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecentPlays: %v", err)
	}
	if len(plays) != 1 || plays[0].CatalogID != winner {
		t.Fatalf("play not repointed to winner: plays=%v", plays)
	}
	if _, err := q.GetCatalogEntity(ctx, loser); err == nil {
		t.Fatal("loser should be deleted after merge")
	}
}

// TestMerge_RepointsPlays verifies that plays recorded against the loser catalog
// entity are repointed to the winner after a merge, so listening history consolidates
// rather than orphaning or double-counting.
func TestMerge_RepointsPlays(t *testing.T) {
	s, q := newTestServiceWithQueries(t)
	ctx := context.Background()

	// Mint two distinct catalog entities (different titles so their norms don't collide).
	winner, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Winner Song", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.CanonicalFor(ctx, Identity{Kind: "track", Title: "Loser Song", Artist: "A", Album: "B", DurationMs: 180000})
	if err != nil {
		t.Fatal(err)
	}

	// Insert a play row referencing the LOSER's catalog_id BEFORE the merge fires.
	const userID = "user-test-001"
	if err := q.InsertPlay(ctx, db.InsertPlayParams{
		ID:        "play-00000001",
		UserID:    userID,
		CatalogID: loser,
		PlayedAt:  1_700_000_000,
		MsPlayed:  180000,
		Completed: 1,
		CreatedAt: 1_700_000_000,
	}); err != nil {
		t.Fatalf("InsertPlay: %v", err)
	}

	// Merge loser into winner (mirrors TestMerge_BindingCollisionPrefersWinner pattern).
	if err := s.merge(ctx, loser, winner); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The play's catalog_id must now point at the winner, not the (deleted) loser.
	plays, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   userID,
		PlayedAt: 1_700_000_001, // exclusive upper-bound cursor; must be > PlayedAt
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListRecentPlays: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play after merge, got %d", len(plays))
	}
	if plays[0].CatalogID != winner {
		t.Fatalf("play.catalog_id = %q; want winner %q", plays[0].CatalogID, winner)
	}
}
