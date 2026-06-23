package playlistsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func track(id string) core.ExternalResult {
	return core.ExternalResult{Source: "spotify", ExternalID: id, Title: id, Type: core.EntityTrack}
}

// seqID returns a func() string that yields "sp1", "sp2", … on each call.
func seqID() func() string {
	var n atomic.Int64
	return func() string {
		return fmt.Sprintf("sp%d", n.Add(1))
	}
}

// ---------------------------------------------------------------------------
// fakeSource
// ---------------------------------------------------------------------------

type fakeSource struct {
	playlists map[string]core.ExternalPlaylist
	syncCount int
	err       error // when non-nil, GetPlaylist returns this error
}

func (f *fakeSource) ParsePlaylistID(url string) (string, bool) {
	// Handles:
	//   spotify:playlist:PL
	//   https://open.spotify.com/playlist/PL
	//   https://open.spotify.com/playlist/PL?si=xxx
	if strings.HasPrefix(url, "spotify:playlist:") {
		id := strings.TrimPrefix(url, "spotify:playlist:")
		if id == "" {
			return "", false
		}
		return id, true
	}
	const seg = "/playlist/"
	idx := strings.LastIndex(url, seg)
	if idx < 0 {
		return "", false
	}
	id := url[idx+len(seg):]
	if q := strings.IndexByte(id, '?'); q >= 0 {
		id = id[:q]
	}
	if id == "" {
		return "", false
	}
	return id, true
}

func (f *fakeSource) GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error) {
	f.syncCount++
	if f.err != nil {
		return core.ExternalPlaylist{}, f.err
	}
	pl, ok := f.playlists[externalID]
	if !ok {
		return core.ExternalPlaylist{}, fmt.Errorf("playlist %q not found", externalID)
	}
	return pl, nil
}

// ---------------------------------------------------------------------------
// fakeMatcher
// ---------------------------------------------------------------------------

type fakeMatcher struct {
	// owned maps ExternalID → library track id
	owned map[string]string
	// meta optionally carries the matched candidate's artist/album/cover ids.
	meta map[string]core.Track
}

func (m fakeMatcher) Match(_ context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	if libID, ok := m.owned[ext.ExternalID]; ok {
		md := m.meta[ext.ExternalID]
		return core.MatchResult{
			Status: core.MatchInLibrary, LibraryTrackID: libID, Method: core.MatchISRC, Confidence: 1.0,
			ArtistID: md.ArtistID, AlbumID: md.AlbumID, CoverArtID: md.CoverArtID,
		}, nil
	}
	return core.MatchResult{Status: core.MatchNotInLibrary}, nil
}

// ---------------------------------------------------------------------------
// fakeDownloader
// ---------------------------------------------------------------------------

type fakeDownloader struct {
	calls []core.DownloadRequest
}

func (d *fakeDownloader) Enqueue(_ context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	d.calls = append(d.calls, req)
	return core.DownloadJob{ID: "dl-" + req.ExternalID, Source: req.Source, ExternalID: req.ExternalID}, nil
}

// ---------------------------------------------------------------------------
// fakeLibraryWriter
// ---------------------------------------------------------------------------

type fakeLibraryWriter struct {
	playlists []core.Playlist
	addCalls  []struct {
		playlistID string
		trackIDs   []string
	}
	createErr error
	addErr    error
	nextID    int
}

func (f *fakeLibraryWriter) CreatePlaylist(_ context.Context, name string) (core.Playlist, error) {
	if f.createErr != nil {
		return core.Playlist{}, f.createErr
	}
	f.nextID++
	pl := core.Playlist{ID: fmt.Sprintf("pl-%d", f.nextID), Name: name}
	f.playlists = append(f.playlists, pl)
	return pl, nil
}

func (f *fakeLibraryWriter) AddTracksToPlaylist(_ context.Context, playlistID string, trackIDs []string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.addCalls = append(f.addCalls, struct {
		playlistID string
		trackIDs   []string
	}{playlistID, trackIDs})
	return nil
}

// ---------------------------------------------------------------------------
// memStore
// ---------------------------------------------------------------------------

type memRow struct {
	SyncedRow
}

type memStore struct {
	rows  map[string]*memRow          // id → row
	index map[string]string           // "source:externalID" → id
}

func newMemStore() *memStore {
	return &memStore{
		rows:  make(map[string]*memRow),
		index: make(map[string]string),
	}
}

