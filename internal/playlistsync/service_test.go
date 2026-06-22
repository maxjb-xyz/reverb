package playlistsync

import (
	"context"
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
}

func (m fakeMatcher) Match(_ context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	if libID, ok := m.owned[ext.ExternalID]; ok {
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: libID, Method: core.MatchISRC, Confidence: 1.0}, nil
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
		out = append(out, r.SyncedRow)
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
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // t2 missing
	svc := NewService(src, m, &fakeDownloader{}, newMemStore(), func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return importTime }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, dl, newMemStore(), func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, newMemStore(), func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return 100 }, seqID())
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
	svc := NewService(src, fakeMatcher{}, &fakeDownloader{}, store, func() int64 { return 100 }, seqID())
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
