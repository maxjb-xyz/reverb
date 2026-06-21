package matching

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakeLib returns a fixed candidate set for any query (the case's library tracks).
type fakeLib struct{ tracks []core.Track }

func (f fakeLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: f.tracks}, nil
}

// countingLib wraps a LibrarySearcher and counts how many times Search is
// invoked, so tests can assert cache hits avoid the library query entirely.
type countingLib struct {
	inner LibrarySearcher
	calls int
}

func (c *countingLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	c.calls++
	return c.inner.Search(ctx, q, types)
}

// memCache is an in-memory MatchCacheStore.
type memCache struct{ rows map[string]db.MatchCache }

func newMemCache() *memCache { return &memCache{rows: map[string]db.MatchCache{}} }
func key(s, e string) string { return s + "\x1f" + e }
func (m *memCache) GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error) {
	r, ok := m.rows[key(arg.Source, arg.ExternalID)]
	if !ok {
		return db.MatchCache{}, sql.ErrNoRows
	}
	return r, nil
}
func (m *memCache) UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error {
	m.rows[key(arg.Source, arg.ExternalID)] = db.MatchCache{
		Source: arg.Source, ExternalID: arg.ExternalID, LibraryTrackID: arg.LibraryTrackID,
		Method: arg.Method, Confidence: arg.Confidence, Isrc: arg.Isrc, Mbid: arg.Mbid,
		DurationMs: arg.DurationMs, LibraryVersion: arg.LibraryVersion,
	}
	return nil
}

func fixtureToTrack(ft fixtureTrack) core.Track {
	return core.Track{ID: ft.ID, Title: ft.Title, Artist: ft.Artist, Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC}
}
func fixtureToExternal(ft fixtureTrack) core.ExternalResult {
	return core.ExternalResult{
		Source: "spotify", ExternalID: "ext-" + ft.Title, Title: ft.Title, Artist: ft.Artist,
		Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC, MBID: ft.MBID, Type: core.EntityTrack,
	}
}

func TestMatchAgainstFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			var cands []core.Track
			for _, lt := range c.Library {
				cands = append(cands, fixtureToTrack(lt))
			}
			svc := NewService(fakeLib{tracks: cands}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
			ext := fixtureToExternal(c.External)

			got, err := svc.Match(context.Background(), ext)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Status) != c.Expect.Status {
				t.Fatalf("status=%q want %q (result %+v)", got.Status, c.Expect.Status, got)
			}
			if got.LibraryTrackID != c.Expect.LibraryTrackID {
				t.Fatalf("libraryTrackId=%q want %q", got.LibraryTrackID, c.Expect.LibraryTrackID)
			}
			if c.Expect.Method != "" && string(got.Method) != c.Expect.Method {
				t.Fatalf("method=%q want %q", got.Method, c.Expect.Method)
			}
		})
	}
}