func (ms *memStore) Upsert(_ context.Context, p core.SyncedPlaylist, tracksJSON string, createdAt int64) (string, error) {
	key := p.Source + ":" + p.ExternalID
	if existingID, ok := ms.index[key]; ok {
		// update tracklist
		row := ms.rows[existingID]
		row.TracksJSON = tracksJSON
		row.Name = p.Name
		row.CoverURL = p.CoverURL
		return existingID, nil
	}
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("auto-%d", len(ms.rows)+1)
	}
	mode := p.Mode
	if mode == "" {
		mode = "synced"
	}
	ms.rows[id] = &memRow{SyncedRow{
		ID:         id,
		Source:     p.Source,
		ExternalID: p.ExternalID,
		Name:       p.Name,
		CoverURL:   p.CoverURL,
		TracksJSON: tracksJSON,
		CreatedAt:  createdAt,
		Mode:       mode,
	}}
	ms.index[key] = id
	return id, nil
}

func (ms *memStore) Get(_ context.Context, id string) (SyncedRow, error) {
	r, ok := ms.rows[id]
	if !ok {
		return SyncedRow{}, fmt.Errorf("synced playlist %q not found", id)
	}
	return r.SyncedRow, nil
}

func (ms *memStore) List(_ context.Context) ([]SyncedRow, error) {
	out := make([]SyncedRow, 0, len(ms.rows))
	for _, r := range ms.rows {
		row := r.SyncedRow
		// Mimic the real store: pre-count tracks so the service List path doesn't
		// need to unmarshal TracksJSON.
		if row.TracksJSON != "" {
			var tracks []core.ExternalResult
			_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)
			row.TrackCount = len(tracks)
		}
		out = append(out, row)
	}
	return out, nil
}

func (ms *memStore) ListDue(_ context.Context, now int64) ([]SyncedRow, error) {
	var out []SyncedRow
	for _, r := range ms.rows {
		if r.SyncEnabled && r.SyncIntervalSec > 0 {
			due := r.LastSyncedAt + int64(r.SyncIntervalSec)
			if due <= now {
				out = append(out, r.SyncedRow)
			}
		}
	}
	return out, nil
}

func (ms *memStore) UpdateTracks(_ context.Context, id, name, coverURL, tracksJSON string, lastSyncedAt int64) error {
	r, ok := ms.rows[id]
	if !ok {
		return fmt.Errorf("synced playlist %q not found", id)
	}
	r.Name = name
	r.CoverURL = coverURL
	r.TracksJSON = tracksJSON
	r.LastSyncedAt = lastSyncedAt
	return nil
}

