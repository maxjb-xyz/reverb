package catalog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store"
)

// newTestService opens a migrated temp sqlite store and returns a *Service.
// Mirrors the pattern in internal/notification/service_test.go.
func newTestService(t *testing.T) *Service {
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
	return NewService(st.Q(), func() time.Time { return fixed }, idgen)
}

// newTestServiceWithStore is like newTestService but also returns the store so
// tests can run raw SQL (e.g. count catalog_entity rows).
func newTestServiceWithStore(t *testing.T) (*Service, *store.Store) {
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
	return NewService(st.Q(), func() time.Time { return fixed }, idgen), st
}

func countCatalogEntities(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow("SELECT COUNT(*) FROM catalog_entity").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestLookup_FoundForExistingEntity verifies Lookup resolves an identity that was
// previously minted, returning the same catalog id with found=true.
func TestLookup_FoundForExistingEntity(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	id := Identity{Kind: "track", Title: "Hurt", Artist: "Johnny Cash", Album: "American IV", DurationMs: 218000}

	minted, err := s.CanonicalFor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	got, found, err := s.Lookup(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Lookup: want found=true for an existing entity")
	}
	if got != minted {
		t.Fatalf("Lookup: want %q got %q", minted, got)
	}
}

// TestLookup_NotFoundDoesNotMint is the load-bearing assertion: a novel identity
// must return ("", false, nil) AND must NOT mint a catalog_entity row (lookup-only).
func TestLookup_NotFoundDoesNotMint(t *testing.T) {
	s, st := newTestServiceWithStore(t)
	ctx := context.Background()

	before := countCatalogEntities(t, st)

	got, found, err := s.Lookup(ctx, Identity{
		Kind: "track", Title: "Never Played", Artist: "Nobody", Album: "Void", DurationMs: 123000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("Lookup: want found=false for a novel identity, got id=%q", got)
	}
	if got != "" {
		t.Fatalf("Lookup: want empty id on miss, got %q", got)
	}

	after := countCatalogEntities(t, st)
	if after != before {
		t.Fatalf("Lookup minted an entity: catalog_entity count %d -> %d (must be lookup-only)", before, after)
	}
}

func TestCanonicalFor_MintsAndIsStable(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	id := Identity{Kind: "track", Title: "Hurt", Artist: "Johnny Cash", Album: "American IV", DurationMs: 218000}

	c1, err := s.CanonicalFor(ctx, id)
	if err != nil || c1 == "" {
		t.Fatalf("mint failed: %v", err)
	}
	if got := c1[:4]; got != "trk_" {
		t.Fatalf("track id prefix = %q", got)
	}

	c2, _ := s.CanonicalFor(ctx, id) // same metadata -> same id (norm alias hit)
	if c1 != c2 {
		t.Fatalf("expected stable id, got %s then %s", c1, c2)
	}
}

func TestCanonicalFor_ISRCAndNormConverge(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	base := Identity{Kind: "track", Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 200000}
	withISRC := base
	withISRC.ISRC = "GBAAA0000001"

	c1, _ := s.CanonicalFor(ctx, withISRC) // mints with isrc + norm aliases
	c2, _ := s.CanonicalFor(ctx, base)     // no isrc -> norm alias hit -> SAME entity
	if c1 != c2 {
		t.Fatalf("norm alias should converge: %s vs %s", c1, c2)
	}
}
