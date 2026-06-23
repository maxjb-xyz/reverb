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
	ms.rows[id] = &memRow{SyncedRow{
		ID:         id,
		Source:     p.Source,
		ExternalID: p.ExternalID,
		Name:       p.Name,
		CoverURL:   p.CoverURL,
		TracksJSON: tracksJSON,
		CreatedAt:  createdAt,
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

// TestImportOnce_OwnedAndMissing asserts that ImportOnce creates a new library
// playlist, adds owned tracks immediately, and enqueues missing tracks with
// AddToPlaylistID set to the new playlist's ID.
func TestImportOnce_OwnedAndMissing(t *testing.T) {
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
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "My Import", Tracks: []core.ExternalResult{ownedTrack, missingTrack}},
	}}
	matcher := fakeMatcher{owned: map[string]string{"t-owned": "lib-owned-1"}}
	dl := &fakeDownloader{}
	lib := &fakeLibraryWriter{}
	svc := NewService(src, matcher, dl, newMemStore(), lib, func() int64 { return 100 }, seqID())

	pl, err := svc.ImportOnce(context.Background(), "spotify:playlist:PL")
	if err != nil {
		t.Fatalf("ImportOnce: %v", err)
	}
	if pl.ID == "" {
		t.Fatal("ImportOnce: returned playlist has no ID")
	}
	if pl.Name != "My Import" {
		t.Fatalf("playlist name: got %q, want %q", pl.Name, "My Import")
	}

	// CreatePlaylist must have been called once.
	if len(lib.playlists) != 1 {
		t.Fatalf("expected 1 created playlist, got %d", len(lib.playlists))
	}

	// AddTracksToPlaylist must have been called with the owned track.
	if len(lib.addCalls) != 1 {
		t.Fatalf("expected 1 AddTracksToPlaylist call (owned tracks), got %d", len(lib.addCalls))
	}
	if lib.addCalls[0].playlistID != pl.ID {
		t.Fatalf("AddTracksToPlaylist playlist ID: got %q, want %q", lib.addCalls[0].playlistID, pl.ID)
	}
	if len(lib.addCalls[0].trackIDs) != 1 || lib.addCalls[0].trackIDs[0] != "lib-owned-1" {
		t.Fatalf("AddTracksToPlaylist track IDs: got %v, want [lib-owned-1]", lib.addCalls[0].trackIDs)
	}

	// Missing track must have been enqueued with AddToPlaylistID set.
	if len(dl.calls) != 1 {
		t.Fatalf("expected 1 Enqueue call (missing track), got %d", len(dl.calls))
	}
	enq := dl.calls[0]
	if enq.ExternalID != "t-missing" {
		t.Fatalf("enqueued track ExternalID: got %q, want %q", enq.ExternalID, "t-missing")
	}
	if enq.AddToPlaylistID != pl.ID {
		t.Fatalf("enqueued track AddToPlaylistID: got %q, want %q", enq.AddToPlaylistID, pl.ID)
	}
}

// TestImportOnce_BadURL asserts that ImportOnce returns ErrNotPlaylistURL for
// a non-playlist URL.
func TestImportOnce_BadURL(t *testing.T) {
	src := &fakeSource{}
	lib := &fakeLibraryWriter{}
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), lib, func() int64 { return 100 }, seqID())

	_, err := svc.ImportOnce(context.Background(), "https://example.com/not-a-playlist")
	if err == nil {
		t.Fatal("expected ErrNotPlaylistURL, got nil")
	}
	if !errors.Is(err, ErrNotPlaylistURL) {
		t.Fatalf("expected ErrNotPlaylistURL, got %v", err)
	}
}