func (ms *memStore) UpdateSettings(_ context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error {
	r, ok := ms.rows[id]
	if !ok {
		return fmt.Errorf("synced playlist %q not found", id)
	}
	r.SyncEnabled = enabled
	r.SyncIntervalSec = intervalSec
	r.AutoDownload = autoDownload
	return nil
}

func (ms *memStore) Delete(_ context.Context, id string) error {
	r, ok := ms.rows[id]
	if !ok {
		return fmt.Errorf("synced playlist %q not found", id)
	}
	key := r.Source + ":" + r.ExternalID
	delete(ms.index, key)
	delete(ms.rows, id)
	return nil
}

// setLastSynced is a helper for Task 4 scheduler tests.
func (ms *memStore) setLastSynced(id string, ts int64) {
	if r, ok := ms.rows[id]; ok {
		r.LastSyncedAt = ts
	}
}

// ---------------------------------------------------------------------------
// fakeLibraryReader
// ---------------------------------------------------------------------------

type fakeLibraryReader struct {
	playlists []core.Playlist
	// perID maps playlist id to a full Playlist with tracks
	perID map[string]core.Playlist
	// errPerID maps playlist id to an error for GetPlaylist
	errPerID map[string]error
}

func (f *fakeLibraryReader) GetPlaylists(_ context.Context) ([]core.Playlist, error) {
	return f.playlists, nil
}
func (f *fakeLibraryReader) GetPlaylist(_ context.Context, id string) (core.Playlist, error) {
	if err, ok := f.errPerID[id]; ok {
		return core.Playlist{}, err
	}
	if pl, ok := f.perID[id]; ok {
		return pl, nil
	}
	return core.Playlist{}, fmt.Errorf("playlist %q not found", id)
}

// ---------------------------------------------------------------------------
// fakeSettingsStore
// ---------------------------------------------------------------------------

type fakeSettingsStore struct {
	settings map[string]string
}

func newFakeSettings() *fakeSettingsStore {
	return &fakeSettingsStore{settings: make(map[string]string)}
}

func (f *fakeSettingsStore) GetSetting(_ context.Context, key string) (string, error) {
	v, ok := f.settings[key]
	if !ok {
		return "", fmt.Errorf("no setting %q", key)
	}
	return v, nil
}

func (f *fakeSettingsStore) UpsertSetting(_ context.Context, key, value string) error {
	f.settings[key] = value
	return nil
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestImportThenDetailComputesOwnership(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	m := fakeMatcher{
		owned: map[string]string{"t1": "L1"}, // t2 missing
		meta:  map[string]core.Track{"t1": {ArtistID: "ar1", AlbumID: "al1", CoverArtID: "cv1"}},
	}
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "https://open.spotify.com/playlist/PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if det.Name != "Chill" || det.TotalCount != 2 || det.OwnedCount != 1 {
		t.Fatalf("bad import detail: %+v", det)
	}
	if det.Tracks[0].State != core.CoverageFull || det.Tracks[1].State != core.CoverageNone {
		t.Fatalf("bad ownership: %+v", det.Tracks)
	}
	// The owned row's synthesized LibraryTrack must thread the matched candidate's
	// artist/album/cover ids so the FE renders clickable artist + album links.
	if lt := det.Tracks[0].LibraryTrack; lt == nil || lt.ArtistID != "ar1" || lt.AlbumID != "al1" || lt.CoverArtID != "cv1" {
		t.Fatalf("owned row LibraryTrack metadata not threaded: %+v", lt)
	}
}

// TestImportStampsLastSyncedAt is the regression for Bug 7: after Import, the row's
// last_synced_at must be set to the import time (an import IS a sync — we just
// fetched the live tracklist). UpsertSyncedPlaylist only writes created_at, so
// without the explicit stamp the UI reads "Never synced" right after import.
func TestImportStampsLastSyncedAt(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	const importTime int64 = 1717_000_000
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return importTime }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if det.LastSyncedAt != importTime {
		t.Fatalf("detail.lastSyncedAt = %d, want %d (import should stamp it, not leave 0/'Never synced')", det.LastSyncedAt, importTime)
	}
	row, err := store.Get(context.Background(), det.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.LastSyncedAt != importTime {
		t.Fatalf("row.LastSyncedAt = %d, want %d", row.LastSyncedAt, importTime)
	}
}

func TestSyncReplacesTracklist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)
	// Spotify playlist gains a track; sync must reflect it.
	src.playlists["PL"] = core.ExternalPlaylist{Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1"), track("t3")}}
	det2, err := svc.Sync(context.Background(), det.ID)
	if err != nil || det2.TotalCount != 2 {
		t.Fatalf("sync should reflect 2 tracks, got %+v err=%v", det2, err)
	}
}

func TestImportSamePlaylistTwiceReturnsSameID(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det1, err1 := svc.Import(context.Background(), "spotify:playlist:PL", false)
	det2, err2 := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err1 != nil || err2 != nil {
		t.Fatalf("import errors: %v / %v", err1, err2)
	}
	if det1.ID != det2.ID {
		t.Fatalf("re-import should return same id: got %q vs %q", det1.ID, det2.ID)
	}
}

func TestImportWithDownloadMissingEnqueues(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Mix", Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	dl := &fakeDownloader{}
	svc := NewService(src, fakeMatcher{}, dl, newMemStore(), nil, func() int64 { return 100 }, seqID())
	_, err := svc.Import(context.Background(), "https://open.spotify.com/playlist/PL", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dl.calls) != 2 {
		t.Fatalf("expected 2 enqueue calls, got %d", len(dl.calls))
	}
}

func TestParsePlaylistIDVariants(t *testing.T) {
	src := &fakeSource{}
	cases := []struct {
		url  string
		want string
		ok   bool
	}{
		{"spotify:playlist:ABC123", "ABC123", true},
		{"https://open.spotify.com/playlist/ABC123", "ABC123", true},
		{"https://open.spotify.com/playlist/ABC123?si=xyz", "ABC123", true},
		{"https://open.spotify.com/track/XYZ", "", false},
		{"not-a-url", "", false},
	}
	for _, c := range cases {
		got, ok := src.ParsePlaylistID(c.url)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePlaylistID(%q) = (%q, %v), want (%q, %v)", c.url, got, ok, c.want, c.ok)
		}
	}
}

