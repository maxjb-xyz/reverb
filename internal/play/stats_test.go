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

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

func TestTimeline_BucketsByDay(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Three plays on 3 distinct UTC days.
	// Day 1: 2000-01-01 00:00:00 UTC = 946684800
	// Day 2: 2000-01-02 00:00:00 UTC = 946771200
	// Day 3: 2000-01-03 00:00:00 UTC = 946857600
	day1 := int64(946684800)
	day2 := int64(946771200)
	day3 := int64(946857600)

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: day1 + 3600,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T2", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 50000, PlayedAt: day2 + 7200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T3", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 75000, PlayedAt: day3 + 1800,
	}); err != nil {
		t.Fatal(err)
	}

	// from/to window covers all 3 plays
	from := day1
	to := day3 + 86400

	buckets, err := stats.Timeline(ctx, "u1", from, to, "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 3 {
		t.Fatalf("want 3 day buckets got %d: %+v", len(buckets), buckets)
	}
	// Buckets must be ascending by Start
	if buckets[0].Start > buckets[1].Start || buckets[1].Start > buckets[2].Start {
		t.Errorf("buckets not ascending: %+v", buckets)
	}
	// Each bucket has 1 play
	for i, b := range buckets {
		if b.Plays != 1 {
			t.Errorf("bucket[%d] plays: want 1 got %d", i, b.Plays)
		}
	}
	// Bucket starts must be the day-boundary (UTC midnight)
	if buckets[0].Start != day1 {
		t.Errorf("bucket[0].Start: want %d got %d", day1, buckets[0].Start)
	}
	if buckets[1].Start != day2 {
		t.Errorf("bucket[1].Start: want %d got %d", day2, buckets[1].Start)
	}
	if buckets[2].Start != day3 {
		t.Errorf("bucket[2].Start: want %d got %d", day3, buckets[2].Start)
	}
	// MsPlayed values
	if buckets[0].MsPlayed != 100000 {
		t.Errorf("bucket[0].MsPlayed: want 100000 got %d", buckets[0].MsPlayed)
	}
	if buckets[1].MsPlayed != 50000 {
		t.Errorf("bucket[1].MsPlayed: want 50000 got %d", buckets[1].MsPlayed)
	}
	if buckets[2].MsPlayed != 75000 {
		t.Errorf("bucket[2].MsPlayed: want 75000 got %d", buckets[2].MsPlayed)
	}
}

func TestTimeline_ExcludesOutsideWindow(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	day1 := int64(946684800) // 2000-01-01 UTC
	day2 := int64(946771200) // 2000-01-02 UTC

	// Play inside
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: day1 + 3600,
	}); err != nil {
		t.Fatal(err)
	}
	// Play outside (after window)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T2", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: day2 + 3600,
	}); err != nil {
		t.Fatal(err)
	}

	buckets, err := stats.Timeline(ctx, "u1", day1, day2, "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 {
		t.Fatalf("want 1 bucket (inside window only) got %d", len(buckets))
	}
}

func TestTimeline_WeekBucket(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// 946857600 = 2000-01-03 00:00 UTC (Monday)
	// 947462400 = 2000-01-10 00:00 UTC (next Monday)
	mon1 := int64(946857600)
	mon2 := int64(947462400)

	// 2 plays in week 1, 1 play in week 2
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: mon1 + 3600,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T2", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 80000, PlayedAt: mon1 + 86400,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T3", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 60000, PlayedAt: mon2 + 3600,
	}); err != nil {
		t.Fatal(err)
	}

	buckets, err := stats.Timeline(ctx, "u1", mon1, mon2+86400, "week")
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 2 {
		t.Fatalf("want 2 week buckets got %d", len(buckets))
	}
	if buckets[0].Plays != 2 {
		t.Errorf("week1 plays: want 2 got %d", buckets[0].Plays)
	}
	if buckets[1].Plays != 1 {
		t.Errorf("week2 plays: want 1 got %d", buckets[1].Plays)
	}
	if buckets[0].Start != mon1 {
		t.Errorf("week1 start: want %d got %d", mon1, buckets[0].Start)
	}
}

