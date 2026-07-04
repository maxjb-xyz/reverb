package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// statsTestServer builds a Server with real play.Stats + play.Service wired in.
// Returns the server, a session cookie for owner, the owner's user ID, the auth
// service (so tests can create additional users), and the play service for seeding.
func statsTestServer(t *testing.T) (*Server, *http.Cookie, string, *play.Service) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/stats.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	authSvc, tok := seededAuthToken(t, st)

	users, err := authSvc.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least one user (owner)")
	}
	ownerID := users[0].ID

	var counter int
	idgen := func() string {
		counter++
		return fmt.Sprintf("%08d-0000-0000-0000-000000000000", counter)
	}
	q := st.Q()
	catalogSvc := catalog.NewService(q, time.Now, idgen)
	playSvc := play.NewService(q, catalogSvc, time.Now, idgen)
	statsSvc := play.NewStats(q)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Play:       playSvc,
		Stats:      statsSvc,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}, ownerID, playSvc
}

// seedPlay records a single play for userID via the play.Service.
func seedPlay(t *testing.T, svc *play.Service, userID, title, artist, album string, playedAt int64, msPlayed int) {
	t.Helper()
	in := play.PlayInput{
		Title:      title,
		Artist:     artist,
		Album:      album,
		DurationMs: msPlayed,
		MsPlayed:   msPlayed,
		Completed:  true,
		PlayedAt:   playedAt,
	}
	if err := svc.Record(context.Background(), userID, in); err != nil {
		t.Fatalf("seedPlay: %v", err)
	}
}

// --- Summary ---

// TestStatsSummary verifies GET /stats/summary returns aggregate counts.
func TestStatsSummary(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Seed 2 plays for owner.
	seedPlay(t, playSvc, ownerID, "Hurt", "Johnny Cash", "American IV", 1_700_000_000, 218_000)
	seedPlay(t, playSvc, ownerID, "Ring of Fire", "Johnny Cash", "Ring of Fire", 1_700_001_000, 157_000)

	rec := doGET(t, srv, "/api/v1/stats/summary", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/summary = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Plays           int   `json:"Plays"`
		DistinctTracks  int   `json:"DistinctTracks"`
		DistinctArtists int   `json:"DistinctArtists"`
		DistinctAlbums  int   `json:"DistinctAlbums"`
		MsPlayed        int64 `json:"MsPlayed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plays != 2 {
		t.Errorf("Plays = %d, want 2", resp.Plays)
	}
	if resp.DistinctArtists != 1 {
		t.Errorf("DistinctArtists = %d, want 1 (same artist both plays)", resp.DistinctArtists)
	}
}

// TestStatsSummaryWindowExcludes verifies that a narrow from/to window excludes plays outside it.
func TestStatsSummaryWindowExcludes(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// One play at t=100, another at t=9000. Query [500, 10000) → only the second.
	seedPlay(t, playSvc, ownerID, "Early", "Artist A", "Alb", 100, 100_000)
	seedPlay(t, playSvc, ownerID, "Late", "Artist B", "Alb2", 9000, 200_000)

	rec := doGET(t, srv, "/api/v1/stats/summary?from=500&to=10000", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/summary?from=500&to=10000 = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Plays int `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plays != 1 {
		t.Errorf("Plays = %d, want 1 (only the late play in window)", resp.Plays)
	}
}

// TestStatsSummaryPerUserIsolation verifies user-2's plays never appear in user-1's summary.
func TestStatsSummaryPerUserIsolation(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/stats-iso.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	authSvc, ownerTok := seededAuthToken(t, st)
	users, err := authSvc.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ownerID := users[0].ID

	// Create a second user and get a session for them.
	otherID, err := authSvc.CreateUser(context.Background(), "other", "pw123456", "role-requester")
	if err != nil {
		t.Fatal(err)
	}

	var counter int
	idgen := func() string { counter++; return fmt.Sprintf("%08d-0000-0000-0000-000000000000", counter) }
	q := st.Q()
	catalogSvc := catalog.NewService(q, time.Now, idgen)
	playSvc := play.NewService(q, catalogSvc, time.Now, idgen)
	statsSvc := play.NewStats(q)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Play:       playSvc,
		Stats:      statsSvc,
	})

	// Seed 1 play for owner, 3 for otherID.
	seedPlay(t, playSvc, ownerID, "Owner Track", "Artist O", "Album O", 1_000_000, 200_000)
	for i := 0; i < 3; i++ {
		seedPlay(t, playSvc, otherID, "Other Track", "Artist X", "Album X", int64(2_000_000+i), 100_000)
	}

	ownerCookie := &http.Cookie{Name: sessionCookie, Value: ownerTok}
	rec := doGET(t, srv, "/api/v1/stats/summary", ownerCookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/summary = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Plays int `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plays != 1 {
		t.Errorf("Plays = %d, want 1 (only owner's play, not other's 3)", resp.Plays)
	}
}

// --- Top Tracks ---

// TestStatsTopTracks verifies GET /stats/top/tracks returns top tracks ordered by play count.
func TestStatsTopTracks(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Track A played 3x, Track B played 1x.
	for i := 0; i < 3; i++ {
		seedPlay(t, playSvc, ownerID, "Track A", "Artist", "Album", int64(1_000_000+i), 200_000)
	}
	seedPlay(t, playSvc, ownerID, "Track B", "Artist", "Album", 1_001_000, 100_000)

	rec := doGET(t, srv, "/api/v1/stats/top/tracks", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/top/tracks = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Title string `json:"Title"`
		Plays int    `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) < 1 {
		t.Fatal("expected at least 1 top track")
	}
	if resp[0].Title != "Track A" {
		t.Errorf("top track = %q, want %q", resp[0].Title, "Track A")
	}
	if resp[0].Plays != 3 {
		t.Errorf("top track plays = %d, want 3", resp[0].Plays)
	}
}