func TestListReturnsAllImported(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL1": {Source: "spotify", ExternalID: "PL1", Name: "A", Tracks: []core.ExternalResult{track("t1")}},
		"PL2": {Source: "spotify", ExternalID: "PL2", Name: "B", Tracks: []core.ExternalResult{track("t2")}},
	}}
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	svc.Import(context.Background(), "spotify:playlist:PL1", false) //nolint
	svc.Import(context.Background(), "spotify:playlist:PL2", false) //nolint
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 playlists, got %d", len(list))
	}
}

func TestDeleteRemovesPlaylist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "X", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err := svc.Delete(context.Background(), det.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List(context.Background())
	if len(list) != 0 {
		t.Fatalf("expected 0 playlists after delete, got %d", len(list))
	}
}

func TestSyncErrorPreservesTracklist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Keep", Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if det.TotalCount != 2 {
		t.Fatalf("expected 2 tracks after import, got %d", det.TotalCount)
	}

	// Simulate the source becoming unavailable.
	src.err = fmt.Errorf("spotify unavailable")

	_, syncErr := svc.Sync(context.Background(), det.ID)
	if syncErr == nil {
		t.Fatal("Sync should have returned an error when source fails")
	}

	// The stored tracklist must be unchanged.
	after, err := svc.Detail(context.Background(), det.ID)
	if err != nil {
		t.Fatalf("Detail after failed sync: %v", err)
	}
	if after.TotalCount != 2 {
		t.Fatalf("TotalCount should still be 2 after failed sync, got %d", after.TotalCount)
	}
}

// TestDetailCoverURLFromExternalTrack is the regression for the playlistsync Detail path:
// AlbumDetailTrack.CoverURL must be populated from the external track's CoverURL for
// a track that is NOT in the library (CoverageNone / external row).
func TestDetailCoverURLFromExternalTrack(t *testing.T) {
	const wantCover = "https://i.scdn.co/image/abc123"
	extTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-missing", Title: "Missing Track",
		Artist: "Some Artist", Album: "Some Album", Type: core.EntityTrack,
		CoverURL: wantCover,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Cover Test", Tracks: []core.ExternalResult{extTrack}},
	}}
	// No tracks owned — the track should appear as CoverageNone with CoverURL from ext.
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	got := det.Tracks[0]
	if got.State != core.CoverageNone {
		t.Fatalf("expected CoverageNone, got %v", got.State)
	}
	if got.CoverURL != wantCover {
		t.Fatalf("Detail track CoverURL = %q, want %q (external track cover not propagated)", got.CoverURL, wantCover)
	}
}

// TestDetailCoverURLOwnedTrackFallsBackToExternal asserts that even for an owned
// (CoverageFull) track, Detail propagates the external CoverURL so the FE has art
// even before the library scanner provides a local cover.
func TestDetailCoverURLOwnedTrackFallsBackToExternal(t *testing.T) {
	const wantCover = "https://i.scdn.co/image/owned123"
	extTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-owned", Title: "Owned Track",
		Artist: "Artist", Album: "Album", Type: core.EntityTrack,
		CoverURL: wantCover,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Owned Cover Test", Tracks: []core.ExternalResult{extTrack}},
	}}
	m := fakeMatcher{owned: map[string]string{"t-owned": "lib-1"}}
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	got := det.Tracks[0]
	if got.State != core.CoverageFull {
		t.Fatalf("expected CoverageFull, got %v", got.State)
	}
	if got.CoverURL != wantCover {
		t.Fatalf("Detail owned track CoverURL = %q, want %q (external cover not propagated)", got.CoverURL, wantCover)
	}
}

// TestListTrackCountViaSQLCount verifies that List returns correct TrackCount
// values. The memStore.List pre-populates TrackCount (mirroring the real store's
// json_array_length SQL path), so the service must not double-unmarshal TracksJSON.
func TestListTrackCountViaSQLCount(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL1": {Source: "spotify", ExternalID: "PL1", Name: "Three", Tracks: []core.ExternalResult{track("a"), track("b"), track("c")}},
		"PL2": {Source: "spotify", ExternalID: "PL2", Name: "One", Tracks: []core.ExternalResult{track("x")}},
	}}
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	svc.Import(context.Background(), "spotify:playlist:PL1", false) //nolint
	svc.Import(context.Background(), "spotify:playlist:PL2", false) //nolint

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int, len(list))
	for _, pl := range list {
		counts[pl.Name] = pl.TrackCount
	}
	if counts["Three"] != 3 {
		t.Fatalf("PL1 TrackCount = %d, want 3", counts["Three"])
	}
	if counts["One"] != 1 {
		t.Fatalf("PL2 TrackCount = %d, want 1", counts["One"])
	}
}

