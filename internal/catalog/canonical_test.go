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