// TestStatsTopTracksLimitTruncates verifies the limit param truncates results.
func TestStatsTopTracksLimitTruncates(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Seed 5 distinct tracks.
	for i := 0; i < 5; i++ {
		seedPlay(t, playSvc, ownerID, fmt.Sprintf("Track %d", i), "Artist", "Album", int64(1_000_000+i), 100_000)
	}

	rec := doGET(t, srv, "/api/v1/stats/top/tracks?limit=2", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/top/tracks?limit=2 = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Title string `json:"Title"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("got %d tracks with limit=2, want 2", len(resp))
	}
}

// --- Top Artists ---

// TestStatsTopArtists verifies GET /stats/top/artists returns top artists.
func TestStatsTopArtists(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	seedPlay(t, playSvc, ownerID, "Song 1", "Queen", "News", 1_000_000, 200_000)
	seedPlay(t, playSvc, ownerID, "Song 2", "Queen", "Innuendo", 1_000_001, 200_000)
	seedPlay(t, playSvc, ownerID, "Song 3", "Bowie", "Ziggy", 1_000_002, 150_000)

	rec := doGET(t, srv, "/api/v1/stats/top/artists", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/top/artists = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Artist string `json:"Artist"`
		Plays  int    `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) < 1 {
		t.Fatal("expected at least 1 top artist")
	}
	if resp[0].Artist != "Queen" {
		t.Errorf("top artist = %q, want Queen", resp[0].Artist)
	}
}

// --- Top Albums ---

// TestStatsTopAlbums verifies GET /stats/top/albums returns top albums.
func TestStatsTopAlbums(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	seedPlay(t, playSvc, ownerID, "T1", "Artist", "Album A", 1_000_000, 200_000)
	seedPlay(t, playSvc, ownerID, "T2", "Artist", "Album A", 1_000_001, 200_000)
	seedPlay(t, playSvc, ownerID, "T3", "Artist", "Album B", 1_000_002, 100_000)

	rec := doGET(t, srv, "/api/v1/stats/top/albums", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/top/albums = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Album string `json:"Album"`
		Plays int    `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) < 1 {
		t.Fatal("expected at least 1 top album")
	}
	if resp[0].Album != "Album A" {
		t.Errorf("top album = %q, want Album A", resp[0].Album)
	}
}

// --- Timeline ---

// TestStatsTimeline verifies GET /stats/timeline returns bucketed play counts.
func TestStatsTimeline(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Two plays on the same day (epoch day 0 = 1970-01-01).
	seedPlay(t, playSvc, ownerID, "T1", "A", "Alb", 100, 100_000)
	seedPlay(t, playSvc, ownerID, "T2", "A", "Alb", 200, 100_000)

	rec := doGET(t, srv, "/api/v1/stats/timeline?from=0&to=9999999999&bucket=day", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/timeline = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Start    int64 `json:"Start"`
		Plays    int   `json:"Plays"`
		MsPlayed int64 `json:"MsPlayed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected at least 1 bucket")
	}
	if resp[0].Plays != 2 {
		t.Errorf("bucket plays = %d, want 2", resp[0].Plays)
	}
}

