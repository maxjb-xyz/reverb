package matching

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store/db"
)

// fakeLib returns a fixed candidate set for any query (the case's library tracks).
type fakeLib struct{ tracks []core.Track }

func (f fakeLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: f.tracks}, nil
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
