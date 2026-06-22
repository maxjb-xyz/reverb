// internal/coverage/service_test.go
package coverage

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// fakeDisco implements DiscoSource for tests.
type fakeDisco struct {
	artists []core.ExternalResult
	albums  map[string]core.ExternalAlbum // externalAlbumID -> full album (with tracks)
	disco   []core.ExternalAlbum
}

func (f fakeDisco) Search(_ context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	if t == core.EntityArtist {
		return f.artists, nil
	}
	return nil, nil
}
func (f fakeDisco) GetAlbum(_ context.Context, id string) (core.ExternalAlbum, error) {
	return f.albums[id], nil
}
func (f fakeDisco) GetArtistDiscography(_ context.Context, _ string) ([]core.ExternalAlbum, error) {
	return f.disco, nil
}

// fakeLibrary implements LibraryArtist for tests.
type fakeLibrary struct{}

func (fakeLibrary) GetArtist(_ context.Context, id string) (core.Artist, error) {
	if id == "libArtist1" {
		return core.Artist{ID: "libArtist1", Name: "Radiohead"}, nil
	}
	return core.Artist{}, errors.New("not found")
}

func (fakeLibrary) GetAlbum(_ context.Context, id string) (core.Album, error) {
	return core.Album{ID: id}, nil
}

// memCache is an in-memory CoverageCache for tests.
type memCache struct {
	mu        sync.Mutex
	artistMap map[string]ArtistMapRow // key: libraryArtistID+"|"+source
	disco     map[string]DiscoRow     // key: source+"|"+externalArtistID
	albumCov  map[string]CoverageRow  // key: source+"|"+externalAlbumID
}

func newMemCache() *memCache {
	return &memCache{
		artistMap: map[string]ArtistMapRow{},
		disco:     map[string]DiscoRow{},
		albumCov:  map[string]CoverageRow{},
	}
}

func (m *memCache) GetArtistExternalMap(_ context.Context, libraryArtistID, source string) (ArtistMapRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.artistMap[libraryArtistID+"|"+source]
	if !ok {
		return ArtistMapRow{}, errors.New("not found")
	}
	return row, nil
}

func (m *memCache) UpsertArtistExternalMap(_ context.Context, libraryArtistID, source, externalID string, confidence float64, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artistMap[libraryArtistID+"|"+source] = ArtistMapRow{ExternalArtistID: externalID, Confidence: confidence}
	return nil
}

func (m *memCache) GetDiscographyCache(_ context.Context, source, externalArtistID string) (DiscoRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.disco[source+"|"+externalArtistID]
	if !ok {
		return DiscoRow{}, errors.New("not found")
	}
	return row, nil
}

func (m *memCache) UpsertDiscographyCache(_ context.Context, source, externalArtistID, albumsJSON string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disco[source+"|"+externalArtistID] = DiscoRow{AlbumsJSON: albumsJSON}
	return nil
}

func (m *memCache) GetAlbumCoverage(_ context.Context, source, externalAlbumID string) (CoverageRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.albumCov[source+"|"+externalAlbumID]
	if !ok {
		// miss: return Found:false, no error
		return CoverageRow{Found: false}, nil
	}
	return row, nil
}

func (m *memCache) UpsertAlbumCoverage(_ context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, libraryVersion int64, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.albumCov[source+"|"+externalAlbumID] = CoverageRow{CoverageJSON: coverageJSON, LibraryAlbumID: libraryAlbumID, LibraryVersion: libraryVersion, Found: true}
	return nil
}

// upsertAlbumCoverageRaw seeds a coverage row directly (for tests that need to
// prime the cache with a specific library_version, e.g. a stale-row test).
func (m *memCache) upsertAlbumCoverageRaw(source, externalAlbumID, coverageJSON, libraryAlbumID string, libraryVersion int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.albumCov[source+"|"+externalAlbumID] = CoverageRow{CoverageJSON: coverageJSON, LibraryAlbumID: libraryAlbumID, LibraryVersion: libraryVersion, Found: true}
}

func TestStreamCoverageComputesPerAlbum(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco:   []core.ExternalAlbum{{Source: "spotify", ExternalID: "AL", Name: "Kid A", Kind: "album", TotalTracks: 2}},
		albums:  map[string]core.ExternalAlbum{"AL": album("t1", "t2")},
	}
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // t2 missing → partial
	svc := NewService(disco, m, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	ch := svc.StreamCoverage(context.Background(), "library", "libArtist1")
	var got []core.AlbumCoverage
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].State != core.CoveragePartial || got[0].OwnedCount != 1 {
		t.Fatalf("bad stream: %+v", got)
	}
}

// source="spotify" skips library resolution and takes the external id directly;
// the artist name must fall back to albums[0].Artist (Fix 1: GetArtistDiscography
// now populates ExternalAlbum.Artist).
func TestArtistDetailSpotifySourceNameFromDiscography(t *testing.T) {
	disco := fakeDisco{
		disco: []core.ExternalAlbum{
			{Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
		},
	}
	svc := NewService(disco, fakeMatcher{}, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "spotify", "art1")
	if err != nil {
		t.Fatal(err)
	}
	if !det.Resolved {
		t.Fatalf("spotify source must resolve, got %+v", det)
	}
	if det.Name != "Radiohead" {
		t.Fatalf("name must fall back to albums[0].Artist, got %q", det.Name)
	}
}

// Fix 4: a library artist whose name matches NO Spotify candidate must degrade to
// library-only (resolved:false) — NOT resolve to a wrong artist's top result.
func TestArtistDetailDegradesWhenNoConfidentMatch(t *testing.T) {
	disco := fakeDisco{
		// "Radiohead" (library) vs a single wrong candidate → no normalized match.
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "wrong", Title: "Coldplay", Type: core.EntityArtist}},
	}
	svc := NewService(disco, fakeMatcher{}, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatal(err)
	}
	if det.Resolved {
		t.Fatalf("must degrade to library-only when no candidate matches, got resolved=%v", det.Resolved)
	}
	// fakeLibrary's artist carries no Albums → empty skeleton (not nil).
	if det.Albums == nil {
		t.Fatalf("degrade must return a (possibly empty) library-album skeleton, got nil")
	}
}

func TestStreamCoverageRecomputesWhenLibraryVersionStale(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco:   []core.ExternalAlbum{{Source: "spotify", ExternalID: "AL", Name: "Kid A", Kind: "album", TotalTracks: 2}},
		albums:  map[string]core.ExternalAlbum{"AL": album("t1", "t2")},
	}
	cache := newMemCache()
	// Seed a STALE cached row (computed at version 1) claiming full coverage.
	cache.upsertAlbumCoverageRaw("spotify", "AL", `{"source":"spotify","externalAlbumId":"AL","state":"full","ownedCount":2,"totalCount":2,"missingTracks":[]}`, "", 1)
	// Current version is 2 → the row is stale and must be recomputed.
	curVer := int64(2)
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // only t1 owned → recompute yields partial 1/2
	svc := NewService(disco, m, fakeLibrary{}, cache, func() int64 { return 1 },
		func(context.Context) (int64, error) { return curVer, nil })
	var got []core.AlbumCoverage
	for c := range svc.StreamCoverage(context.Background(), "library", "libArtist1") {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].State != core.CoveragePartial || got[0].OwnedCount != 1 {
		t.Fatalf("stale row must be recomputed to partial 1/2, got %+v", got)
	}
}