// TestStatsTimelineBucketParam verifies the bucket param is respected.
func TestStatsTimelineBucketParam(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Two plays in the same month but different days.
	// 2024-03-01 and 2024-03-15 in unix seconds.
	seedPlay(t, playSvc, ownerID, "T1", "A", "Alb", 1_709_251_200, 100_000) // 2024-03-01
	seedPlay(t, playSvc, ownerID, "T2", "A", "Alb", 1_710_460_800, 100_000) // 2024-03-15

	rec := doGET(t, srv, "/api/v1/stats/timeline?from=0&to=9999999999&bucket=month", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/timeline bucket=month = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Plays int `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Both plays should be in the same month bucket.
	if len(resp) != 1 {
		t.Errorf("got %d month buckets, want 1 (both in March 2024)", len(resp))
	}
	if resp[0].Plays != 2 {
		t.Errorf("month bucket plays = %d, want 2", resp[0].Plays)
	}
}

// --- Clock ---

// TestStatsClock verifies GET /stats/clock returns weekday/hour cells.
func TestStatsClock(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// 2024-06-01 00:00:00 UTC = Saturday, hour 0. In Go's Sunday=0 scheme: Sat=6.
	// Using a well-known non-zero timestamp so Record() doesn't substitute time.Now().
	const ts = int64(1_717_200_000)
	seedPlay(t, playSvc, ownerID, "T1", "A", "Alb", ts, 100_000)

	rec := doGET(t, srv, fmt.Sprintf("/api/v1/stats/clock?from=%d&to=9999999999", ts-1), cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/clock = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Weekday int `json:"Weekday"`
		Hour    int `json:"Hour"`
		Plays   int `json:"Plays"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected at least 1 clock cell")
	}
	if resp[0].Plays != 1 {
		t.Errorf("clock cell plays = %d, want 1", resp[0].Plays)
	}
	// Saturday (Go weekday: Sun=0 … Sat=6) at hour 0.
	if resp[0].Hour != 0 {
		t.Errorf("clock cell hour = %d, want 0 (midnight UTC)", resp[0].Hour)
	}
}

// TestStatsClockTzOffsetMinutes verifies that tzOffsetMinutes shifts the hour bucket.
func TestStatsClockTzOffsetMinutes(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// 2024-06-01 00:00:00 UTC. With tzOffsetMinutes=60 (UTC+1) → 01:00 local, same day.
	const ts = int64(1_717_200_000)
	seedPlay(t, playSvc, ownerID, "T1", "A", "Alb", ts, 100_000)

	rec := doGET(t, srv, fmt.Sprintf("/api/v1/stats/clock?from=%d&to=9999999999&tzOffsetMinutes=60", ts-1), cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/clock tzOffset = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Hour int `json:"Hour"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("expected 1 clock cell")
	}
	if resp[0].Hour != 1 {
		t.Errorf("clock cell hour with tz+60 = %d, want 1", resp[0].Hour)
	}
}

// --- Recent ---

// TestStatsRecent verifies GET /stats/recent returns plays newest-first.
func TestStatsRecent(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	seedPlay(t, playSvc, ownerID, "Old", "A", "Alb", 1_000, 100_000)
	seedPlay(t, playSvc, ownerID, "New", "A", "Alb", 2_000, 100_000)

	rec := doGET(t, srv, "/api/v1/stats/recent", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/recent = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		Title    string `json:"Title"`
		PlayedAt int64  `json:"PlayedAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) < 2 {
		t.Fatalf("got %d recent plays, want ≥2", len(resp))
	}
	// Newest first.
	if resp[0].Title != "New" {
		t.Errorf("recent[0] = %q, want New (newest first)", resp[0].Title)
	}
}

