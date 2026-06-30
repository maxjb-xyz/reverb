package play_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// newTestStatsHarness opens a real sqlite store, migrates it, and returns a
// *play.Stats and a *play.Service so tests can insert plays via the service.
func newTestStatsHarness(t *testing.T) (*play.Stats, *play.Service) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/stats.db")
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
	stats := play.NewStats(q)
	return stats, svc
}

// window used across most tests: [1000, 2000)
const (
	winFrom int64 = 1000
	winTo   int64 = 2000
)

func TestStatsSummary_BasicCounts(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Insert 3 plays: 2 same track, 1 different artist+album
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track A", Artist: "Artist X", Album: "Album 1",
		DurationMs: 200000, MsPlayed: 180000, Completed: true, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track A", Artist: "Artist X", Album: "Album 1",
		DurationMs: 200000, MsPlayed: 180000, Completed: true, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track B", Artist: "Artist Y", Album: "Album 2",
		DurationMs: 150000, MsPlayed: 150000, Completed: true, PlayedAt: 1300,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := stats.Summary(ctx, "u1", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}

	if got.Plays != 3 {
		t.Errorf("Plays: want 3 got %d", got.Plays)
	}
	if got.DistinctTracks != 2 {
		t.Errorf("DistinctTracks: want 2 got %d", got.DistinctTracks)
	}
	if got.DistinctArtists != 2 {
		t.Errorf("DistinctArtists: want 2 got %d", got.DistinctArtists)
	}
	if got.DistinctAlbums != 2 {
		t.Errorf("DistinctAlbums: want 2 got %d", got.DistinctAlbums)
	}
	wantMs := int64(180000 + 180000 + 150000)
	if got.MsPlayed != wantMs {
		t.Errorf("MsPlayed: want %d got %d", wantMs, got.MsPlayed)
	}
}

func TestStatsSummary_EmptyWindow(t *testing.T) {
	stats, _ := newTestStatsHarness(t)
	ctx := context.Background()

	// No plays at all — should return zeros, no error
	got, err := stats.Summary(ctx, "u1", winFrom, winTo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Plays != 0 || got.DistinctTracks != 0 || got.DistinctArtists != 0 || got.DistinctAlbums != 0 || got.MsPlayed != 0 {
		t.Errorf("empty window should yield zeros, got %+v", got)
	}
}

func TestStatsSummary_ExcludesOutsideWindow(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Play before window (excluded)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Before", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 999,
	}); err != nil {
		t.Fatal(err)
	}
	// Play inside window (included)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Inside", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	// Play at upper bound (excluded — exclusive to)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "AtBound", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 2000,
	}); err != nil {
		t.Fatal(err)
	}
	// Play after window (excluded)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "After", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := stats.Summary(ctx, "u1", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plays != 1 {
		t.Errorf("Plays: want 1 (only inside-window play) got %d", got.Plays)
	}
}

func TestStatsSummary_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// user1 has 2 plays
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "U1Track", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "U1Track2", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}
	// user2 also has plays in window
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "U2Track", Artist: "B", Album: "C", DurationMs: 200000, MsPlayed: 200000, PlayedAt: 1500,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := stats.Summary(ctx, "u1", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plays != 2 {
		t.Errorf("u1 Plays: want 2 got %d (u2 plays must not appear)", got.Plays)
	}
}

func TestStatsTopTracks_OrderingAndLimit(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Track A: played 3 times
	for i := 0; i < 3; i++ {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: "Track A", Artist: "Artist X", Album: "Album 1",
			DurationMs: 200000, MsPlayed: 180000, Completed: true,
			PlayedAt: int64(1100 + i*10),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Track B: played 2 times
	for i := 0; i < 2; i++ {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: "Track B", Artist: "Artist Y", Album: "Album 2",
			DurationMs: 150000, MsPlayed: 150000, Completed: true,
			PlayedAt: int64(1200 + i*10),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Track C: played 1 time
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track C", Artist: "Artist Z", Album: "Album 3",
		DurationMs: 130000, MsPlayed: 130000, Completed: true, PlayedAt: 1300,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopTracks(ctx, "u1", winFrom, winTo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit 2: want 2 rows got %d", len(rows))
	}
	if rows[0].Title != "Track A" {
		t.Errorf("top track should be Track A (3 plays), got %q", rows[0].Title)
	}
	if rows[0].Plays != 3 {
		t.Errorf("Track A plays: want 3 got %d", rows[0].Plays)
	}
	if rows[1].Title != "Track B" {
		t.Errorf("second track should be Track B (2 plays), got %q", rows[1].Title)
	}
	if rows[1].Plays != 2 {
		t.Errorf("Track B plays: want 2 got %d", rows[1].Plays)
	}
}

func TestStatsTopTracks_CatalogIDPresent(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track A", Artist: "Artist X", Album: "Album 1",
		DurationMs: 200000, MsPlayed: 180000, Completed: true, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopTracks(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least 1 row")
	}
	if rows[0].CatalogID == "" {
		t.Error("CatalogID must be non-empty for TopTracks")
	}
}

func TestStatsTopTracks_ExcludesOutsideWindow(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Play outside window
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "OutsideTrack", Artist: "A", Album: "B",
		DurationMs: 100000, MsPlayed: 100000, PlayedAt: 500,
	}); err != nil {
		t.Fatal(err)
	}
	// Play inside window
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "InsideTrack", Artist: "A", Album: "B",
		DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1500,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopTracks(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 track (inside window only), got %d", len(rows))
	}
	if rows[0].Title != "InsideTrack" {
		t.Errorf("want InsideTrack got %q", rows[0].Title)
	}
}

