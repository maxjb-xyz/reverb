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
	mu          sync.Mutex
	artistMap   map[string]ArtistMapRow   // key: libraryArtistID+"|"+source
	disco       map[string]DiscoRow       // key: source+"|"+externalArtistID
	albumCov    map[string]CoverageRow    // key: source+"|"+externalAlbumID
}

func newMemCache() CoverageCache {
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

func (m *memCache) UpsertAlbumCoverage(_ context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.albumCov[source+"|"+externalAlbumID] = CoverageRow{CoverageJSON: coverageJSON, LibraryAlbumID: libraryAlbumID, Found: true}
	return nil
}

func (m *memCache) DeleteAlbumCoverageForLibraryAlbum(_ context.Context, libraryAlbumID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.albumCov {
		if v.LibraryAlbumID == libraryAlbumID {
			delete(m.albumCov, k)
		}
	}
	return nil
}

func TestStreamCoverageComputesPerAlbum(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco:   []core.ExternalAlbum{{Source: "spotify", ExternalID: "AL", Name: "Kid A", Kind: "album", TotalTracks: 2}},
		albums:  map[string]core.ExternalAlbum{"AL": album("t1", "t2")},
	}
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // t2 missing → partial
	svc := NewService(disco, m, fakeLibrary{}, newMemCache(), func() int64 { return 1 })
	ch := svc.StreamCoverage(context.Background(), "library", "libArtist1")
	var got []core.AlbumCoverage
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].State != core.CoveragePartial || got[0].OwnedCount != 1 {
		t.Fatalf("bad stream: %+v", got)
	}
}
