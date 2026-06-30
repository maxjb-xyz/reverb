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