// TestStatsRecentLimitTruncates verifies limit param is respected.
func TestStatsRecentLimitTruncates(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	for i := 0; i < 5; i++ {
		seedPlay(t, playSvc, ownerID, fmt.Sprintf("Track %d", i), "A", "Alb", int64(1_000+i), 100_000)
	}

	rec := doGET(t, srv, "/api/v1/stats/recent?limit=2", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/recent?limit=2 = %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct{ Title string }
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("got %d recent with limit=2, want 2", len(resp))
	}
}

// --- Entity ---

// TestStatsEntityArtist verifies GET /stats/entity?kind=artist returns artist stats.
func TestStatsEntityArtist(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	seedPlay(t, playSvc, ownerID, "Song 1", "Johnny Cash", "American IV", 1_000_000, 218_000)
	seedPlay(t, playSvc, ownerID, "Song 2", "Johnny Cash", "Ring of Fire", 1_000_001, 157_000)

	rec := doGET(t, srv, "/api/v1/stats/entity?kind=artist&id=Johnny+Cash", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/entity kind=artist = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Plays    int   `json:"Plays"`
		MsPlayed int64 `json:"MsPlayed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plays != 2 {
		t.Errorf("entity artist plays = %d, want 2", resp.Plays)
	}
}

// TestStatsEntityAlbum verifies GET /stats/entity?kind=album&id&artist returns
// album stats, disambiguated by artist.
func TestStatsEntityAlbum(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Two albums sharing the title "Hits" under different artists.
	seedPlay(t, playSvc, ownerID, "A", "Artist X", "Hits", 1_000_000, 100_000)
	seedPlay(t, playSvc, ownerID, "B", "Artist X", "Hits", 1_000_001, 120_000)
	seedPlay(t, playSvc, ownerID, "C", "Artist Y", "Hits", 1_000_002, 130_000)

	rec := doGET(t, srv, "/api/v1/stats/entity?kind=album&id=Hits&artist=Artist+X", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /stats/entity kind=album = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Plays    int   `json:"Plays"`
		MsPlayed int64 `json:"MsPlayed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only Artist X's two plays on "Hits" — Artist Y's must be disambiguated out.
	if resp.Plays != 2 {
		t.Errorf("entity album plays = %d, want 2 (artist Y leaked?)", resp.Plays)
	}
}

// TestStatsEntityAlbumMissingArtist verifies kind=album without the artist param is 400.
func TestStatsEntityAlbumMissingArtist(t *testing.T) {
	srv, cookie, _, _ := statsTestServer(t)

	rec := doGET(t, srv, "/api/v1/stats/entity?kind=album&id=Hits", cookie.Value)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /stats/entity kind=album with no artist = %d, want 400", rec.Code)
	}
}

// --- Play counts ---