// TestDetailCarriesArtistAlbumExternalIDs asserts that Detail propagates
// ArtistExternalID and AlbumExternalID from the stored ExternalResult onto both
// owned (CoverageFull) and missing (CoverageNone) AlbumDetailTrack rows, so the
// FE can render clickable artist/album links on synced-playlist rows.
func TestDetailCarriesArtistAlbumExternalIDs(t *testing.T) {
	ownedTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-owned", Title: "Owned Track", Artist: "Artist", Album: "Album",
		Type: core.EntityTrack, ArtistExternalID: "sp-artist-1", AlbumExternalID: "sp-album-1",
	}
	missingTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-missing", Title: "Missing Track", Artist: "Artist", Album: "Album",
		Type: core.EntityTrack, ArtistExternalID: "sp-artist-2", AlbumExternalID: "sp-album-2",
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Ext ID Test", Tracks: []core.ExternalResult{ownedTrack, missingTrack}},
	}}
	m := fakeMatcher{owned: map[string]string{"t-owned": "lib-owned-1"}}
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(det.Tracks))
	}

	// Owned track.
	owned := det.Tracks[0]
	if owned.State != core.CoverageFull {
		t.Fatalf("track[0] expected CoverageFull, got %v", owned.State)
	}
	if owned.ArtistExternalID != "sp-artist-1" {
		t.Fatalf("owned track ArtistExternalID = %q, want %q", owned.ArtistExternalID, "sp-artist-1")
	}
	if owned.AlbumExternalID != "sp-album-1" {
		t.Fatalf("owned track AlbumExternalID = %q, want %q", owned.AlbumExternalID, "sp-album-1")
	}

	// Missing track.
	missing := det.Tracks[1]
	if missing.State != core.CoverageNone {
		t.Fatalf("track[1] expected CoverageNone, got %v", missing.State)
	}
	if missing.ArtistExternalID != "sp-artist-2" {
		t.Fatalf("missing track ArtistExternalID = %q, want %q", missing.ArtistExternalID, "sp-artist-2")
	}
	if missing.AlbumExternalID != "sp-album-2" {
		t.Fatalf("missing track AlbumExternalID = %q, want %q", missing.AlbumExternalID, "sp-album-2")
	}
}

// ---------------------------------------------------------------------------
// ImportOnce tests
// ---------------------------------------------------------------------------

// TestImportOnceCreatesManagedPlaylist asserts that ImportOnce creates a mode='once'
// managed playlist (not a Navidrome library playlist), sets cover_url, stores all
// tracks, enqueues missing tracks, and returns a SyncedPlaylistDetail.
func TestImportOnceCreatesManagedPlaylist(t *testing.T) {
	ownedTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-owned", Title: "Owned Track",
		Artist: "Artist", Album: "Album", ISRC: "ISRC1", DurationMs: 210000,
		Type: core.EntityTrack,
	}
	missingTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "t-missing", Title: "Missing Track",
		Artist: "Artist2", Album: "Album2", ISRC: "ISRC2", DurationMs: 180000,
		Type: core.EntityTrack,
	}
	const coverURL = "https://i.scdn.co/image/playlist-cover"
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "My Import", CoverURL: coverURL,
			Tracks: []core.ExternalResult{ownedTrack, missingTrack}},
	}}
	matcher := fakeMatcher{owned: map[string]string{"t-owned": "lib-owned-1"}}
	dl := &fakeDownloader{}
	store := newMemStore()
	svc := NewService(src, matcher, dl, store, nil, func() int64 { return 100 }, seqID())

	det, err := svc.ImportOnce(context.Background(), "spotify:playlist:PL")
	if err != nil {
		t.Fatalf("ImportOnce: %v", err)
	}
	// Should be a managed detail (not a Playlist).
	if det.ID == "" {
		t.Fatal("detail has no ID")
	}
	if det.Name != "My Import" {
		t.Fatalf("name = %q, want %q", det.Name, "My Import")
	}
	if det.CoverURL != coverURL {
		t.Fatalf("CoverURL = %q, want %q", det.CoverURL, coverURL)
	}
	if det.Mode != "once" {
		t.Fatalf("Mode = %q, want 'once'", det.Mode)
	}
	if det.TotalCount != 2 {
		t.Fatalf("TotalCount = %d, want 2", det.TotalCount)
	}
	// Missing track (t-missing) should be enqueued; owned track should NOT be.
	if len(dl.calls) != 1 {
		t.Fatalf("expected 1 enqueue call (missing track only), got %d", len(dl.calls))
	}
	if dl.calls[0].ExternalID != "t-missing" {
		t.Fatalf("enqueued track = %q, want 't-missing'", dl.calls[0].ExternalID)
	}
	// No AddToPlaylistID (managed playlists compute ownership live).
	if dl.calls[0].AddToPlaylistID != "" {
		t.Fatalf("AddToPlaylistID should be empty for managed playlists, got %q", dl.calls[0].AddToPlaylistID)
	}
	// The row should exist in store with mode='once'.
	row, err := store.Get(context.Background(), det.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if row.Mode != "once" {
		t.Fatalf("row.Mode = %q, want 'once'", row.Mode)
	}
	if row.CoverURL != coverURL {
		t.Fatalf("row.CoverURL = %q, want %q", row.CoverURL, coverURL)
	}
}