func TestStatsTopTracks_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "U1Track", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "U2Track", Artist: "C", Album: "D", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopTracks(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "U1Track" {
		t.Errorf("u1 TopTracks should only show u1's tracks, got %+v", rows)
	}
}

func TestStatsTopArtists_Grouping(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Artist X has 2 different tracks → 2 plays
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track A", Artist: "Artist X", Album: "Album 1",
		DurationMs: 200000, MsPlayed: 180000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track B", Artist: "Artist X", Album: "Album 2",
		DurationMs: 150000, MsPlayed: 150000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}
	// Artist Y has 1 play
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track C", Artist: "Artist Y", Album: "Album 3",
		DurationMs: 130000, MsPlayed: 130000, PlayedAt: 1300,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopArtists(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 artist rows got %d", len(rows))
	}
	if rows[0].Artist != "Artist X" {
		t.Errorf("top artist should be Artist X, got %q", rows[0].Artist)
	}
	if rows[0].Plays != 2 {
		t.Errorf("Artist X plays: want 2 got %d", rows[0].Plays)
	}
	// CatalogID/Title/Album should be empty for artist grouping
	if rows[0].CatalogID != "" {
		t.Errorf("CatalogID should be empty for TopArtists, got %q", rows[0].CatalogID)
	}
	if rows[0].Title != "" {
		t.Errorf("Title should be empty for TopArtists, got %q", rows[0].Title)
	}
	wantMs := int64(180000 + 150000)
	if rows[0].MsPlayed != wantMs {
		t.Errorf("Artist X MsPlayed: want %d got %d", wantMs, rows[0].MsPlayed)
	}
}

func TestStatsTopAlbums_Grouping(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Album 1 by Artist X: 2 tracks
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track A", Artist: "Artist X", Album: "Album 1",
		DurationMs: 200000, MsPlayed: 180000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track B", Artist: "Artist X", Album: "Album 1",
		DurationMs: 150000, MsPlayed: 150000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}
	// Album 2 by Artist Y: 1 track
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "Track C", Artist: "Artist Y", Album: "Album 2",
		DurationMs: 130000, MsPlayed: 130000, PlayedAt: 1300,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopAlbums(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 album rows got %d", len(rows))
	}
	if rows[0].Album != "Album 1" {
		t.Errorf("top album should be Album 1, got %q", rows[0].Album)
	}
	if rows[0].Artist != "Artist X" {
		t.Errorf("top album artist should be Artist X, got %q", rows[0].Artist)
	}
	if rows[0].Plays != 2 {
		t.Errorf("Album 1 plays: want 2 got %d", rows[0].Plays)
	}
	// CatalogID/Title should be empty for album grouping
	if rows[0].CatalogID != "" {
		t.Errorf("CatalogID should be empty for TopAlbums, got %q", rows[0].CatalogID)
	}
	if rows[0].Title != "" {
		t.Errorf("Title should be empty for TopAlbums, got %q", rows[0].Title)
	}
	wantMs := int64(180000 + 150000)
	if rows[0].MsPlayed != wantMs {
		t.Errorf("Album 1 MsPlayed: want %d got %d", wantMs, rows[0].MsPlayed)
	}
}

func TestStatsTopAlbums_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "A", Album: "U1Album", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "T2", Artist: "B", Album: "U2Album", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopAlbums(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Album != "U1Album" {
		t.Errorf("u1 TopAlbums should only show u1's albums, got %+v", rows)
	}
}

func TestStatsTopArtists_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "U1Artist", Album: "A", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "T2", Artist: "U2Artist", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.TopArtists(ctx, "u1", winFrom, winTo, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Artist != "U1Artist" {
		t.Errorf("u1 TopArtists should only show u1's artists, got %+v", rows)
	}
}