// TestStatsPlayCounts verifies POST /stats/play-counts returns per-track counts
// keyed by the caller's opaque key, scoped to the session user.
func TestStatsPlayCounts(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Owner plays "Hurt" twice and "Ring of Fire" once.
	seedPlay(t, playSvc, ownerID, "Hurt", "Johnny Cash", "American IV", 1_000_000, 218_000)
	seedPlay(t, playSvc, ownerID, "Hurt", "Johnny Cash", "American IV", 1_000_001, 218_000)
	seedPlay(t, playSvc, ownerID, "Ring of Fire", "Johnny Cash", "Ring of Fire", 1_000_002, 157_000)

	body := `{"tracks":[
		{"key":"k1","title":"Hurt","artist":"Johnny Cash","album":"American IV","durationMs":218000},
		{"key":"k2","title":"Ring of Fire","artist":"Johnny Cash","album":"Ring of Fire","durationMs":157000},
		{"key":"k3","title":"Unplayed","artist":"Nobody","album":"Void","durationMs":100000}
	]}`
	rec := doPOST(t, srv, "/api/v1/stats/play-counts", cookie.Value, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /stats/play-counts = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Counts["k1"] != 2 {
		t.Errorf("k1 count = %d, want 2", resp.Counts["k1"])
	}
	if resp.Counts["k2"] != 1 {
		t.Errorf("k2 count = %d, want 1", resp.Counts["k2"])
	}
	if resp.Counts["k3"] != 0 {
		t.Errorf("k3 count = %d, want 0", resp.Counts["k3"])
	}
}

// TestStatsPlayCountsSessionScoped is the load-bearing privacy assertion: the
// counts for the session user must NOT include another user's plays of the same
// track. Non-vacuous: a different user has 4 plays of the track at the DB level.
func TestStatsPlayCountsSessionScoped(t *testing.T) {
	srv, cookie, ownerID, playSvc := statsTestServer(t)

	// Session user (owner) played "Hurt" once.
	seedPlay(t, playSvc, ownerID, "Hurt", "Johnny Cash", "American IV", 1_000_000, 218_000)
	// A DIFFERENT user played "Hurt" 4 times — same track identity at the DB level.
	for i := 0; i < 4; i++ {
		seedPlay(t, playSvc, "other-user", "Hurt", "Johnny Cash", "American IV", int64(2_000_000+i), 218_000)
	}

	body := `{"tracks":[{"key":"k","title":"Hurt","artist":"Johnny Cash","album":"American IV","durationMs":218000}]}`
	rec := doPOST(t, srv, "/api/v1/stats/play-counts", cookie.Value, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /stats/play-counts = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Counts["k"] != 1 {
		t.Errorf("session-scoped count = %d, want 1 (other user's 4 plays leaked)", resp.Counts["k"])
	}
}

// TestStatsPlayCountsRequiresAuth verifies the endpoint rejects unauthenticated requests.
func TestStatsPlayCountsRequiresAuth(t *testing.T) {
	srv, _, _, _ := statsTestServer(t)
	rec := doPOST(t, srv, "/api/v1/stats/play-counts", "", `{"tracks":[]}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /stats/play-counts unauthenticated = %d, want 401", rec.Code)
	}
}

// TestStatsPlayCountsBadBody verifies a malformed body is rejected with 400.
func TestStatsPlayCountsBadBody(t *testing.T) {
	srv, cookie, _, _ := statsTestServer(t)
	rec := doPOST(t, srv, "/api/v1/stats/play-counts", cookie.Value, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /stats/play-counts bad body = %d, want 400", rec.Code)
	}
}

// --- Auth / nil-dep guards ---

// TestStatsRequiresAuth verifies all stats endpoints reject unauthenticated requests.
func TestStatsRequiresAuth(t *testing.T) {
	srv, _, _, _ := statsTestServer(t)

	paths := []string{
		"/api/v1/stats/summary",
		"/api/v1/stats/top/tracks",
		"/api/v1/stats/top/artists",
		"/api/v1/stats/top/albums",
		"/api/v1/stats/timeline",
		"/api/v1/stats/clock",
		"/api/v1/stats/recent",
		"/api/v1/stats/entity?kind=artist&id=X",
	}
	for _, p := range paths {
		rec := doGET(t, srv, p, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s unauthenticated = %d, want 401", p, rec.Code)
		}
	}
}

// TestStatsNilServiceReturns503 verifies that 503 is returned when Stats is nil.
func TestStatsNilServiceReturns503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/stats-nil.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		// Stats intentionally nil.
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	paths := []string{
		"/api/v1/stats/summary",
		"/api/v1/stats/top/tracks",
		"/api/v1/stats/top/artists",
		"/api/v1/stats/top/albums",
		"/api/v1/stats/timeline",
		"/api/v1/stats/clock",
		"/api/v1/stats/recent",
		"/api/v1/stats/entity?kind=artist&id=X",
	}
	for _, p := range paths {
		rec := doGET(t, srv, p, cookie.Value)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s with nil Stats = %d, want 503", p, rec.Code)
		}
	}
}