// TestImportOnce_BadURL asserts that ImportOnce returns ErrNotPlaylistURL.
func TestImportOnce_BadURL(t *testing.T) {
	src := &fakeSource{}
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID())
	_, err := svc.ImportOnce(context.Background(), "https://example.com/not-a-playlist")
	if err == nil {
		t.Fatal("expected ErrNotPlaylistURL, got nil")
	}
	if !errors.Is(err, ErrNotPlaylistURL) {
		t.Fatalf("expected ErrNotPlaylistURL, got %v", err)
	}
}

// TestDetailLibrarySourceTrack asserts that Detail treats a stored entry with
// Source=="library" as already-owned (no matching call needed).
func TestDetailLibrarySourceTrack(t *testing.T) {
	libraryEntry := core.ExternalResult{
		Source: "library", ExternalID: "lib-track-99", Title: "Local Track",
		Artist: "Local Artist", Album: "Local Album", DurationMs: 200000,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Mixed", Tracks: []core.ExternalResult{libraryEntry}},
	}}
	// Matcher that would fail if called (should not be called for library entries).
	m := fakeMatcher{}
	store := newMemStore()
	svc := NewService(src, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	tr := det.Tracks[0]
	if tr.State != core.CoverageFull {
		t.Fatalf("library-source track State = %v, want CoverageFull", tr.State)
	}
	if tr.LibraryTrack == nil || tr.LibraryTrack.ID != "lib-track-99" {
		t.Fatalf("library-source track LibraryTrack = %+v", tr.LibraryTrack)
	}
}

// TestAddTrackAppendsAndDedupes asserts AddTrack adds to a mode='once' playlist,
// deduplicates, and enqueues missing spotify tracks.
func TestAddTrackAppendsAndDedupes(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Once", Tracks: []core.ExternalResult{track("t1")}},
	}}
	dl := &fakeDownloader{}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, dl, store, nil, func() int64 { return 100 }, seqID())
	det, _ := svc.ImportOnce(context.Background(), "spotify:playlist:PL")
	initialEnqueues := len(dl.calls) // t1 was enqueued on import

	// Add a new track.
	newTrack := core.ExternalResult{Source: "spotify", ExternalID: "t-new", Title: "New", Type: core.EntityTrack}
	det2, err := svc.AddTrack(context.Background(), det.ID, newTrack)
	if err != nil {
		t.Fatalf("AddTrack: %v", err)
	}
	if det2.TotalCount != 2 {
		t.Fatalf("TotalCount = %d, want 2 after add", det2.TotalCount)
	}
	if len(dl.calls) != initialEnqueues+1 {
		t.Fatalf("expected 1 new enqueue after AddTrack, got %d new", len(dl.calls)-initialEnqueues)
	}

	// Adding the same track again should be a no-op (dedupe).
	det3, err := svc.AddTrack(context.Background(), det.ID, newTrack)
	if err != nil {
		t.Fatalf("AddTrack dedupe: %v", err)
	}
	if det3.TotalCount != 2 {
		t.Fatalf("TotalCount = %d after dedupe, want 2", det3.TotalCount)
	}
	if len(dl.calls) != initialEnqueues+1 {
		t.Fatalf("dedupe should not enqueue again, got %d total calls", len(dl.calls))
	}
}

// TestAddTrackNotEditableOnSyncedPlaylist asserts that AddTrack on a mode='synced'
// playlist returns ErrNotEditable.
func TestAddTrackNotEditableOnSyncedPlaylist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Synced", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)

	newTrack := core.ExternalResult{Source: "spotify", ExternalID: "t-new", Title: "New", Type: core.EntityTrack}
	_, err := svc.AddTrack(context.Background(), det.ID, newTrack)
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("expected ErrNotEditable, got %v", err)
	}
}

