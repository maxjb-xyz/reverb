package playlistsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/resolver"
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
		return SyncedRow{}, ErrNotFound
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
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return importTime }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, dl, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, matcher, dl, store, nil, func() int64 { return 100 }, seqID(), nil)

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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)
	_, err := svc.ImportOnce(context.Background(), "https://example.com/not-a-playlist")
	if err == nil {
		t.Fatal("expected ErrNotPlaylistURL, got nil")
	}
	if !errors.Is(err, ErrNotPlaylistURL) {
		t.Fatalf("expected ErrNotPlaylistURL, got %v", err)
	}
}

// TestDetailLibrarySourceTrack asserts that Detail re-resolves a stored
// Source=="library" track via the matcher at read time and returns the library
// track ID returned by the matcher (not the frozen stored ExternalID).
func TestDetailLibrarySourceTrack(t *testing.T) {
	const storedID = "lib-track-99"
	libraryEntry := core.ExternalResult{
		Source: "library", ExternalID: storedID, Title: "Local Track",
		Artist: "Local Artist", Album: "Local Album", DurationMs: 200000,
		Type: core.EntityTrack,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Mixed", Tracks: []core.ExternalResult{libraryEntry}},
	}}
	// Matcher confirms the track exists in the library (same id — same backend).
	m := fakeMatcher{owned: map[string]string{storedID: storedID}}
	store := newMemStore()
	svc := NewService(src, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	if tr.LibraryTrack == nil || tr.LibraryTrack.ID != storedID {
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
	svc := NewService(src, fakeMatcher{}, dl, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
		nil,
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
// nil-src (no Spotify) tests
// ---------------------------------------------------------------------------

// TestNilSrcSpotifyMethodsReturnErrSpotifyNotConfigured asserts that Import,
// ImportOnce, and Sync all return ErrSpotifyNotConfigured when the service was
// constructed without a PlaylistSource (src=nil). All other managed-playlist
// operations must work normally.
func TestNilSrcSpotifyMethodsReturnErrSpotifyNotConfigured(t *testing.T) {
	svc := NewService(nil, fakeMatcher{}, &fakeDownloader{}, newMemStore(), nil, func() int64 { return 100 }, seqID(), nil)

	_, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if !errors.Is(err, ErrSpotifyNotConfigured) {
		t.Fatalf("Import with nil src: want ErrSpotifyNotConfigured, got %v", err)
	}

	_, err = svc.ImportOnce(context.Background(), "spotify:playlist:PL")
	if !errors.Is(err, ErrSpotifyNotConfigured) {
		t.Fatalf("ImportOnce with nil src: want ErrSpotifyNotConfigured, got %v", err)
	}

	// Seed a row so Sync can attempt to look up the playlist.
	store := newMemStore()
	svc2 := NewService(nil, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	_, err = svc2.Sync(context.Background(), "nonexistent-id")
	if !errors.Is(err, ErrSpotifyNotConfigured) {
		t.Fatalf("Sync with nil src: want ErrSpotifyNotConfigured, got %v", err)
	}
}

// TestNilSrcManagedPlaylistOpsWork asserts that CreateManaged, List, Detail,
// AddTrack, and RemoveTrack all work when the service has a nil PlaylistSource.
func TestNilSrcManagedPlaylistOpsWork(t *testing.T) {
	store := newMemStore()
	// Detail re-resolves library-source tracks via the matcher, so the added
	// lib-t1 entry must be in the matcher's owned map to resolve as owned.
	m := fakeMatcher{owned: map[string]string{"lib-t1": "lib-t1"}}
	svc := NewService(nil, m, &fakeDownloader{}, store, nil, func() int64 { return 200 }, seqID(), nil)

	// CreateManaged works.
	det, err := svc.CreateManaged(context.Background(), "No Spotify Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}
	if det.Name != "No Spotify Playlist" {
		t.Fatalf("Name = %q, want 'No Spotify Playlist'", det.Name)
	}
	if det.Source != "local" || det.Mode != "once" {
		t.Fatalf("unexpected Source=%q Mode=%q", det.Source, det.Mode)
	}

	// List works.
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: want 1 playlist, got %d", len(list))
	}

	// Detail works.
	got, err := svc.Detail(context.Background(), det.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.ID != det.ID {
		t.Fatalf("Detail ID mismatch")
	}

	// AddTrack works (library entry — Detail re-resolves it via the matcher,
	// which has lib-t1 owned, so it stays CoverageFull).
	entry := core.ExternalResult{Source: "library", ExternalID: "lib-t1", Title: "Track 1", Type: core.EntityTrack}
	det2, err := svc.AddTrack(context.Background(), det.ID, entry)
	if err != nil {
		t.Fatalf("AddTrack: %v", err)
	}
	if det2.TotalCount != 1 {
		t.Fatalf("TotalCount = %d after AddTrack, want 1", det2.TotalCount)
	}
	if det2.OwnedCount != 1 {
		t.Fatalf("OwnedCount = %d after AddTrack, want 1 (library track re-resolved as owned)", det2.OwnedCount)
	}
	if det2.Tracks[0].State != core.CoverageFull {
		t.Fatalf("Tracks[0].State = %v after AddTrack, want CoverageFull", det2.Tracks[0].State)
	}

	// RemoveTrack works.
	det3, err := svc.RemoveTrack(context.Background(), det.ID, "library", "lib-t1")
	if err != nil {
		t.Fatalf("RemoveTrack: %v", err)
	}
	if det3.TotalCount != 0 {
		t.Fatalf("TotalCount = %d after RemoveTrack, want 0", det3.TotalCount)
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
	// Matcher must know about all migrated track IDs so Detail re-resolves them as owned.
	// This mirrors the real scenario: the live backend still has these tracks, so the
	// matcher returns MatchInLibrary with the same IDs and the correct CoverArtIDs.
	migrateMatcher := fakeMatcher{
		owned: map[string]string{"t1": "t1", "t2": "t2", "t3": "t3"},
		meta: map[string]core.Track{
			"t1": {CoverArtID: "cov-1"},
			"t2": {CoverArtID: "cov-2"},
			"t3": {CoverArtID: "cov-3"},
		},
	}
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		migrateMatcher,
		&fakeDownloader{},
		store,
		nil,
		func() int64 { return 1000 },
		seqID(),
		nil,
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
	// Check first track: CoverArtID comes from matcher (re-resolved, not frozen stored value).
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
		nil,
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

// ---------------------------------------------------------------------------
// Rename tests
// ---------------------------------------------------------------------------

func TestServiceRename(t *testing.T) {
	now := int64(1000)
	stor := newMemStore()
	id := "pl1"
	stor.rows[id] = &memRow{SyncedRow{ID: id, Name: "Old", CoverURL: "cover.jpg", TracksJSON: "[]", Mode: "once"}}
	svc := NewService(nil, nil, nil, stor, nil, func() int64 { return now }, func() string { return id }, nil)
	det, err := svc.Rename(context.Background(), id, "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if det.Name != "New Name" {
		t.Errorf("Name = %q, want %q", det.Name, "New Name")
	}
	if stor.rows[id].Name != "New Name" {
		t.Errorf("stored name = %q", stor.rows[id].Name)
	}
}

func TestServiceRenameEmptyName(t *testing.T) {
	stor := newMemStore()
	id := "pl1"
	stor.rows[id] = &memRow{SyncedRow{ID: id, Name: "Old", Mode: "once"}}
	svc := NewService(nil, nil, nil, stor, nil, func() int64 { return 0 }, func() string { return id }, nil)
	_, err := svc.Rename(context.Background(), id, "   ")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestServiceRenameAllModes(t *testing.T) {
	for _, mode := range []string{"local", "once", "synced"} {
		t.Run(mode, func(t *testing.T) {
			stor := newMemStore()
			id := "pl-" + mode
			stor.rows[id] = &memRow{SyncedRow{ID: id, Name: "Old", TracksJSON: "[]", Mode: mode}}
			svc := NewService(nil, nil, nil, stor, nil, func() int64 { return 0 }, func() string { return id }, nil)
			det, err := svc.Rename(context.Background(), id, "New")
			if err != nil {
				t.Fatalf("mode %q: unexpected error: %v", mode, err)
			}
			if det.Name != "New" {
				t.Errorf("mode %q: Name = %q", mode, det.Name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SetCover tests
// ---------------------------------------------------------------------------

func TestSetCoverUpdatesCoverURL(t *testing.T) {
	store := newMemStore()
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		fakeMatcher{}, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(), nil,
	)
	det, err := svc.CreateManaged(context.Background(), "My Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}

	const newURL = "/api/v1/playlists/pl-1/cover?v=1234"
	det2, err := svc.SetCover(context.Background(), det.ID, newURL)
	if err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	if det2.CoverURL != newURL {
		t.Fatalf("CoverURL = %q, want %q", det2.CoverURL, newURL)
	}

	// Verify persisted.
	row, err := store.Get(context.Background(), det.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if row.CoverURL != newURL {
		t.Fatalf("stored CoverURL = %q, want %q", row.CoverURL, newURL)
	}
}

func TestSetCoverNotEditableOnSyncedPlaylist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Synced", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)

	_, err := svc.SetCover(context.Background(), det.ID, "/some/url")
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("expected ErrNotEditable, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReorderTracks tests
// ---------------------------------------------------------------------------

func TestReorderTracksReordersCorrectly(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Once",
			Tracks: []core.ExternalResult{track("t1"), track("t2"), track("t3")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, _ := svc.ImportOnce(context.Background(), "spotify:playlist:PL")

	// Reorder: t3 first, then t1 (t2 not in order → appended at end)
	order := []core.TrackKey{
		{Source: "spotify", ExternalID: "t3"},
		{Source: "spotify", ExternalID: "t1"},
	}
	det2, err := svc.ReorderTracks(context.Background(), det.ID, order)
	if err != nil {
		t.Fatalf("ReorderTracks: %v", err)
	}
	if det2.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3", det2.TotalCount)
	}
	// Check stored order: t3, t1, t2
	row, _ := store.Get(context.Background(), det.ID)
	var stored []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &stored)
	want := []string{"t3", "t1", "t2"}
	for i, w := range want {
		if stored[i].ExternalID != w {
			t.Errorf("stored[%d].ExternalID = %q, want %q", i, stored[i].ExternalID, w)
		}
	}
}

func TestReorderTracksIgnoresUnknownKeys(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Once",
			Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, _ := svc.ImportOnce(context.Background(), "spotify:playlist:PL")

	order := []core.TrackKey{
		{Source: "spotify", ExternalID: "no-such"},
		{Source: "spotify", ExternalID: "t2"},
	}
	det2, err := svc.ReorderTracks(context.Background(), det.ID, order)
	if err != nil {
		t.Fatalf("ReorderTracks: %v", err)
	}
	if det2.TotalCount != 2 {
		t.Fatalf("TotalCount = %d, want 2", det2.TotalCount)
	}
	// Expected order: t2 first (it was in order), then t1 (remaining)
	row, _ := store.Get(context.Background(), det.ID)
	var stored []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &stored)
	if stored[0].ExternalID != "t2" || stored[1].ExternalID != "t1" {
		t.Errorf("stored order = [%s, %s], want [t2, t1]", stored[0].ExternalID, stored[1].ExternalID)
	}
}

func TestReorderTracksNotEditableOnSyncedPlaylist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Synced", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)

	_, err := svc.ReorderTracks(context.Background(), det.ID, []core.TrackKey{{Source: "spotify", ExternalID: "t1"}})
	if !errors.Is(err, ErrNotEditable) {
		t.Fatalf("expected ErrNotEditable, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDetailLibrarySourceTrackCoverArtID (existing, moved down)
// ---------------------------------------------------------------------------

// TestDetailSetsKeyOnAllRows asserts that Detail sets Key on ALL track rows:
// library-source, matched-spotify, and missing (CoverageNone).
// Only the missing track should have ExternalRef set.
func TestDetailSetsKeyOnAllRows(t *testing.T) {
	libraryTrack := core.ExternalResult{
		Source: "library", ExternalID: "lib-t1", Title: "Library Track",
		Artist: "Artist A", Album: "Album A", DurationMs: 180000,
		Type: core.EntityTrack,
	}
	matchedTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "sp-t2", Title: "Matched Track",
		Artist: "Artist B", Album: "Album B", DurationMs: 200000,
		Type: core.EntityTrack,
	}
	missingTrack := core.ExternalResult{
		Source: "spotify", ExternalID: "sp-t3", Title: "Missing Track",
		Artist: "Artist C", Album: "Album C", DurationMs: 210000,
		Type: core.EntityTrack,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Key Test",
			Tracks: []core.ExternalResult{libraryTrack, matchedTrack, missingTrack}},
	}}
	// lib-t1 must also be in the matcher so the library-source re-resolve succeeds.
	m := fakeMatcher{owned: map[string]string{"lib-t1": "lib-t1", "sp-t2": "lib-match-1"}}
	store := newMemStore()
	svc := NewService(src, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, err := svc.Import(context.Background(), "spotify:playlist:PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(det.Tracks))
	}

	// Library-source track: re-resolved via matcher, Key preserves stored source+externalID.
	libRow := det.Tracks[0]
	if libRow.State != core.CoverageFull {
		t.Fatalf("Tracks[0] expected CoverageFull, got %v", libRow.State)
	}
	// LibraryTrack must be synthesized from the matcher's returned id.
	if libRow.LibraryTrack == nil {
		t.Fatal("Tracks[0] (library-source): LibraryTrack must be non-nil after re-resolution")
	}
	if libRow.LibraryTrack.ID != "lib-t1" {
		t.Fatalf("Tracks[0].LibraryTrack.ID = %q, want matcher-returned 'lib-t1'", libRow.LibraryTrack.ID)
	}
	if libRow.Key == nil {
		t.Fatal("Tracks[0] (library-source): Key must be non-nil")
	}
	if libRow.Key.Source != "library" || libRow.Key.ExternalID != "lib-t1" {
		t.Fatalf("Tracks[0].Key = %+v, want {library, lib-t1}", libRow.Key)
	}
	if libRow.ExternalRef != nil {
		t.Fatal("Tracks[0] (library-source): ExternalRef must be nil (only set on missing rows)")
	}

	// Matched-spotify track.
	matchedRow := det.Tracks[1]
	if matchedRow.State != core.CoverageFull {
		t.Fatalf("Tracks[1] expected CoverageFull, got %v", matchedRow.State)
	}
	if matchedRow.Key == nil {
		t.Fatal("Tracks[1] (matched-spotify): Key must be non-nil")
	}
	if matchedRow.Key.Source != "spotify" || matchedRow.Key.ExternalID != "sp-t2" {
		t.Fatalf("Tracks[1].Key = %+v, want {spotify, sp-t2}", matchedRow.Key)
	}
	if matchedRow.ExternalRef != nil {
		t.Fatal("Tracks[1] (matched-spotify): ExternalRef must be nil (only set on missing rows)")
	}

	// Missing track.
	missingRow := det.Tracks[2]
	if missingRow.State != core.CoverageNone {
		t.Fatalf("Tracks[2] expected CoverageNone, got %v", missingRow.State)
	}
	if missingRow.Key == nil {
		t.Fatal("Tracks[2] (missing): Key must be non-nil")
	}
	if missingRow.Key.Source != "spotify" || missingRow.Key.ExternalID != "sp-t3" {
		t.Fatalf("Tracks[2].Key = %+v, want {spotify, sp-t3}", missingRow.Key)
	}
	if missingRow.ExternalRef == nil {
		t.Fatal("Tracks[2] (missing): ExternalRef must be non-nil")
	}
}

// TestDetailEmptyPlaylistHasNonNilTracks asserts that Detail on a brand-new empty
// managed playlist returns Tracks == [] (non-nil slice), never null. This prevents
// the FE from crashing with "Cannot read properties of null (reading 'filter')".
func TestDetailEmptyPlaylistHasNonNilTracks(t *testing.T) {
	svc := NewService(
		&fakeSource{playlists: map[string]core.ExternalPlaylist{}},
		fakeMatcher{},
		&fakeDownloader{},
		newMemStore(),
		nil,
		func() int64 { return 1000 },
		seqID(),
		nil,
	)
	det, err := svc.CreateManaged(context.Background(), "Empty Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}
	if det.Tracks == nil {
		t.Fatal("Detail.Tracks is nil for an empty playlist; want non-nil empty slice (JSON must be [] not null)")
	}
	if len(det.Tracks) != 0 {
		t.Fatalf("Detail.Tracks len = %d, want 0", len(det.Tracks))
	}
}

// TestDetailLibrarySourceTrackCoverArtID asserts that Detail's synthesized
// LibraryTrack carries the CoverArtID returned by the MATCHER, not the frozen
// CoverArtID stored in TracksJSON. The stored cover is an old/dead id and the
// matcher returns a distinct fresh id; asserting we get the fresh id proves the
// cover comes from re-resolution, not storage (the old frozen-id bug).
func TestDetailLibrarySourceTrackCoverArtID(t *testing.T) {
	const storedID = "lib-track-55"
	const oldCover = "old-dead-cover-id"
	const freshCover = "fresh-cover-id"
	libraryEntry := core.ExternalResult{
		Source:     "library",
		ExternalID: storedID,
		Title:      "Local Track With Cover",
		Artist:     "Artist",
		Album:      "Album",
		DurationMs: 200000,
		CoverArtID: oldCover, // stale stored cover — must NOT leak through
		Type:       core.EntityTrack,
	}
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Cover Test", Tracks: []core.ExternalResult{libraryEntry}},
	}}
	store := newMemStore()
	// Matcher returns a DISTINCT fresh CoverArtID for this track's metadata.
	m := fakeMatcher{
		owned: map[string]string{storedID: storedID},
		meta:  map[string]core.Track{storedID: {CoverArtID: freshCover}},
	}
	svc := NewService(src, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
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
	if tr.LibraryTrack.CoverArtID == oldCover {
		t.Fatalf("LibraryTrack.CoverArtID = %q (frozen stored cover leaked through — re-resolution failed)", oldCover)
	}
	if tr.LibraryTrack.CoverArtID != freshCover {
		t.Fatalf("LibraryTrack.CoverArtID = %q, want fresh %q (matcher-supplied, not frozen storage)", tr.LibraryTrack.CoverArtID, freshCover)
	}
}

// ---------------------------------------------------------------------------
// Task 10: library-source re-resolution at read time
// ---------------------------------------------------------------------------

// TestDetail_LibrarySourceResolvesAtRead asserts that Detail re-resolves a
// stored library-source track via the matcher at read time, returning FRESH
// library IDs from the live backend — NOT the frozen (potentially stale) IDs
// stored in TracksJSON. This proves the backend-swap safety guarantee: even if
// the stored ExternalID and CoverArtID are dead (old backend), the matcher
// re-locates the track by its durable metadata (title/artist/album/isrc) and
// returns the current IDs.
func TestDetail_LibrarySourceResolvesAtRead(t *testing.T) {
	const oldID = "old-backend-track-id"
	const oldCover = "old-cover-art-id"
	const freshID = "new-backend-track-id"
	const freshCover = "new-cover-art-id"
	const freshArtistID = "new-artist-id"
	const freshAlbumID = "new-album-id"

	// Stored entry carries the OLD (now-dead) backend IDs.
	storedEntry := core.ExternalResult{
		Source:     "library",
		ExternalID: oldID,
		Title:      "Resilient Track",
		Artist:     "Some Artist",
		Album:      "Some Album",
		DurationMs: 200000,
		CoverArtID: oldCover,
		Type:       core.EntityTrack,
	}

	store := newMemStore()
	// Seed the playlist directly with the stored (stale) entry.
	tj, _ := json.Marshal([]core.ExternalResult{storedEntry})
	store.rows["pl-1"] = &memRow{SyncedRow{
		ID: "pl-1", Source: "local", ExternalID: "pl-1",
		Name: "Test Playlist", Mode: "once", TracksJSON: string(tj),
	}}
	store.index["local:pl-1"] = "pl-1"

	// Fake matcher: resolves the OLD ExternalID to FRESH IDs (simulates post-swap re-match).
	m := fakeMatcher{
		owned: map[string]string{oldID: freshID},
		meta:  map[string]core.Track{oldID: {ArtistID: freshArtistID, AlbumID: freshAlbumID, CoverArtID: freshCover}},
	}

	svc := NewService(nil, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, err := svc.Detail(context.Background(), "pl-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
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
	// Must use FRESH IDs, NOT frozen old ones.
	if tr.LibraryTrack.ID != freshID {
		t.Fatalf("LibraryTrack.ID = %q, want fresh %q (got frozen old id)", tr.LibraryTrack.ID, freshID)
	}
	if tr.LibraryTrack.CoverArtID != freshCover {
		t.Fatalf("LibraryTrack.CoverArtID = %q, want fresh %q (got frozen old cover)", tr.LibraryTrack.CoverArtID, freshCover)
	}
	if tr.LibraryTrack.ArtistID != freshArtistID {
		t.Fatalf("LibraryTrack.ArtistID = %q, want %q", tr.LibraryTrack.ArtistID, freshArtistID)
	}
	if tr.LibraryTrack.AlbumID != freshAlbumID {
		t.Fatalf("LibraryTrack.AlbumID = %q, want %q", tr.LibraryTrack.AlbumID, freshAlbumID)
	}
	if det.OwnedCount != 1 {
		t.Fatalf("OwnedCount = %d, want 1", det.OwnedCount)
	}
}

// TestDetail_LibrarySourceNoMatch_DegradesToMissing asserts that when a stored
// library-source track fails re-resolution (matcher returns NotInLibrary — the
// track is genuinely gone after a backend swap), Detail degrades it safely to
// CoverageNone with an ExternalRef rather than emitting a dead playable ID.
// OwnedCount must NOT include the unresolved track.
func TestDetail_LibrarySourceNoMatch_DegradesToMissing(t *testing.T) {
	const storedID = "gone-track-id"

	// Stored entry: library source, but the backend no longer has it.
	storedEntry := core.ExternalResult{
		Source:     "library",
		ExternalID: storedID,
		Title:      "Gone Track",
		Artist:     "Some Artist",
		Album:      "Some Album",
		DurationMs: 180000,
		CoverArtID: "dead-cover-id",
		Type:       core.EntityTrack,
	}

	store := newMemStore()
	tj, _ := json.Marshal([]core.ExternalResult{storedEntry})
	store.rows["pl-2"] = &memRow{SyncedRow{
		ID: "pl-2", Source: "local", ExternalID: "pl-2",
		Name: "Test Playlist", Mode: "once", TracksJSON: string(tj),
	}}
	store.index["local:pl-2"] = "pl-2"

	// Fake matcher: nothing matches (backend swap, track is gone).
	m := fakeMatcher{owned: map[string]string{}}

	svc := NewService(nil, m, &fakeDownloader{}, store, nil, func() int64 { return 100 }, seqID(), nil)
	det, err := svc.Detail(context.Background(), "pl-2")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}

	tr := det.Tracks[0]
	// Must degrade to CoverageNone — not emit a dead playable id.
	if tr.State != core.CoverageNone {
		t.Fatalf("State = %v, want CoverageNone (track gone after swap)", tr.State)
	}
	if tr.LibraryTrack != nil {
		t.Fatalf("LibraryTrack must be nil for degraded track, got %+v", tr.LibraryTrack)
	}
	// ExternalRef must be set so the FE can show a download option.
	if tr.ExternalRef == nil {
		t.Fatal("ExternalRef must be non-nil for degraded (no-match) library track")
	}
	if tr.ExternalRef.Source != "library" || tr.ExternalRef.ExternalID != storedID {
		t.Fatalf("ExternalRef = %+v, want {library, %s}", tr.ExternalRef, storedID)
	}
	// OwnedCount must not include this track.
	if det.OwnedCount != 0 {
		t.Fatalf("OwnedCount = %d, want 0 (degraded track must not count as owned)", det.OwnedCount)
	}
}

// ---------------------------------------------------------------------------
// Task 5: CanonicalMinter injection + mint-at-persist + resolve-at-read
// ---------------------------------------------------------------------------

// fakeBindingResolver is a fake BindingResolver for Task-5 tests.
// It records which catalog IDs were resolved and can be swapped to simulate
// a backend swap (the resolved BackendID changes after the swap).
type fakeBindingResolver struct {
	// resolved maps catalogID → Addressing returned.
	resolved map[string]resolver.Addressing
	// calls records the catalog IDs Resolve was called with (in order).
	calls []string
}

func (f *fakeBindingResolver) Resolve(_ context.Context, catalogID string) (resolver.Addressing, error) {
	f.calls = append(f.calls, catalogID)
	addr, ok := f.resolved[catalogID]
	if !ok {
		return resolver.Addressing{}, nil
	}
	return addr, nil
}

func (f *fakeBindingResolver) RefreshLinked(_ context.Context, _ []string) error { return nil }

// fakeCanonicalMinter is a fake CanonicalMinter for Task-5 tests.
// It records which identities were minted and returns predictable IDs.
type fakeCanonicalMinter struct {
	// mintCalls records the catalog.Identity values passed to CanonicalFor.
	mintCalls []catalog.Identity
	// nextID returns the canonical id to assign. Default: "trk_minted".
	nextID func(catalog.Identity) string
	// err, if non-nil, is returned on every call.
	err error
}

func (f *fakeCanonicalMinter) CanonicalFor(_ context.Context, id catalog.Identity) (string, error) {
	f.mintCalls = append(f.mintCalls, id)
	if f.err != nil {
		return "", f.err
	}
	if f.nextID != nil {
		return f.nextID(id), nil
	}
	return "trk_minted", nil
}

// newSvcWithMinter builds a Service with a CanonicalMinter injected.
func newSvcWithMinter(m Matcher, store Store, minter CanonicalMinter) *Service {
	return NewService(nil, m, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(), nil).
		WithCanonicalMinter(minter)
}

// newSvcWithMinterAndResolver builds a Service with both a minter and resolver provider.
func newSvcWithMinterAndResolver(
	m Matcher,
	store Store,
	minter CanonicalMinter,
	resolverFn func() BindingResolver,
) *Service {
	return NewService(nil, m, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(), resolverFn).
		WithCanonicalMinter(minter)
}

// seedLibraryTrackWithCanonicalID seeds a playlist containing one library-source
// track that already has a CanonicalID set (simulates a track persisted after Task 5).
func seedLibraryTrackWithCanonicalID(store *memStore, plID, catalogID, backendID string) {
	entry := core.ExternalResult{
		Source:      "library",
		ExternalID:  backendID,
		CanonicalID: catalogID,
		Title:       "Track Title",
		Artist:      "Artist",
		Album:       "Album",
		DurationMs:  180000,
		Type:        core.EntityTrack,
	}
	tj, _ := json.Marshal([]core.ExternalResult{entry})
	store.rows[plID] = &memRow{SyncedRow{
		ID: plID, Source: "local", ExternalID: plID,
		Name: "Test Playlist", Mode: "once", TracksJSON: string(tj),
	}}
	store.index["local:"+plID] = plID
}

// TestTask5_LibraryTrackWithCanonicalIDResolvesViaResolver asserts that Detail()
// for a library-source track WITH a CanonicalID routes through BindingResolver.Resolve
// (NOT through s.match.Match). The fake resolver returns a fresh BackendID;
// the matcher returns nothing — so if the matcher were called, the track would
// degrade to CoverageNone.
func TestTask5_LibraryTrackWithCanonicalIDResolvesViaResolver(t *testing.T) {
	const catalogID = "trk_abc123"
	const oldBackendID = "old-be-id"
	const freshBackendID = "fresh-be-id"
	const freshCover = "fresh-cover"

	store := newMemStore()
	seedLibraryTrackWithCanonicalID(store, "pl-t5-1", catalogID, oldBackendID)

	// Matcher returns nothing (if called, track would degrade).
	matcherCalled := false
	m := fakeMatcher{owned: map[string]string{}}
	_ = m // not wired — we'll use a tracking matcher below

	trackingMatcher := &trackingMatcherT5{inner: fakeMatcher{owned: map[string]string{}}, called: &matcherCalled}

	resolver := &fakeBindingResolver{
		resolved: map[string]resolver.Addressing{
			catalogID: {BackendID: freshBackendID, CoverArtID: freshCover, Found: true},
		},
	}

	minter := &fakeCanonicalMinter{}
	svc := newSvcWithMinterAndResolver(
		trackingMatcher,
		store,
		minter,
		func() BindingResolver { return resolver },
	)

	det, err := svc.Detail(context.Background(), "pl-t5-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}

	tr := det.Tracks[0]
	// Must be CoverageFull (resolver found it).
	if tr.State != core.CoverageFull {
		t.Fatalf("State = %v, want CoverageFull", tr.State)
	}
	if tr.LibraryTrack == nil {
		t.Fatal("LibraryTrack is nil; resolver path must populate it")
	}
	// Must use the resolver's fresh BackendID, not oldBackendID.
	if tr.LibraryTrack.ID != freshBackendID {
		t.Fatalf("LibraryTrack.ID = %q, want fresh %q from resolver", tr.LibraryTrack.ID, freshBackendID)
	}
	if tr.LibraryTrack.CoverArtID != freshCover {
		t.Fatalf("LibraryTrack.CoverArtID = %q, want %q from resolver", tr.LibraryTrack.CoverArtID, freshCover)
	}
	// Matcher must NOT have been called (resolver path must short-circuit it).
	if matcherCalled {
		t.Fatal("s.match.Match was called — but it should NOT be called when CanonicalID is present and resolver returns a result")
	}
	// Resolver must have been called with the right catalogID.
	if len(resolver.calls) != 1 || resolver.calls[0] != catalogID {
		t.Fatalf("resolver.calls = %v, want [%q]", resolver.calls, catalogID)
	}
}

// trackingMatcherT5 wraps fakeMatcher and records whether it was called.
type trackingMatcherT5 struct {
	inner  fakeMatcher
	called *bool
}

func (m *trackingMatcherT5) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	*m.called = true
	return m.inner.Match(ctx, ext)
}

// TestTask5_SwapSurvival asserts that Detail() always re-resolves at read time
// so a backend swap (resolver returning a NEW backend id) is reflected immediately.
func TestTask5_SwapSurvival(t *testing.T) {
	const catalogID = "trk_swap"
	const preSwapID = "pre-swap-be-id"
	const postSwapID = "post-swap-be-id"

	store := newMemStore()
	seedLibraryTrackWithCanonicalID(store, "pl-swap", catalogID, preSwapID)

	fakeRes := &fakeBindingResolver{
		resolved: map[string]resolver.Addressing{
			catalogID: {BackendID: preSwapID, Found: true},
		},
	}

	svc := newSvcWithMinterAndResolver(
		fakeMatcher{owned: map[string]string{}},
		store,
		&fakeCanonicalMinter{},
		func() BindingResolver { return fakeRes },
	)

	// First Detail call: gets pre-swap backend id.
	det1, err := svc.Detail(context.Background(), "pl-swap")
	if err != nil {
		t.Fatalf("Detail pre-swap: %v", err)
	}
	if det1.Tracks[0].LibraryTrack == nil || det1.Tracks[0].LibraryTrack.ID != preSwapID {
		t.Fatalf("pre-swap: expected LibraryTrack.ID=%q, got %+v", preSwapID, det1.Tracks[0].LibraryTrack)
	}

	// Simulate backend swap: resolver now returns a new backend id.
	fakeRes.resolved[catalogID] = resolver.Addressing{BackendID: postSwapID, Found: true}

	// Second Detail call: must see the new backend id without any re-import.
	det2, err := svc.Detail(context.Background(), "pl-swap")
	if err != nil {
		t.Fatalf("Detail post-swap: %v", err)
	}
	if det2.Tracks[0].LibraryTrack == nil || det2.Tracks[0].LibraryTrack.ID != postSwapID {
		t.Fatalf("post-swap: expected LibraryTrack.ID=%q, got %+v", postSwapID, det2.Tracks[0].LibraryTrack)
	}
}

// TestTask5_MintAtPersist_LibraryTrack asserts that when a library-source track
// is added (AddTrack), the CanonicalMinter is called and the resulting CanonicalID
// is stored on the persisted ExternalResult.
func TestTask5_MintAtPersist_LibraryTrack(t *testing.T) {
	store := newMemStore()
	// Create a managed playlist.
	minter := &fakeCanonicalMinter{nextID: func(_ catalog.Identity) string { return "trk_minted_lib" }}
	svc := newSvcWithMinter(fakeMatcher{owned: map[string]string{}}, store, minter)

	det, err := svc.CreateManaged(context.Background(), "My Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}

	libraryTrack := core.ExternalResult{
		Source:     "library",
		ExternalID: "lib-track-99",
		Title:      "Local Song",
		Artist:     "Local Artist",
		Album:      "Local Album",
		DurationMs: 240000,
		ISRC:       "USABC1234567",
		Type:       core.EntityTrack,
	}
	_, err = svc.AddTrack(context.Background(), det.ID, libraryTrack)
	if err != nil {
		t.Fatalf("AddTrack: %v", err)
	}

	// Minter must have been called exactly once with correct Identity.
	if len(minter.mintCalls) != 1 {
		t.Fatalf("expected 1 mint call, got %d", len(minter.mintCalls))
	}
	id := minter.mintCalls[0]
	if id.Kind != "track" {
		t.Fatalf("mint call Kind = %q, want %q", id.Kind, "track")
	}
	if id.Title != libraryTrack.Title || id.Artist != libraryTrack.Artist || id.Album != libraryTrack.Album {
		t.Fatalf("mint call metadata mismatch: %+v", id)
	}
	if id.DurationMs != libraryTrack.DurationMs {
		t.Fatalf("mint call DurationMs = %d, want %d", id.DurationMs, libraryTrack.DurationMs)
	}
	if id.ISRC != libraryTrack.ISRC {
		t.Fatalf("mint call ISRC = %q, want %q", id.ISRC, libraryTrack.ISRC)
	}
	// Source and ExternalID must be blank (pure library — no external anchor).
	if id.Source != "" || id.ExternalID != "" {
		t.Fatalf("mint call Source=%q ExternalID=%q, want both empty (pure library)", id.Source, id.ExternalID)
	}

	// The stored track must carry the minted CanonicalID.
	row, err := store.Get(context.Background(), det.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	var persisted []core.ExternalResult
	if err := json.Unmarshal([]byte(row.TracksJSON), &persisted); err != nil {
		t.Fatalf("unmarshal TracksJSON: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted track, got %d", len(persisted))
	}
	if persisted[0].CanonicalID != "trk_minted_lib" {
		t.Fatalf("persisted CanonicalID = %q, want %q", persisted[0].CanonicalID, "trk_minted_lib")
	}
}

// TestTask5_MintAtPersist_ExternalTrackNotMinted asserts that a non-library
// (external/Spotify) track does NOT trigger a CanonicalMinter call when added.
func TestTask5_MintAtPersist_ExternalTrackNotMinted(t *testing.T) {
	store := newMemStore()
	minter := &fakeCanonicalMinter{}
	svc := newSvcWithMinter(fakeMatcher{owned: map[string]string{}}, store, minter)

	det, err := svc.CreateManaged(context.Background(), "External Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}

	externalTrack := core.ExternalResult{
		Source:     "spotify",
		ExternalID: "sp-track-123",
		Title:      "Remote Song",
		Artist:     "Remote Artist",
		Album:      "Remote Album",
		DurationMs: 200000,
		Type:       core.EntityTrack,
	}
	_, err = svc.AddTrack(context.Background(), det.ID, externalTrack)
	if err != nil {
		t.Fatalf("AddTrack: %v", err)
	}

	// Minter must NOT have been called for a non-library track.
	if len(minter.mintCalls) != 0 {
		t.Fatalf("expected 0 mint calls for external track, got %d: %+v", len(minter.mintCalls), minter.mintCalls)
	}
}

// TestTask5_NilMinter_NoMintNoPanic asserts that when no CanonicalMinter is
// wired, AddTrack completes without panicking and no CanonicalID is set.
func TestTask5_NilMinter_NoMintNoPanic(t *testing.T) {
	store := newMemStore()
	// No minter — use NewService directly without WithCanonicalMinter.
	svc := NewService(nil, fakeMatcher{owned: map[string]string{}}, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(), nil)

	det, err := svc.CreateManaged(context.Background(), "Nil Minter Playlist")
	if err != nil {
		t.Fatalf("CreateManaged: %v", err)
	}

	libraryTrack := core.ExternalResult{
		Source:     "library",
		ExternalID: "lib-no-minter",
		Title:      "Local Song",
		Artist:     "Artist",
		Album:      "Album",
		DurationMs: 180000,
		Type:       core.EntityTrack,
	}
	// Must not panic.
	_, err = svc.AddTrack(context.Background(), det.ID, libraryTrack)
	if err != nil {
		t.Fatalf("AddTrack with nil minter: %v", err)
	}

	// CanonicalID must be empty (no minter = no mint).
	row, _ := store.Get(context.Background(), det.ID)
	var persisted []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &persisted)
	if len(persisted) > 0 && persisted[0].CanonicalID != "" {
		t.Fatalf("expected empty CanonicalID with nil minter, got %q", persisted[0].CanonicalID)
	}
}

// TestTask5_NoResolver_LegacyTrack_FallsBackToMatcher asserts that when a
// library-source track has NO CanonicalID (legacy — persisted before Task 5),
// Detail() falls back to s.match.Match (existing behavior), no panic.
func TestTask5_NoResolver_LegacyTrack_FallsBackToMatcher(t *testing.T) {
	const oldBackendID = "legacy-be-id"
	const freshBackendID = "matched-fresh-id"

	// Seed a legacy library track WITHOUT a CanonicalID.
	legacyEntry := core.ExternalResult{
		Source:      "library",
		ExternalID:  oldBackendID,
		CanonicalID: "", // legacy — no canonical id
		Title:       "Legacy Track",
		Artist:      "Artist",
		Album:       "Album",
		DurationMs:  180000,
		Type:        core.EntityTrack,
	}
	store := newMemStore()
	tj, _ := json.Marshal([]core.ExternalResult{legacyEntry})
	store.rows["pl-legacy"] = &memRow{SyncedRow{
		ID: "pl-legacy", Source: "local", ExternalID: "pl-legacy",
		Name: "Legacy Playlist", Mode: "once", TracksJSON: string(tj),
	}}
	store.index["local:pl-legacy"] = "pl-legacy"

	// Matcher resolves the track (simulate it being found by fuzzy match).
	m := fakeMatcher{owned: map[string]string{oldBackendID: freshBackendID}}

	// No resolver needed — omit resolverFn so s.resolve is nil.
	svc := NewService(nil, m, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(), nil)

	det, err := svc.Detail(context.Background(), "pl-legacy")
	if err != nil {
		t.Fatalf("Detail legacy track: %v", err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	tr := det.Tracks[0]
	// Must resolve via matcher → CoverageFull with the matched fresh id.
	if tr.State != core.CoverageFull {
		t.Fatalf("State = %v, want CoverageFull (matcher fallback)", tr.State)
	}
	if tr.LibraryTrack == nil || tr.LibraryTrack.ID != freshBackendID {
		t.Fatalf("LibraryTrack.ID = %q (via matcher), want %q", tr.LibraryTrack.ID, freshBackendID)
	}
}

// TestTask5_ResolverNilReturn_FallsBackToMatcher asserts that when the resolver
// provider returns nil (resolver not yet ready), Detail() falls back to
// s.match.Match without panicking.
func TestTask5_ResolverNilReturn_FallsBackToMatcher(t *testing.T) {
	const catalogID = "trk_nilresolver"
	const backendID = "be-id-via-matcher"

	store := newMemStore()
	seedLibraryTrackWithCanonicalID(store, "pl-nilres", catalogID, "old-be")

	// Matcher can resolve by the stored ExternalID "old-be".
	m := fakeMatcher{owned: map[string]string{"old-be": backendID}}

	// Resolver provider returns nil (not ready).
	svc := NewService(nil, m, &fakeDownloader{}, store, nil,
		func() int64 { return 100 }, seqID(),
		func() BindingResolver { return nil }, // nil resolver
	)

	det, err := svc.Detail(context.Background(), "pl-nilres")
	if err != nil {
		t.Fatalf("Detail with nil resolver: %v", err)
	}
	if len(det.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(det.Tracks))
	}
	// Should fall back to matcher.
	tr := det.Tracks[0]
	if tr.State != core.CoverageFull {
		t.Fatalf("State = %v, want CoverageFull (matcher fallback when resolver nil)", tr.State)
	}
	if tr.LibraryTrack == nil || tr.LibraryTrack.ID != backendID {
		t.Fatalf("LibraryTrack.ID = %q, want %q (matcher fallback)", tr.LibraryTrack.ID, backendID)
	}
}
