package play_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// newTestPlayService opens a real in-memory sqlite store, migrates it, and
// returns both a *play.Service and the *db.Queries for reading back state.
func newTestPlayService(t *testing.T) (*play.Service, *db.Queries) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/play.db")
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
	now := func() time.Time { return fixed }

	q := st.Q()
	catalogSvc := catalog.NewService(q, now, idgen)
	svc := play.NewService(q, catalogSvc, now, idgen)
	return svc, q
}

func TestRecord_MintsCatalogAndInsertsPlay(t *testing.T) {
	s, q := newTestPlayService(t)
	ctx := context.Background()

	err := s.Record(ctx, "user-1", play.PlayInput{
		Title:      "Hurt",
		Artist:     "Johnny Cash",
		Album:      "American IV",
		DurationMs: 218000,
		MsPlayed:   140000,
		Completed:  true,
		PlayedAt:   1719000000,
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   "user-1",
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "Hurt" {
		t.Fatalf("play not recorded: %+v", rows)
	}
}

func TestRecord_PerUserScoping(t *testing.T) {
	s, q := newTestPlayService(t)
	ctx := context.Background()

	// user-1 records a play
	if err := s.Record(ctx, "user-1", play.PlayInput{
		Title:    "Hurt",
		Artist:   "Johnny Cash",
		Album:    "American IV",
		DurationMs: 218000,
		MsPlayed: 140000,
		Completed: true,
		PlayedAt: 1719000000,
	}); err != nil {
		t.Fatal(err)
	}

	// user-2 records a different play
	if err := s.Record(ctx, "user-2", play.PlayInput{
		Title:    "Ring of Fire",
		Artist:   "Johnny Cash",
		Album:    "Ring of Fire",
		DurationMs: 157000,
		MsPlayed: 157000,
		Completed: true,
		PlayedAt: 1719001000,
	}); err != nil {
		t.Fatal(err)
	}

	// user-1's recent plays should only contain their own play
	rows, err := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   "user-1",
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 play for user-1, got %d: %+v", len(rows), rows)
	}
	if rows[0].Title != "Hurt" {
		t.Fatalf("expected 'Hurt' for user-1, got %q", rows[0].Title)
	}
}