// TestRemoveTrackRemovesEntry asserts RemoveTrack removes from a mode='once' playlist.
func TestRemoveTrackRemovesEntry(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Once",
			Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, _ := svc.ImportOnce(context.Background(), "spotify:playlist:PL")

	det2, err := svc.RemoveTrack(context.Background(), det.ID, "spotify", "t1")
	if err != nil {
		t.Fatalf("RemoveTrack: %v", err)
	}
	if det2.TotalCount != 1 {
		t.Fatalf("TotalCount = %d after remove, want 1", det2.TotalCount)
	}
}

// ---------------------------------------------------------------------------
// CreateManaged tests
// ---------------------------------------------------------------------------

// TestCreateManagedCreatesLocalModeOncePlaylist verifies that CreateManaged
// creates a source="local", mode="once" empty managed playlist.
func TestCreateManagedCreatesLocalModeOncePlaylist(t *testing.T) {
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		fakeMatcher{},
		&fakeDownloader{},
		newMemStore(),
		nil,
		func() int64 { return 500 },
		seqID(),
	)
	det, err := svc.CreateManaged(context.Background(), "My New Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}
	if det.ID == "" {
		t.Fatal("detail has no ID")
	}
	if det.Name != "My New Playlist" {
		t.Fatalf("Name = %q, want %q", det.Name, "My New Playlist")
	}
	if det.Source != "local" {
		t.Fatalf("Source = %q, want 'local'", det.Source)
	}
	if det.Mode != "once" {
		t.Fatalf("Mode = %q, want 'once'", det.Mode)
	}
	if det.TotalCount != 0 {
		t.Fatalf("TotalCount = %d, want 0 (empty)", det.TotalCount)
	}
	if det.OwnedCount != 0 {
		t.Fatalf("OwnedCount = %d, want 0", det.OwnedCount)
	}
	// ExternalID should equal ID for local playlists.
	if det.ExternalID != det.ID {
		t.Fatalf("ExternalID = %q, want == ID %q", det.ExternalID, det.ID)
	}
}

// ---------------------------------------------------------------------------
// MigrateLibraryPlaylists tests
// ---------------------------------------------------------------------------

// TestMigrateLibraryPlaylistsMigratesAll verifies that 2 library playlists are
// migrated as source="local" managed playlists with library-source tracks carrying
// title/artist/album/coverArtId. It also checks the flag is set and a second
// call is a no-op.
func TestMigrateLibraryPlaylistsMigratesAll(t *testing.T) {
	pl1 := core.Playlist{
		ID:   "lib-pl-1",
		Name: "Favorites",
		Tracks: []core.Track{
			{ID: "t1", Title: "Song A", Artist: "Artist A", Album: "Album A", DurationMs: 200000, CoverArtID: "cov-1", ISRC: "US12300000001"},
			{ID: "t2", Title: "Song B", Artist: "Artist B", Album: "Album B", DurationMs: 180000, CoverArtID: "cov-2"},
		},
	}
	pl2 := core.Playlist{
		ID:   "lib-pl-2",
		Name: "Workout",
		Tracks: []core.Track{
			{ID: "t3", Title: "Track C", Artist: "Artist C", Album: "Album C", DurationMs: 240000, CoverArtID: "cov-3"},
		},
	}
	libReader := &fakeLibraryReader{
		playlists: []core.Playlist{
			{ID: pl1.ID, Name: pl1.Name},
			{ID: pl2.ID, Name: pl2.Name},
		},
		perID: map[string]core.Playlist{
			pl1.ID: pl1,
			pl2.ID: pl2,
		},
	}
	ss := newFakeSettings()
	store := newMemStore()
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		fakeMatcher{},
		&fakeDownloader{},
		store,
		nil,
		func() int64 { return 1000 },
		seqID(),
	)
	svc.WithLibraryReader(libReader)
	svc.WithSettingsStore(ss)

	if err := svc.MigrateLibraryPlaylists(context.Background()); err != nil {
		t.Fatalf("MigrateLibraryPlaylists: %v", err)
	}

	// Flag should now be "true".
	got, _ := ss.GetSetting(context.Background(), migrationKey)
	if got != "true" {
		t.Fatalf("migrationKey setting = %q, want 'true'", got)
	}

	// Should have 2 managed playlists.
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 managed playlists, got %d", len(list))
	}
	// Check that all playlists are source="local" mode="once".
	for _, pl := range list {
		if pl.Source != "local" {
			t.Errorf("playlist %q: Source = %q, want 'local'", pl.Name, pl.Source)
		}
		if pl.Mode != "once" {
			t.Errorf("playlist %q: Mode = %q, want 'once'", pl.Name, pl.Mode)
		}
	}

	// Check that "Favorites" has correct tracks with library-source entries including CoverArtID.
	var favID string
	for _, pl := range list {
		if pl.Name == "Favorites" {
			favID = pl.ID
			break
		}
	}
	if favID == "" {
		t.Fatal("did not find 'Favorites' playlist after migration")
	}
	det, err := svc.Detail(context.Background(), favID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if det.TotalCount != 2 {
		t.Fatalf("Favorites TotalCount = %d, want 2", det.TotalCount)
	}
	if det.OwnedCount != 2 {
		t.Fatalf("Favorites OwnedCount = %d, want 2 (all owned library tracks)", det.OwnedCount)
	}
	// Check first track carries CoverArtID.
	tr0 := det.Tracks[0]
	if tr0.LibraryTrack == nil {
		t.Fatal("Tracks[0].LibraryTrack is nil")
	}
	if tr0.LibraryTrack.CoverArtID != "cov-1" {
		t.Fatalf("Tracks[0].LibraryTrack.CoverArtID = %q, want 'cov-1'", tr0.LibraryTrack.CoverArtID)
	}
	if tr0.LibraryTrack.Title != "Song A" {
		t.Fatalf("Tracks[0].LibraryTrack.Title = %q, want 'Song A'", tr0.LibraryTrack.Title)
	}

	// Second call must be a no-op (flag already set).
	if err := svc.MigrateLibraryPlaylists(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}
	list2, _ := svc.List(context.Background())
	if len(list2) != 2 {
		t.Fatalf("second call should not add more playlists; got %d", len(list2))
	}
}

