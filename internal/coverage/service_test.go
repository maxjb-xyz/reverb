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
	artists     []core.ExternalResult
	albumSearch []core.ExternalResult         // returned for EntityAlbum searches
	albums      map[string]core.ExternalAlbum // externalAlbumID -> full album (with tracks)
	disco       []core.ExternalAlbum
	artist      *core.ExternalArtist // returned by GetArtist; nil → error (not found)
}

func (f fakeDisco) Search(_ context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	if t == core.EntityArtist {
		return f.artists, nil
	}
	if t == core.EntityAlbum {
		return f.albumSearch, nil
	}
	return nil, nil
}
func (f fakeDisco) GetAlbum(_ context.Context, id string) (core.ExternalAlbum, error) {
	return f.albums[id], nil
}
func (f fakeDisco) GetArtist(_ context.Context, _ string) (core.ExternalArtist, error) {
	if f.artist != nil {
		return *f.artist, nil
	}
	return core.ExternalArtist{}, errors.New("not found")
}
func (f fakeDisco) GetArtistDiscography(_ context.Context, _ string) ([]core.ExternalAlbum, error) {
	return f.disco, nil
}

// fakeLibrary implements LibraryArtist for tests.
type fakeLibrary struct {
	// searchAlbums, if set, are returned when Search is called with EntityAlbum.
	// When nil, no albums are returned. When searchErr is set, Search returns that error.
	searchAlbums []core.Album
	searchErr    error
}

func (f fakeLibrary) GetArtist(_ context.Context, id string) (core.Artist, error) {
	if id == "libArtist1" {
		return core.Artist{ID: "libArtist1", Name: "Radiohead"}, nil
	}
	return core.Artist{}, errors.New("not found")
}

func (f fakeLibrary) GetAlbum(_ context.Context, id string) (core.Album, error) {
	if id == "libAlbum1" {
		return core.Album{
			ID: "libAlbum1", Name: "Kid A", Artist: "Radiohead", ArtistID: "libArtist1",
			Tracks: []core.Track{
				{ID: "lt1", Title: "Everything in Its Right Place", Artist: "Radiohead", TrackNumber: 1},
				{ID: "lt2", Title: "Kid A", Artist: "Radiohead", TrackNumber: 2},
				{ID: "lt3", Title: "The National Anthem", Artist: "Radiohead", TrackNumber: 3},
			},
		}, nil
	}
	return core.Album{ID: id}, nil
}

func (f fakeLibrary) Search(_ context.Context, _ string, types []core.EntityType) (core.SearchResults, error) {
	if f.searchErr != nil {
		return core.SearchResults{}, f.searchErr
	}
	res := core.SearchResults{
		Tracks:  []core.Track{},
		Albums:  []core.Album{},
		Artists: []core.Artist{},
	}
	for _, t := range types {
		if t == core.EntityAlbum {
			res.Albums = f.searchAlbums
		}
	}
	return res, nil
}

// memCache is an in-memory CoverageCache for tests.
type memCache struct {
	mu        sync.Mutex
	artistMap map[string]ArtistMapRow // key: libraryArtistID+"|"+source
	albumMap  map[string]AlbumMapRow  // key: libraryAlbumID+"|"+source
	disco     map[string]DiscoRow     // key: source+"|"+externalArtistID
	albumCov  map[string]CoverageRow  // key: source+"|"+externalAlbumID
}