// ---------------------------------------------------------------------------
// Clock
// ---------------------------------------------------------------------------

// TestClock_TzOffset: a play at UTC 2000-01-06 23:30:00 (Thursday night UTC)
// with tzOffsetMin=+60 shifts to 2000-01-07 00:30:00 local (Friday, hour 0).
//
// UTC epoch for 2000-01-06 23:30:00:
//
//	946000000 was 1999-12-23; let's compute precisely:
//	2000-01-01 00:00:00 UTC = 946684800
//	+ 5 days = 946684800 + 5*86400 = 947116800 = 2000-01-06 00:00:00 UTC
//	+ 23*3600 + 30*60 = 82800 + 1800 = 84600
//	= 947116800 + 84600 = 947201400
//
// With tzOffsetMin=+60 (UTC+1):
//
//	localSec = 947201400 + 60*60 = 947201400 + 3600 = 947205000
//	localDays = 947205000 / 86400 = 10963 (integer division)
//	947205000 % 86400 = 947205000 - 10963*86400 = 947205000 - 947203200 = 1800
//	hour = 1800 / 3600 = 0  ✓ (00:30)
//	weekday: (10963 + 4) % 7 = 10967 % 7 = 10967 - 1566*7 = 10967 - 10962 = 5
//	0=Sun,1=Mon,2=Tue,3=Wed,4=Thu,5=Fri  ✓
func TestClock_TzOffset(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// 2000-01-06 23:30:00 UTC
	playedAt := int64(947201400)

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T1", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 42000, PlayedAt: playedAt,
	}); err != nil {
		t.Fatal(err)
	}

	// Window: slightly before and after the play
	cells, err := stats.Clock(ctx, "u1", playedAt-1, playedAt+1, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 1 {
		t.Fatalf("want 1 non-empty cell got %d: %+v", len(cells), cells)
	}
	c := cells[0]
	// Expected: Friday=5, Hour=0
	if c.Weekday != 5 {
		t.Errorf("weekday: want 5 (Fri) got %d", c.Weekday)
	}
	if c.Hour != 0 {
		t.Errorf("hour: want 0 got %d", c.Hour)
	}
	if c.Plays != 1 {
		t.Errorf("plays: want 1 got %d", c.Plays)
	}
	if c.MsPlayed != 42000 {
		t.Errorf("ms_played: want 42000 got %d", c.MsPlayed)
	}
}

func TestClock_AggregatesGrid(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Two plays at the same local weekday+hour; one at a different cell.
	// 2000-01-03 10:00:00 UTC = 946944000 + 36000 = 946980000 (Monday 10:00 UTC)
	// 2000-01-03 14:00:00 UTC = 946944000 + 50400 = 946994400 (Monday 14:00 UTC)
	// With tzOffsetMin=0 these map to Mon(1),10 and Mon(1),14.
	t1 := int64(946980000)      // Mon 10:00 UTC → with tz=0: weekday=1,hour=10
	t2 := int64(946994400)      // Mon 14:00 UTC → with tz=0: weekday=1,hour=14
	t3 := int64(946980000 + 10) // same cell as t1

	for _, ts := range []int64{t1, t2, t3} {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: "T", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 30000, PlayedAt: ts,
		}); err != nil {
			t.Fatal(err)
		}
	}

	cells, err := stats.Clock(ctx, "u1", t1-1, t2+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Expect 2 distinct cells
	if len(cells) != 2 {
		t.Fatalf("want 2 non-empty cells got %d: %+v", len(cells), cells)
	}

	// Find the cell with 2 plays (hour 10)
	var cell10, cell14 *play.ClockCell
	for i := range cells {
		if cells[i].Hour == 10 {
			cell10 = &cells[i]
		} else if cells[i].Hour == 14 {
			cell14 = &cells[i]
		}
	}
	if cell10 == nil {
		t.Fatal("missing cell for hour 10")
	}
	if cell14 == nil {
		t.Fatal("missing cell for hour 14")
	}
	if cell10.Plays != 2 {
		t.Errorf("hour-10 cell plays: want 2 got %d", cell10.Plays)
	}
	if cell14.Plays != 1 {
		t.Errorf("hour-14 cell plays: want 1 got %d", cell14.Plays)
	}
}