// TestMigrateLibraryPlaylistsPerPlaylistError verifies that an error for one
// playlist does not stop the migration of others, and the flag is still set.
func TestMigrateLibraryPlaylistsPerPlaylistError(t *testing.T) {
	libReader := &fakeLibraryReader{
		playlists: []core.Playlist{
			{ID: "lib-ok", Name: "Good"},
			{ID: "lib-err", Name: "Bad"},
		},
		perID: map[string]core.Playlist{
			"lib-ok": {ID: "lib-ok", Name: "Good", Tracks: []core.Track{{ID: "t1", Title: "Track 1"}}},
		},
		errPerID: map[string]error{
			"lib-err": fmt.Errorf("subsonic: timeout"),
		},
	}
	ss := newFakeSettings()
	store := newMemStore()
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		fakeMatcher{},
		&fakeDownloader{},
		store,
		nil,
		func() int64 { return 2000 },
		seqID(),
	)
	svc.WithLibraryReader(libReader)
	svc.WithSettingsStore(ss)

	if err := svc.MigrateLibraryPlaylists(context.Background()); err != nil {
		t.Fatalf("MigrateLibraryPlaylists: %v", err)
	}
	// Flag must be set.
	got, _ := ss.GetSetting(context.Background(), migrationKey)
	if got != "true" {
		t.Fatalf("migrationKey = %q, want 'true'", got)
	}
	// Only the good playlist should be migrated.
	list, _ := svc.List(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 migrated playlist (bad one skipped), got %d", len(list))
	}
	if list[0].Name != "Good" {
		t.Fatalf("expected 'Good', got %q", list[0].Name)
	}
}

// TestDetailLibrarySourceTrackCoverArtID asserts that Detail carries CoverArtID
// from a stored library-source entry onto the synthesized LibraryTrack.
func TestDetailLibrarySourceTrackCoverArtID(t *testing.T) {
	libraryEntry := core.ExternalResult{
		Source:     "library",
		ExternalID: "lib-track-55",
		Title:      "Local Track With Cover",
		Artist:     "Artist",
		Album:      "Album",
		DurationMs: 200000,
		CoverArtID: "cover-art-xyz",
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Cover Test", Tracks: []core.ExternalResult{libraryEntry}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	tr := det.Tracks[0]
	if tr.State != core.CoverageFull {
		t.Fatalf("State = %v, want CoverageFull", tr.State)
	}
	if tr.LibraryTrack == nil {
		t.Fatal("LibraryTrack is nil")
	}
	if tr.LibraryTrack.CoverArtID != "cover-art-xyz" {
		t.Fatalf("LibraryTrack.CoverArtID = %q, want 'cover-art-xyz'", tr.LibraryTrack.CoverArtID)
	}
}