func TestMatchZeroDurationStillMatches(t *testing.T) {
	// The post-download re-match carries no DurationMs (0). Duration must only
	// disambiguate, never reject — otherwise a title+artist match is dropped and
	// the download is never linked (no play / no cover).
	cands := []core.Track{{ID: "lib-1", Title: "COMË N GO", Artist: "Yeat", Album: "Dangerous Summer", DurationMs: 180000}}
	svc := NewService(fakeLib{tracks: cands}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{
		Source: "spotify", ExternalID: "sp-x", Type: core.EntityTrack,
		Title: "COMË N GO", Artist: "Yeat", Album: "Dangerous Summer", DurationMs: 0,
	}
	res, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != core.MatchInLibrary || res.LibraryTrackID != "lib-1" {
		t.Fatalf("zero-duration re-match should link to lib-1, got %+v", res)
	}
}

func TestMatchCacheFirstAndInvalidation(t *testing.T) {
	cands := []core.Track{{ID: "t1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1"}}
	cache := newMemCache()
	version := int64(1)
	svc := NewService(fakeLib{tracks: cands}, cache, func(context.Context) (int64, error) { return version, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	first, _ := svc.Match(context.Background(), ext)
	if first.Status != core.MatchInLibrary || first.Method != core.MatchISRC {
		t.Fatalf("first match: %+v", first)
	}
	// Now the library no longer has the track, but the cached positive (version 1) should still be served.
	svc.lib = fakeLib{tracks: nil}
	cached, _ := svc.Match(context.Background(), ext)
	if cached.Status != core.MatchInLibrary {
		t.Fatalf("expected cached positive, got %+v", cached)
	}
	// Bump library_version → cache stale → re-match against the (now empty) library → negative.
	version = 2
	fresh, _ := svc.Match(context.Background(), ext)
	if fresh.Status != core.MatchNotInLibrary {
		t.Fatalf("expected re-match negative after version bump, got %+v", fresh)
	}
}

// TestMatchCacheAvoidsLibraryQuery proves cache-first EXPLICITLY: a second Match
// for the same external result must NOT re-query the library (call counter stays
// flat), and a library_version bump must force a recompute (counter increments).
func TestMatchCacheAvoidsLibraryQuery(t *testing.T) {
	cands := []core.Track{{ID: "t1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1"}}
	lib := &countingLib{inner: fakeLib{tracks: cands}}
	cache := newMemCache()
	version := int64(1)
	svc := NewService(lib, cache, func(context.Context) (int64, error) { return version, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	// First Match computes the decision, which requires one library query.
	first, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != core.MatchInLibrary || first.Method != core.MatchISRC {
		t.Fatalf("first match: %+v", first)
	}
	if lib.calls != 1 {
		t.Fatalf("after first Match: lib.calls=%d want 1", lib.calls)
	}

	// Second Match for the SAME external result must be served from match_cache
	// without touching the library: the counter must NOT increment.
	cached, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Status != core.MatchInLibrary {
		t.Fatalf("expected cached positive, got %+v", cached)
	}
	if lib.calls != 1 {
		t.Fatalf("cache hit queried the library: lib.calls=%d want 1", lib.calls)
	}

	// Bump library_version → cached row is stale → Match must recompute, which
	// requires a fresh library query: the counter must increment.
	version = 2
	fresh, err := svc.Match(context.Background(), ext)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Status != core.MatchInLibrary {
		t.Fatalf("expected recompute positive after version bump, got %+v", fresh)
	}
	if lib.calls != 2 {
		t.Fatalf("version bump did not recompute: lib.calls=%d want 2", lib.calls)
	}
}

// TestMatchNonTrackExternalReturnsNotInLibrary verifies that a non-track external
// (e.g. album or artist) returns not_in_library immediately without querying the
// library — the candidate fetch assumes track-typed externals.
func TestMatchNonTrackExternalReturnsNotInLibrary(t *testing.T) {
	lib := &countingLib{inner: fakeLib{tracks: []core.Track{{ID: "t1", Title: "Song", Artist: "A", DurationMs: 200000}}}}
	svc := NewService(lib, nil, func(context.Context) (int64, error) { return 1, nil })

	for _, typ := range []core.EntityType{core.EntityAlbum, core.EntityArtist, core.EntityPlaylist} {
		ext := core.ExternalResult{Source: "spotify", ExternalID: "ext-1", Title: "Song", Artist: "A", DurationMs: 200000, Type: typ}
		got, err := svc.Match(context.Background(), ext)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", typ, err)
		}
		if got.Status != core.MatchNotInLibrary {
			t.Errorf("type %q: status=%q want %q", typ, got.Status, core.MatchNotInLibrary)
		}
	}
	// Library must not have been queried for any of the above non-track types.
	if lib.calls != 0 {
		t.Errorf("library queried %d times for non-track externals, want 0", lib.calls)
	}
}

func TestMatchNegativeIsCached(t *testing.T) {
	cache := newMemCache()
	svc := NewService(fakeLib{tracks: nil}, cache, func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp9", Title: "Nope", Artist: "Z", DurationMs: 100000, Type: core.EntityTrack}
	if _, err := svc.Match(context.Background(), ext); err != nil {
		t.Fatal(err)
	}
	row, err := cache.GetMatchCache(context.Background(), db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp9"})
	if err != nil {
		t.Fatalf("negative match not cached: %v", err)
	}
	if row.LibraryTrackID.Valid {
		t.Fatalf("cached negative should have NULL library_track_id: %+v", row)
	}
}