// ---------------------------------------------------------------------------
// Recent
// ---------------------------------------------------------------------------

func TestRecent_NewestFirstWithCursor(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Insert 3 plays at t=100, 200, 300
	for i, ts := range []int64{100, 200, 300} {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: fmt.Sprintf("Track%d", i+1), Artist: "A", Album: "B",
			DurationMs: 100000, MsPlayed: 50000, PlayedAt: ts,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// before=far-future → all 3, newest first
	rows, err := stats.Recent(ctx, "u1", 9999999999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows got %d", len(rows))
	}
	if rows[0].PlayedAt != 300 {
		t.Errorf("first row should be newest (t=300), got played_at=%d", rows[0].PlayedAt)
	}
	if rows[2].PlayedAt != 100 {
		t.Errorf("last row should be oldest (t=100), got played_at=%d", rows[2].PlayedAt)
	}

	// before=200 → only t=100
	rows2, err := stats.Recent(ctx, "u1", 200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 1 {
		t.Fatalf("cursor before=200 → want 1 row got %d", len(rows2))
	}
	if rows2[0].PlayedAt != 100 {
		t.Errorf("cursor result: want played_at=100 got %d", rows2[0].PlayedAt)
	}
}

func TestRecent_LimitRespected(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: fmt.Sprintf("T%d", i), Artist: "A", Album: "B",
			DurationMs: 100000, MsPlayed: 50000, PlayedAt: int64(1000 + i*10),
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := stats.Recent(ctx, "u1", 9999999999, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("limit 3: want 3 rows got %d", len(rows))
	}
}

func TestRecent_FieldsPresent(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "My Song", Artist: "My Artist", Album: "My Album",
		DurationMs: 200000, MsPlayed: 180000, PlayedAt: 5000,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.Recent(ctx, "u1", 9999999999, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("expected 1 row")
	}
	r := rows[0]
	if r.Title != "My Song" {
		t.Errorf("Title: want %q got %q", "My Song", r.Title)
	}
	if r.Artist != "My Artist" {
		t.Errorf("Artist: want %q got %q", "My Artist", r.Artist)
	}
	if r.Album != "My Album" {
		t.Errorf("Album: want %q got %q", "My Album", r.Album)
	}
	if r.CatalogID == "" {
		t.Error("CatalogID must be non-empty")
	}
	if r.PlayedAt != 5000 {
		t.Errorf("PlayedAt: want 5000 got %d", r.PlayedAt)
	}
}

func TestRecent_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "U1Song", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "U2Song", Artist: "A", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 600,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := stats.Recent(ctx, "u1", 9999999999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "U1Song" {
		t.Errorf("u1 Recent should only show u1's plays, got %+v", rows)
	}
}

// ---------------------------------------------------------------------------
// Entity
// ---------------------------------------------------------------------------

func TestEntity_ArtistAggregation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// 3 plays by Artist X, 1 by Artist Y — only X's plays should appear
	for i, ts := range []int64{1100, 1200, 1300} {
		if err := svc.Record(ctx, "u1", play.PlayInput{
			Title: fmt.Sprintf("AX-Track%d", i), Artist: "Artist X", Album: "Album",
			DurationMs: 100000, MsPlayed: int(30000 + i*10000), PlayedAt: ts,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "AY-Track", Artist: "Artist Y", Album: "Album",
		DurationMs: 100000, MsPlayed: 99000, PlayedAt: 1400,
	}); err != nil {
		t.Fatal(err)
	}

	es, err := stats.Entity(ctx, "u1", "artist", "Artist X", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if es.Plays != 3 {
		t.Errorf("artist Plays: want 3 got %d", es.Plays)
	}
	wantMs := int64(30000 + 40000 + 50000)
	if es.MsPlayed != wantMs {
		t.Errorf("artist MsPlayed: want %d got %d", wantMs, es.MsPlayed)
	}
	if es.FirstPlayed != 1100 {
		t.Errorf("FirstPlayed: want 1100 got %d", es.FirstPlayed)
	}
	if es.LastPlayed != 1300 {
		t.Errorf("LastPlayed: want 1300 got %d", es.LastPlayed)
	}
	// TopTracks should have Artist X's 3 tracks (limit 5)
	if len(es.TopTracks) == 0 {
		t.Error("TopTracks must be non-empty for artist entity")
	}
	for _, tr := range es.TopTracks {
		if tr.Artist != "Artist X" {
			t.Errorf("TopTracks contain non-Artist-X track: %+v", tr)
		}
	}
}

func TestEntity_TrackAggregation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Same track played twice
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "My Song", Artist: "Artist X", Album: "Album",
		DurationMs: 100000, MsPlayed: 80000, PlayedAt: 1100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "My Song", Artist: "Artist X", Album: "Album",
		DurationMs: 100000, MsPlayed: 90000, PlayedAt: 1200,
	}); err != nil {
		t.Fatal(err)
	}

	// Get the catalog_id by looking at what recent returns
	recent, err := stats.Recent(ctx, "u1", 9999999999, 1)
	if err != nil || len(recent) == 0 {
		t.Fatal("need recent to get catalog_id")
	}
	catalogID := recent[0].CatalogID

	es, err := stats.Entity(ctx, "u1", "track", catalogID, winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if es.Plays != 2 {
		t.Errorf("track Plays: want 2 got %d", es.Plays)
	}
	if es.MsPlayed != 80000+90000 {
		t.Errorf("track MsPlayed: want %d got %d", 80000+90000, es.MsPlayed)
	}
	if es.FirstPlayed != 1100 {
		t.Errorf("FirstPlayed: want 1100 got %d", es.FirstPlayed)
	}
	if es.LastPlayed != 1200 {
		t.Errorf("LastPlayed: want 1200 got %d", es.LastPlayed)
	}
	if len(es.TopTracks) != 1 {
		t.Errorf("track entity TopTracks: want 1 (itself) got %d", len(es.TopTracks))
	}
}

func TestEntity_WindowExclusion(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	// Play outside window (before)
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T", Artist: "Artist X", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 999,
	}); err != nil {
		t.Fatal(err)
	}
	// Play inside window
	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T", Artist: "Artist X", Album: "B", DurationMs: 100000, MsPlayed: 50000, PlayedAt: 1500,
	}); err != nil {
		t.Fatal(err)
	}

	es, err := stats.Entity(ctx, "u1", "artist", "Artist X", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if es.Plays != 1 {
		t.Errorf("window exclusion: want 1 play got %d", es.Plays)
	}
	if es.MsPlayed != 50000 {
		t.Errorf("window exclusion: want ms_played=50000 got %d", es.MsPlayed)
	}
}

func TestEntity_PerUserIsolation(t *testing.T) {
	stats, svc := newTestStatsHarness(t)
	ctx := context.Background()

	if err := svc.Record(ctx, "u1", play.PlayInput{
		Title: "T", Artist: "Artist X", Album: "B", DurationMs: 100000, MsPlayed: 100000, PlayedAt: 1500,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(ctx, "u2", play.PlayInput{
		Title: "T", Artist: "Artist X", Album: "B", DurationMs: 100000, MsPlayed: 200000, PlayedAt: 1600,
	}); err != nil {
		t.Fatal(err)
	}

	es, err := stats.Entity(ctx, "u1", "artist", "Artist X", winFrom, winTo)
	if err != nil {
		t.Fatal(err)
	}
	if es.Plays != 1 {
		t.Errorf("user isolation: want 1 play for u1 got %d", es.Plays)
	}
	if es.MsPlayed != 100000 {
		t.Errorf("user isolation: want ms=100000 got %d", es.MsPlayed)
	}
}