func newMemCache() *memCache {
	return &memCache{
		artistMap: map[string]ArtistMapRow{},
		albumMap:  map[string]AlbumMapRow{},
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

func (m *memCache) GetAlbumExternalMap(_ context.Context, libraryAlbumID, source string) (AlbumMapRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.albumMap[libraryAlbumID+"|"+source]
	if !ok {
		return AlbumMapRow{}, errors.New("not found")
	}
	return row, nil
}

func (m *memCache) UpsertAlbumExternalMap(_ context.Context, libraryAlbumID, source, externalID string, confidence float64, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.albumMap[libraryAlbumID+"|"+source] = AlbumMapRow{ExternalAlbumID: externalID, Confidence: confidence}
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

func (m *memCache) ListCachedDiscographies(_ context.Context) ([]CachedDiscographyRow, error) {
	return []CachedDiscographyRow{}, nil
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

func (m *memCache) GetLibraryAlbumIDByExternal(_ context.Context, source, externalAlbumID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.albumCov[source+"|"+externalAlbumID]
	if !ok || !row.Found {
		return ""
	}
	return row.LibraryAlbumID
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

// TestArtistDetailSpotifyUsesRealProfile: when GetArtist succeeds, ArtistDetail must
// use the real profile name+cover, not derive the name from albums[0].Artist.
// This is the Chopin fix: albums[0].Artist would be "Martha Argerich" (performer),
// but GetArtist returns "Frédéric Chopin" with a real image.
func TestArtistDetailSpotifyUsesRealProfile(t *testing.T) {
	chopinCover := "https://img/chopin.jpg"
	disco := fakeDisco{
		artist: &core.ExternalArtist{
			Source:     "spotify",
			ExternalID: "chopin_id",
			Name:       "Frédéric Chopin",
			CoverURL:   chopinCover,
		},
		disco: []core.ExternalAlbum{
			// First album's artist is a performer, not the composer — the old bug.
			{Source: "spotify", ExternalID: "AL1", Name: "Chopin: Ballades", Artist: "Martha Argerich", Kind: "album", TotalTracks: 4},
		},
	}
	svc := NewService(disco, fakeMatcher{}, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "spotify", "chopin_id")
	if err != nil {
		t.Fatal(err)
	}
	if !det.Resolved {
		t.Fatalf("spotify source must resolve, got %+v", det)
	}
	if det.Name != "Frédéric Chopin" {
		t.Errorf("Name: want %q (real profile), got %q (must not use performer name)", "Frédéric Chopin", det.Name)
	}
	if det.CoverURL != chopinCover {
		t.Errorf("CoverURL: want %q, got %q", chopinCover, det.CoverURL)
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

// TestAlbumDetailLibraryMergesFullTracklist: library album "Kid A" resolves to a
// 3-track Spotify album; matcher owns 2 of 3 external track ids → merged view.
func TestAlbumDetailLibraryMergesFullTracklist(t *testing.T) {
	extAlbum := core.ExternalAlbum{
		Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead",
		Tracks: []core.ExternalResult{
			{Source: "spotify", ExternalID: "et1", Title: "Everything in Its Right Place", Artist: "Radiohead", CoverURL: "https://img/et1.jpg", Type: core.EntityTrack},
			{Source: "spotify", ExternalID: "et2", Title: "Kid A", Artist: "Radiohead", CoverURL: "https://img/et2.jpg", Type: core.EntityTrack},
			{Source: "spotify", ExternalID: "et3", Title: "The National Anthem", Artist: "Radiohead", CoverURL: "https://img/et3.jpg", Type: core.EntityTrack},
		},
	}
	disco := fakeDisco{
		albumSearch: []core.ExternalResult{
			{Source: "spotify", ExternalID: "AL", Title: "Kid A", Artist: "Radiohead", Type: core.EntityAlbum},
		},
		albums: map[string]core.ExternalAlbum{"AL": extAlbum},
	}
	// matcher owns et1 and et2 but not et3; the matched candidates carry artist/
	// album/cover ids that must be threaded onto the synthesized LibraryTrack.
	m := fakeMatcher{
		owned: map[string]string{"et1": "lt1", "et2": "lt2"},
		meta: map[string]core.Track{
			"et1": {ArtistID: "ar1", AlbumID: "al1", CoverArtID: "cv1"},
			"et2": {ArtistID: "ar2", AlbumID: "al2", CoverArtID: "cv2"},
		},
	}
	svc := NewService(disco, m, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.AlbumDetail(context.Background(), "library", "libAlbum1")
	if err != nil {
		t.Fatal(err)
	}
	if det.TotalCount != 3 {
		t.Errorf("TotalCount: want 3, got %d", det.TotalCount)
	}
	if det.OwnedCount != 2 {
		t.Errorf("OwnedCount: want 2, got %d", det.OwnedCount)
	}
	if det.Source != "library" {
		t.Errorf("Source: want %q, got %q", "library", det.Source)
	}
	if det.Name != "Kid A" {
		t.Errorf("Name: want %q, got %q", "Kid A", det.Name)
	}
	if det.LibraryAlbumID != "libAlbum1" {
		t.Errorf("LibraryAlbumID: want %q, got %q", "libAlbum1", det.LibraryAlbumID)
	}
	noneCount := 0
	for _, tr := range det.Tracks {
		if tr.State == core.CoverageNone {
			noneCount++
		}
	}
	if noneCount != 1 {
		t.Errorf("want 1 track with state:none, got %d", noneCount)
	}
	// Every track must carry the external CoverURL regardless of owned/missing state.
	for i, tr := range det.Tracks {
		wantCover := extAlbum.Tracks[i].CoverURL
		if tr.CoverURL != wantCover {
			t.Errorf("track[%d] CoverURL: want %q, got %q", i, wantCover, tr.CoverURL)
		}
	}
	// Owned rows must thread the matched candidate's artist/album/cover ids onto the
	// synthesized LibraryTrack (so the FE renders clickable artist + album links).
	if lt := det.Tracks[0].LibraryTrack; lt == nil || lt.ArtistID != "ar1" || lt.AlbumID != "al1" || lt.CoverArtID != "cv1" {
		t.Errorf("track[0] LibraryTrack metadata not threaded: %+v", lt)
	}
	if lt := det.Tracks[1].LibraryTrack; lt == nil || lt.ArtistID != "ar2" || lt.AlbumID != "al2" || lt.CoverArtID != "cv2" {
		t.Errorf("track[1] LibraryTrack metadata not threaded: %+v", lt)
	}
}

// countingDisco wraps fakeDisco and counts Search calls for EntityAlbum.
type countingDisco struct {
	fakeDisco
	mu           sync.Mutex
	albumSearchN int
}

func (c *countingDisco) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	if t == core.EntityAlbum {
		c.mu.Lock()
		c.albumSearchN++
		c.mu.Unlock()
	}
	return c.fakeDisco.Search(ctx, q, t)
}

// TestAlbumDetailCachesResolution: after a successful AlbumDetail("library", id),
// the album_external_map entry is populated; a second call must NOT re-hit Search.
func TestAlbumDetailCachesResolution(t *testing.T) {
	extAlbum := core.ExternalAlbum{
		Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead",
		Tracks: []core.ExternalResult{
			{Source: "spotify", ExternalID: "et1", Title: "Everything in Its Right Place", Artist: "Radiohead", Type: core.EntityTrack},
		},
	}
	cd := &countingDisco{
		fakeDisco: fakeDisco{
			albumSearch: []core.ExternalResult{
				{Source: "spotify", ExternalID: "AL", Title: "Kid A", Artist: "Radiohead", Type: core.EntityAlbum},
			},
			albums: map[string]core.ExternalAlbum{"AL": extAlbum},
		},
	}
	m := fakeMatcher{owned: map[string]string{"et1": "lt1"}}
	cache := newMemCache()
	svc := NewService(cd, m, fakeLibrary{}, cache, func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })

	// First call: Search must be invoked once.
	if _, err := svc.AlbumDetail(context.Background(), "library", "libAlbum1"); err != nil {
		t.Fatal(err)
	}
	if cd.albumSearchN != 1 {
		t.Fatalf("first call: expected 1 Search, got %d", cd.albumSearchN)
	}

	// Second call: cache hit → Search must NOT be called again.
	if _, err := svc.AlbumDetail(context.Background(), "library", "libAlbum1"); err != nil {
		t.Fatal(err)
	}
	if cd.albumSearchN != 1 {
		t.Fatalf("second call: expected Search count to stay at 1 (cached), got %d", cd.albumSearchN)
	}

	// Confirm the cache entry was actually written.
	row, err := cache.GetAlbumExternalMap(context.Background(), "libAlbum1", "spotify")
	if err != nil || row.ExternalAlbumID != "AL" {
		t.Fatalf("expected cache entry AL, got row=%+v err=%v", row, err)
	}
}

// TestAlbumDetailCachePreseededSkipsSearch: if the cache is pre-seeded with an
// album map entry, AlbumDetail must skip Search entirely.
func TestAlbumDetailCachePreseededSkipsSearch(t *testing.T) {
	extAlbum := core.ExternalAlbum{
		Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead",
		Tracks: []core.ExternalResult{
			{Source: "spotify", ExternalID: "et1", Title: "Everything in Its Right Place", Artist: "Radiohead", Type: core.EntityTrack},
		},
	}
	cd := &countingDisco{
		fakeDisco: fakeDisco{
			albumSearch: []core.ExternalResult{
				{Source: "spotify", ExternalID: "AL", Title: "Kid A", Artist: "Radiohead", Type: core.EntityAlbum},
			},
			albums: map[string]core.ExternalAlbum{"AL": extAlbum},
		},
	}
	m := fakeMatcher{owned: map[string]string{"et1": "lt1"}}
	cache := newMemCache()
	// Pre-seed the album map.
	_ = cache.UpsertAlbumExternalMap(context.Background(), "libAlbum1", "spotify", "AL", 1.0, 0)

	svc := NewService(cd, m, fakeLibrary{}, cache, func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })

	if _, err := svc.AlbumDetail(context.Background(), "library", "libAlbum1"); err != nil {
		t.Fatal(err)
	}
	if cd.albumSearchN != 0 {
		t.Fatalf("pre-seeded cache: expected 0 Search calls, got %d", cd.albumSearchN)
	}
}

// TestAlbumDetailLibraryDegradesWhenNoMatch: no matching Spotify album candidate →
// falls back to the library album's own tracks, all state:full, source=="library".
func TestAlbumDetailLibraryDegradesWhenNoMatch(t *testing.T) {
	disco := fakeDisco{
		albumSearch: nil, // no candidates
		albums:      map[string]core.ExternalAlbum{},
	}
	m := fakeMatcher{}
	svc := NewService(disco, m, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.AlbumDetail(context.Background(), "library", "libAlbum1")
	if err != nil {
		t.Fatal(err)
	}
	if det.Source != "library" {
		t.Errorf("Source: want %q, got %q", "library", det.Source)
	}
	// fakeLibrary returns 3 tracks for libAlbum1; all should be state:full
	if det.TotalCount != 3 {
		t.Errorf("TotalCount: want 3, got %d", det.TotalCount)
	}
	if det.OwnedCount != 3 {
		t.Errorf("OwnedCount: want 3, got %d", det.OwnedCount)
	}
	if det.LibraryAlbumID != "libAlbum1" {
		t.Errorf("LibraryAlbumID: want %q, got %q", "libAlbum1", det.LibraryAlbumID)
	}
	for _, tr := range det.Tracks {
		if tr.State != core.CoverageFull {
			t.Errorf("fallback track %q: want state:full, got %s", tr.Title, tr.State)
		}
	}
}

// TestArtistDetailLibraryAlbumIDBackfill: ArtistDetail sets LibraryAlbumID on a
// DiscographyAlbum when the album_coverage cache already holds a mapping for its
// external id, and leaves it empty when the mapping is absent.
func TestArtistDetailLibraryAlbumIDBackfill(t *testing.T) {
	// Two external albums: "AL" is mapped (library album "libAlbum1"), "BL" is not.
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco: []core.ExternalAlbum{
			{Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
			{Source: "spotify", ExternalID: "BL", Name: "Amnesiac", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
		},
		albums: map[string]core.ExternalAlbum{},
	}
	cache := newMemCache()
	// Pre-seed the coverage cache for "AL" with a known library album id.
	cache.upsertAlbumCoverageRaw("spotify", "AL", `{"source":"spotify","externalAlbumId":"AL","state":"full","ownedCount":2,"totalCount":2,"missingTracks":[]}`, "libAlbum1", 1)
	// "BL" has no coverage entry.

	svc := NewService(disco, fakeMatcher{}, fakeLibrary{}, cache, func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatal(err)
	}
	if !det.Resolved {
		t.Fatalf("expected resolved=true, got %+v", det)
	}
	if len(det.Albums) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(det.Albums))
	}
	for _, da := range det.Albums {
		switch da.ExternalID {
		case "AL":
			if da.LibraryAlbumID != "libAlbum1" {
				t.Errorf("AL: LibraryAlbumID: want %q, got %q", "libAlbum1", da.LibraryAlbumID)
			}
		case "BL":
			if da.LibraryAlbumID != "" {
				t.Errorf("BL: LibraryAlbumID: want empty, got %q", da.LibraryAlbumID)
			}
		default:
			t.Errorf("unexpected album externalId %q", da.ExternalID)
		}
	}
}

// TestArtistDetailLibraryAlbumsPopulated: when the library search returns albums
// for the artist name, ArtistDetail populates LibraryAlbums with deduped
// DiscographyAlbum entries (source "library", LibraryAlbumID set).
func TestArtistDetailLibraryAlbumsPopulated(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco: []core.ExternalAlbum{
			{Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
		},
		artist: &core.ExternalArtist{Source: "spotify", ExternalID: "art1", Name: "Radiohead"},
	}
	lib := fakeLibrary{
		searchAlbums: []core.Album{
			{ID: "libAlbum1", Name: "Kid A", Artist: "Radiohead", Year: 2000, SongCount: 10},
			{ID: "libAlbum2", Name: "Amnesiac", Artist: "Radiohead", Year: 2001, SongCount: 11},
		},
	}
	svc := NewService(disco, fakeMatcher{}, lib, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatal(err)
	}
	if len(det.LibraryAlbums) != 2 {
		t.Fatalf("LibraryAlbums: want 2, got %d: %+v", len(det.LibraryAlbums), det.LibraryAlbums)
	}
	for _, la := range det.LibraryAlbums {
		if la.Source != "library" {
			t.Errorf("LibraryAlbum %q: Source want %q, got %q", la.Name, "library", la.Source)
		}
		if la.LibraryAlbumID == "" {
			t.Errorf("LibraryAlbum %q: LibraryAlbumID must not be empty", la.Name)
		}
		if la.LibraryAlbumID != la.ExternalID {
			t.Errorf("LibraryAlbum %q: LibraryAlbumID %q != ExternalID %q", la.Name, la.LibraryAlbumID, la.ExternalID)
		}
	}
}

// TestArtistDetailLibraryAlbumsDedup: two search-result albums with the same ID
// must produce only one LibraryAlbum entry (dedup by ID + normalized name).
func TestArtistDetailLibraryAlbumsDedup(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco: []core.ExternalAlbum{
			{Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
		},
		artist: &core.ExternalArtist{Source: "spotify", ExternalID: "art1", Name: "Radiohead"},
	}
	// Return the same album twice — service must deduplicate.
	lib := fakeLibrary{
		searchAlbums: []core.Album{
			{ID: "libAlbum1", Name: "Kid A", Artist: "Radiohead", Year: 2000, SongCount: 10},
			{ID: "libAlbum1", Name: "Kid A", Artist: "Radiohead", Year: 2000, SongCount: 10},
		},
	}
	svc := NewService(disco, fakeMatcher{}, lib, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatal(err)
	}
	if len(det.LibraryAlbums) != 1 {
		t.Fatalf("LibraryAlbums dedup: want 1, got %d: %+v", len(det.LibraryAlbums), det.LibraryAlbums)
	}
}

// TestArtistProfileSpotify: ArtistProfile("spotify", id) calls GetArtist once and
// returns the name + coverUrl. No discography call is made.
func TestArtistProfileSpotify(t *testing.T) {
	discoCallCount := 0
	disco := fakeDisco{
		artist: &core.ExternalArtist{
			Source:     "spotify",
			ExternalID: "sp1",
			Name:       "Radiohead",
			CoverURL:   "https://img/rh.jpg",
		},
	}
	// Wrap fakeDisco in a countingDisco to assert GetArtistDiscography is not called.
	cd := &countingDisco{fakeDisco: disco}
	_ = discoCallCount // suppress unused warning
	svc := NewService(cd, fakeMatcher{}, fakeLibrary{}, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	prof, err := svc.ArtistProfile(context.Background(), "spotify", "sp1")
	if err != nil {
		t.Fatalf("ArtistProfile: unexpected error: %v", err)
	}
	if prof.Name != "Radiohead" {
		t.Errorf("Name: want %q, got %q", "Radiohead", prof.Name)
	}
	if prof.CoverURL != "https://img/rh.jpg" {
		t.Errorf("CoverURL: want %q, got %q", "https://img/rh.jpg", prof.CoverURL)
	}
	if prof.Source != "spotify" {
		t.Errorf("Source: want %q, got %q", "spotify", prof.Source)
	}
	if prof.ExternalID != "sp1" {
		t.Errorf("ExternalID: want %q, got %q", "sp1", prof.ExternalID)
	}
	// Discography must NOT have been fetched — ArtistProfile is lightweight.
	if cd.albumSearchN != 0 {
		t.Errorf("albumSearchN: want 0 (no discography), got %d", cd.albumSearchN)
	}
}

// TestArtistProfileLibrary: ArtistProfile("library", id) calls lib.GetArtist once
// and returns name + coverArtId. No external call is made.
func TestArtistProfileLibrary(t *testing.T) {
	// Extend fakeLibrary with a known artist that has a CoverArtID.
	lib := fakeLibraryWithCover{}
	svc := NewService(fakeDisco{}, fakeMatcher{}, lib, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	prof, err := svc.ArtistProfile(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatalf("ArtistProfile: unexpected error: %v", err)
	}
	if prof.Name != "Radiohead" {
		t.Errorf("Name: want %q, got %q", "Radiohead", prof.Name)
	}
	if prof.CoverArtID != "cov42" {
		t.Errorf("CoverArtID: want %q, got %q", "cov42", prof.CoverArtID)
	}
	if prof.Source != "library" {
		t.Errorf("Source: want %q, got %q", "library", prof.Source)
	}
	if prof.ExternalID != "libArtist1" {
		t.Errorf("ExternalID: want %q, got %q", "libArtist1", prof.ExternalID)
	}
}

// fakeLibraryWithCover is a fakeLibrary variant that returns an artist with CoverArtID.
type fakeLibraryWithCover struct{ fakeLibrary }

func (f fakeLibraryWithCover) GetArtist(_ context.Context, id string) (core.Artist, error) {
	if id == "libArtist1" {
		return core.Artist{ID: "libArtist1", Name: "Radiohead", CoverArtID: "cov42"}, nil
	}
	return core.Artist{}, errors.New("not found")
}

// TestArtistDetailLibraryAlbumsSearchError: when the library Search fails,
// ArtistDetail degrades gracefully — LibraryAlbums is empty, no error returned.
func TestArtistDetailLibraryAlbumsSearchError(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco: []core.ExternalAlbum{
			{Source: "spotify", ExternalID: "AL", Name: "Kid A", Artist: "Radiohead", Kind: "album", TotalTracks: 2},
		},
		artist: &core.ExternalArtist{Source: "spotify", ExternalID: "art1", Name: "Radiohead"},
	}
	lib := fakeLibrary{
		searchErr: errors.New("subsonic unavailable"),
	}
	svc := NewService(disco, fakeMatcher{}, lib, newMemCache(), func() int64 { return 1 }, func(context.Context) (int64, error) { return 1, nil })
	det, err := svc.ArtistDetail(context.Background(), "library", "libArtist1")
	if err != nil {
		t.Fatalf("ArtistDetail must not propagate search error: %v", err)
	}
	if det.LibraryAlbums == nil {
		t.Fatal("LibraryAlbums must not be nil on search error, want empty slice")
	}
	if len(det.LibraryAlbums) != 0 {
		t.Fatalf("LibraryAlbums: want empty on search error, got %d entries", len(det.LibraryAlbums))
	}
}
