package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// fakeSync is a controllable SyncService for handler tests.
type fakeSync struct {
	detail        core.SyncedPlaylistDetail
	list          []core.SyncedPlaylist
	jobs          []core.DownloadJob
	importErr     error
	importOnceErr error
	importOnceDet core.SyncedPlaylistDetail
	createDet     core.SyncedPlaylistDetail
	createErr     error
	listErr       error
	detailErr     error
	syncErr       error
	dlErr         error

	lastURL        string
	lastDL         bool
	lastID         string
	lastImportOnce string
	lastCreateName string
	settings       syncedSettingsBody
	settingsID     string
	deletedID      string
	settingsErr    error
	deleteErr      error

	addTrackEntry core.ExternalResult
	addTrackID    string
	addTrackErr   error
	removeTrackID string
	removeSource  string
	removeExtID   string
	removeErr     error

	setCoverID  string
	setCoverURL string
	setCoverDet core.SyncedPlaylistDetail
	setCoverErr error

	reorderID    string
	reorderOrder []core.TrackKey
	reorderDet   core.SyncedPlaylistDetail
	reorderErr   error

	renameID   string
	renameName string
	renameDet  core.SyncedPlaylistDetail
	renameErr  error
}

func (f *fakeSync) Import(_ context.Context, url string, downloadMissing bool) (core.SyncedPlaylistDetail, error) {
	f.lastURL, f.lastDL = url, downloadMissing
	return f.detail, f.importErr
}
func (f *fakeSync) ImportOnce(_ context.Context, url string) (core.SyncedPlaylistDetail, error) {
	f.lastImportOnce = url
	return f.importOnceDet, f.importOnceErr
}
func (f *fakeSync) CreateManaged(_ context.Context, name string) (core.SyncedPlaylistDetail, error) {
	f.lastCreateName = name
	return f.createDet, f.createErr
}
func (f *fakeSync) AddTrack(_ context.Context, id string, entry core.ExternalResult) (core.SyncedPlaylistDetail, error) {
	f.addTrackID, f.addTrackEntry = id, entry
	return f.detail, f.addTrackErr
}
func (f *fakeSync) RemoveTrack(_ context.Context, id, source, externalID string) (core.SyncedPlaylistDetail, error) {
	f.removeTrackID, f.removeSource, f.removeExtID = id, source, externalID
	return f.detail, f.removeErr
}
func (f *fakeSync) List(_ context.Context) ([]core.SyncedPlaylist, error) {
	return f.list, f.listErr
}
func (f *fakeSync) Detail(_ context.Context, id string) (core.SyncedPlaylistDetail, error) {
	f.lastID = id
	return f.detail, f.detailErr
}
func (f *fakeSync) Sync(_ context.Context, id string) (core.SyncedPlaylistDetail, error) {
	f.lastID = id
	return f.detail, f.syncErr
}
func (f *fakeSync) DownloadMissing(_ context.Context, id string) ([]core.DownloadJob, error) {
	f.lastID = id
	return f.jobs, f.dlErr
}
func (f *fakeSync) UpdateSettings(_ context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error {
	f.settingsID = id
	f.settings = syncedSettingsBody{SyncEnabled: enabled, IntervalSec: intervalSec, AutoDownload: autoDownload}
	return f.settingsErr
}
func (f *fakeSync) Delete(_ context.Context, id string) error {
	f.deletedID = id
	return f.deleteErr
}
func (f *fakeSync) SetCover(_ context.Context, id, coverURL string) (core.SyncedPlaylistDetail, error) {
	f.setCoverID, f.setCoverURL = id, coverURL
	return f.setCoverDet, f.setCoverErr
}
func (f *fakeSync) ReorderTracks(_ context.Context, id string, order []core.TrackKey) (core.SyncedPlaylistDetail, error) {
	f.reorderID, f.reorderOrder = id, order
	return f.reorderDet, f.reorderErr
}
func (f *fakeSync) Rename(_ context.Context, id, name string) (core.SyncedPlaylistDetail, error) {
	f.renameID, f.renameName = id, name
	return f.renameDet, f.renameErr
}

// syncTestServer builds a Server with a fake sync service.
func syncTestServer(t *testing.T, svc SyncService) (*Server, *http.Cookie) {
	t.Helper()
	return syncTestServerWithDataDir(t, svc, "")
}

// syncTestServerWithDataDir builds a Server with a fake sync service and a data dir.
func syncTestServerWithDataDir(t *testing.T, svc SyncService, dataDir string) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())
	srv := NewServer(Deps{
		Auth:       authSvc,
		Sync:       svc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		DataDir:    dataDir,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestSyncedImportReturnsDetail(t *testing.T) {
	svc := &fakeSync{detail: core.SyncedPlaylistDetail{
		SyncedPlaylist: core.SyncedPlaylist{ID: "p1", Source: "spotify", ExternalID: "ext1", Name: "Mix", TrackCount: 2},
		TotalCount:     2, OwnedCount: 1,
		Tracks: []core.AlbumDetailTrack{{Title: "One"}, {Title: "Two"}},
	}}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ext1","downloadMissing":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var det core.SyncedPlaylistDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil {
		t.Fatal(err)
	}
	if det.ID != "p1" || det.TotalCount != 2 || len(det.Tracks) != 2 {
		t.Fatalf("detail = %+v", det)
	}
	if svc.lastURL != "https://open.spotify.com/playlist/ext1" || !svc.lastDL {
		t.Fatalf("import args = %q / %v", svc.lastURL, svc.lastDL)
	}
}

func TestSyncedImportMissingURLReturns400(t *testing.T) {
	srv, cookie := syncTestServer(t, &fakeSync{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSyncedImportBadURLReturns400(t *testing.T) {
	svc := &fakeSync{importErr: playlistsync.ErrNotPlaylistURL}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	body := `{"url":"https://example.com/nope"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not a spotify playlist url") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSyncedImportFetchErrorReturns422(t *testing.T) {
	svc := &fakeSync{importErr: errors.New("spotify: 502 bad gateway")}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ext1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "502 bad gateway") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSyncedListReturns(t *testing.T) {
	svc := &fakeSync{list: []core.SyncedPlaylist{
		{ID: "p1", Name: "A"}, {ID: "p2", Name: "B"},
	}}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/synced-playlists", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var list []core.SyncedPlaylist
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[1].ID != "p2" {
		t.Fatalf("list = %+v", list)
	}
}

func TestSyncedDownloadMissingReturnsJobs(t *testing.T) {
	svc := &fakeSync{jobs: []core.DownloadJob{{ID: "j1"}, {ID: "j2"}}}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/p1/download-missing", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var jobs []core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || svc.lastID != "p1" {
		t.Fatalf("jobs = %+v, lastID = %q", jobs, svc.lastID)
	}
}

func TestSyncedSettingsUpdates(t *testing.T) {
	svc := &fakeSync{}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	body := `{"syncEnabled":true,"intervalSec":3600,"autoDownload":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/p9/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.settingsID != "p9" || !svc.settings.SyncEnabled || svc.settings.IntervalSec != 3600 || !svc.settings.AutoDownload {
		t.Fatalf("settings id=%q %+v", svc.settingsID, svc.settings)
	}
}

func TestSyncedDelete(t *testing.T) {
	svc := &fakeSync{}
	srv, cookie := syncTestServer(t, svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/synced-playlists/p7", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if svc.deletedID != "p7" {
		t.Fatalf("deletedID = %q", svc.deletedID)
	}
}

func TestSyncedNilServiceReturns503(t *testing.T) {
	srv, cookie := syncTestServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/synced-playlists", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /playlists/import (one-time import) tests
// ---------------------------------------------------------------------------

func TestImportPlaylistOnceHappyPath(t *testing.T) {
	svc := &fakeSync{importOnceDet: core.SyncedPlaylistDetail{
		SyncedPlaylist: core.SyncedPlaylist{ID: "new-pl-1", Name: "Imported Mix", Mode: "once", CoverURL: "https://cover.example.com/img.jpg"},
		TotalCount:     2, OwnedCount: 0,
		Tracks: []core.AlbumDetailTrack{{Title: "Track 1"}, {Title: "Track 2"}},
	}}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ABCDEF"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playlists/import", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var det core.SyncedPlaylistDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil {
		t.Fatal(err)
	}
	if det.ID != "new-pl-1" || det.Name != "Imported Mix" || det.Mode != "once" {
		t.Fatalf("detail = %+v", det)
	}
	if svc.lastImportOnce != "https://open.spotify.com/playlist/ABCDEF" {
		t.Fatalf("ImportOnce url = %q", svc.lastImportOnce)
	}
}

func TestImportPlaylistOnceBadURLReturns400(t *testing.T) {
	svc := &fakeSync{importOnceErr: playlistsync.ErrNotPlaylistURL}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"url":"https://example.com/not-a-playlist"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playlists/import", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestImportPlaylistOnceMissingURLReturns400(t *testing.T) {
	srv, cookie := syncTestServer(t, &fakeSync{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playlists/import", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestImportPlaylistOnceNilServiceReturns503(t *testing.T) {
	srv, cookie := syncTestServer(t, nil)
	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playlists/import", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAddSyncedTrackHappyPath(t *testing.T) {
	svc := &fakeSync{detail: core.SyncedPlaylistDetail{
		SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
		TotalCount:     1,
	}}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"source":"spotify","externalId":"t-new","title":"New Track","artist":"Artist","album":"Album","durationMs":210000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-1/tracks", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.addTrackID != "pl-1" || svc.addTrackEntry.ExternalID != "t-new" {
		t.Fatalf("AddTrack args: id=%q entry=%+v", svc.addTrackID, svc.addTrackEntry)
	}
}

func TestAddSyncedTrackNotEditableReturns409(t *testing.T) {
	svc := &fakeSync{addTrackErr: playlistsync.ErrNotEditable}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"source":"spotify","externalId":"t-new","title":"T","artist":"A","album":"B","durationMs":200000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-synced/tracks", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveSyncedTrackHappyPath(t *testing.T) {
	svc := &fakeSync{detail: core.SyncedPlaylistDetail{
		SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
	}}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/synced-playlists/SP1/tracks?source=spotify&externalId=t-old", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.removeTrackID != "SP1" || svc.removeSource != "spotify" || svc.removeExtID != "t-old" {
		t.Fatalf("RemoveTrack args: id=%q src=%q extID=%q", svc.removeTrackID, svc.removeSource, svc.removeExtID)
	}
}

// TestImportSpotifyNotConfiguredReturns503 asserts that Import returns 503 when
// the service returns ErrSpotifyNotConfigured (library present, Spotify absent).
func TestImportSpotifyNotConfiguredReturns503(t *testing.T) {
	svc := &fakeSync{importErr: playlistsync.ErrSpotifyNotConfigured}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

// TestImportOnceSpotifyNotConfiguredReturns503 asserts that ImportOnce returns
// 503 when the service returns ErrSpotifyNotConfigured.
func TestImportOnceSpotifyNotConfiguredReturns503(t *testing.T) {
	svc := &fakeSync{importOnceErr: playlistsync.ErrSpotifyNotConfigured}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	body := `{"url":"https://open.spotify.com/playlist/ABC"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/playlists/import", strings.NewReader(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

// TestSyncNowSpotifyNotConfiguredReturns503 asserts that Sync returns 503 when
// the service returns ErrSpotifyNotConfigured.
func TestSyncNowSpotifyNotConfiguredReturns503(t *testing.T) {
	svc := &fakeSync{syncErr: playlistsync.ErrSpotifyNotConfigured}
	srv, cookie := syncTestServer(t, svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/p1/sync", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveSyncedTrackMissingParamsReturns400(t *testing.T) {
	srv, cookie := syncTestServer(t, &fakeSync{})

	tests := []struct {
		name string
		url  string
	}{
		{"missing both", "/api/v1/synced-playlists/SP1/tracks"},
		{"missing externalId", "/api/v1/synced-playlists/SP1/tracks?source=spotify"},
		{"missing source", "/api/v1/synced-playlists/SP1/tracks?externalId=t-old"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodDelete, tc.url, nil)
			req.AddCookie(cookie)
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: build a multipart form with an "image" field
// ---------------------------------------------------------------------------

func buildCoverMultipart(t *testing.T, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="image"; filename="cover.bin"`)
	h.Set("Content-Type", contentType)
	pw, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

// ---------------------------------------------------------------------------
// Cover upload tests
// ---------------------------------------------------------------------------

func TestUploadPlaylistCoverHappyPath(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{
		// detail is returned by Detail() — pre-check needs Mode="once".
		detail: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
		},
		setCoverDet: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
		},
	}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	// Minimal 1×1 PNG bytes.
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}

	body, ct := buildCoverMultipart(t, "image/png", pngData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-1/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// File should exist on disk.
	savedPath := filepath.Join(dataDir, "playlist-covers", "pl-1.png")
	if _, err := os.Stat(savedPath); err != nil {
		t.Fatalf("cover file not saved: %v", err)
	}

	// Service should have been called with correct id and a URL pointing to the cover endpoint.
	if svc.setCoverID != "pl-1" {
		t.Fatalf("SetCover id = %q, want pl-1", svc.setCoverID)
	}
	if !strings.Contains(svc.setCoverURL, "/api/v1/synced-playlists/pl-1/cover") {
		t.Fatalf("SetCover url = %q, unexpected", svc.setCoverURL)
	}

	// Response should be the detail.
	var det core.SyncedPlaylistDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil {
		t.Fatal(err)
	}
	if det.ID != "pl-1" {
		t.Fatalf("detail.ID = %q, want pl-1", det.ID)
	}
}

func TestUploadPlaylistCoverOversizedReturns413(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	// 5 MB + 1 byte.
	bigData := make([]byte, 5*1024*1024+1)
	body, ct := buildCoverMultipart(t, "image/jpeg", bigData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-1/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadPlaylistCoverWrongTypeReturns400(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	body, ct := buildCoverMultipart(t, "image/gif", []byte("GIF89a"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-1/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadPlaylistCoverSyncedPlaylistReturns409(t *testing.T) {
	dataDir := t.TempDir()
	// detail.Mode = "synced" → pre-check fires before file write and returns 409.
	svc := &fakeSync{
		detail: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-synced", Mode: "synced"},
		},
	}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	// Full PNG signature so content sniff passes; pre-check should fire before write.
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	}
	body, ct := buildCoverMultipart(t, "image/png", pngData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-synced/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestServePlaylistCoverHappyPath(t *testing.T) {
	dataDir := t.TempDir()
	// Pre-create the cover file.
	coversDir := filepath.Join(dataDir, "playlist-covers")
	if err := os.MkdirAll(coversDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pngData := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(coversDir, "pl-1.png"), pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &fakeSync{}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/synced-playlists/pl-1/cover", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000" {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestServePlaylistCoverNotFoundReturns404(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/synced-playlists/no-such-pl/cover", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Reorder tracks tests
// ---------------------------------------------------------------------------

func TestReorderSyncedTracksHappyPath(t *testing.T) {
	svc := &fakeSync{
		reorderDet: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
		},
	}
	srv, cookie := syncTestServer(t, svc)

	order := []core.TrackKey{
		{Source: "spotify", ExternalID: "t2"},
		{Source: "spotify", ExternalID: "t1"},
	}
	bodyBytes, _ := json.Marshal(map[string]any{"order": order})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/pl-1/tracks/order", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.reorderID != "pl-1" {
		t.Fatalf("ReorderTracks id = %q, want pl-1", svc.reorderID)
	}
	if len(svc.reorderOrder) != 2 || svc.reorderOrder[0].ExternalID != "t2" {
		t.Fatalf("ReorderTracks order = %+v", svc.reorderOrder)
	}
}

func TestReorderSyncedTracksNotEditableReturns409(t *testing.T) {
	svc := &fakeSync{reorderErr: playlistsync.ErrNotEditable}
	srv, cookie := syncTestServer(t, svc)

	bodyBytes, _ := json.Marshal(map[string]any{"order": []core.TrackKey{}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/pl-synced/tracks/order", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// validPlaylistID tests
// ---------------------------------------------------------------------------

func TestValidPlaylistID(t *testing.T) {
	valid := []string{
		"pl-1",
		"abc",
		"ABC123",
		"my_playlist",
		"a1b2-c3d4",
		"ABCDEF-1234_xyz",
	}
	for _, id := range valid {
		if !validPlaylistID(id) {
			t.Errorf("validPlaylistID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"",
		"../etc/passwd",
		"pl/1",
		"pl 1",
		"pl%20one",
		"pl\x00null",
		"pl;drop",
		"pl.dot",
	}
	for _, id := range invalid {
		if validPlaylistID(id) {
			t.Errorf("validPlaylistID(%q) = true, want false", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Cover upload hardening tests (FIX 2 + FIX 3)
// ---------------------------------------------------------------------------

// TestUploadPlaylistCoverNonImageLabeledAsPng asserts that bytes that are not
// a PNG but are declared as image/png are rejected (content sniff wins).
func TestUploadPlaylistCoverNonImageLabeledAsPng(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{
		detail: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-1", Mode: "once"},
		},
	}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	// Bytes that are clearly not an image.
	notAnImage := []byte("not an image at all")
	body, ct := buildCoverMultipart(t, "image/png", notAnImage)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-1/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	// Verify no file was written.
	coversDir := filepath.Join(dataDir, "playlist-covers")
	entries, _ := os.ReadDir(coversDir)
	if len(entries) != 0 {
		t.Fatalf("expected no files written, found %d", len(entries))
	}
}

// TestUploadPlaylistCoverSniffedExtStored asserts that when bytes are a real PNG
// but Content-Type says image/jpeg, the stored file has .png extension (sniffed wins).
func TestUploadPlaylistCoverSniffedExtStored(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{
		detail: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-sniff", Mode: "once"},
		},
		setCoverDet: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-sniff", Mode: "once"},
		},
	}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	// Real minimal PNG bytes.
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	// Lie: declare as image/jpeg but send PNG bytes.
	body, ct := buildCoverMultipart(t, "image/jpeg", pngData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-sniff/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Sniffed type is PNG → file must have .png extension, NOT .jpg.
	pngPath := filepath.Join(dataDir, "playlist-covers", "pl-sniff.png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Fatalf("expected pl-sniff.png to exist, got: %v", err)
	}
	jpgPath := filepath.Join(dataDir, "playlist-covers", "pl-sniff.jpg")
	if _, err := os.Stat(jpgPath); err == nil {
		t.Fatal("pl-sniff.jpg should NOT exist (sniffed ext wins)")
	}
}

// TestUploadPlaylistCoverNotEditableNoFileWritten asserts that when the playlist
// is mode='synced' (not editable), the handler returns 409 and does NOT write any
// file to disk. This verifies the pre-check happens before the write.
func TestUploadPlaylistCoverNotEditableNoFileWritten(t *testing.T) {
	dataDir := t.TempDir()
	svc := &fakeSync{
		// Detail returns mode='synced' → not editable.
		detail: core.SyncedPlaylistDetail{
			SyncedPlaylist: core.SyncedPlaylist{ID: "pl-synced", Mode: "synced"},
		},
	}
	srv, cookie := syncTestServerWithDataDir(t, svc, dataDir)

	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde,
	}
	body, ct := buildCoverMultipart(t, "image/png", pngData)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/synced-playlists/pl-synced/cover", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// No file should have been written.
	coversDir := filepath.Join(dataDir, "playlist-covers")
	entries, _ := os.ReadDir(coversDir)
	if len(entries) != 0 {
		t.Fatalf("expected no files written to disk, found %d", len(entries))
	}
}

// TestCoverExtFromContentType tests the helper directly.
func TestCoverExtFromContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want string
		ok   bool
	}{
		{"image/jpeg", "jpg", true},
		{"image/png", "png", true},
		{"image/webp", "webp", true},
		{"image/jpeg; charset=utf-8", "jpg", true},
		{"image/gif", "", false},
		{"text/plain", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := coverExtFromContentType(tc.ct)
		if ok != tc.ok || got != tc.want {
			t.Errorf("coverExtFromContentType(%q) = %q, %v; want %q, %v", tc.ct, got, ok, tc.want, tc.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// Rename synced playlist tests
// ---------------------------------------------------------------------------

func TestRenameSyncedPlaylist(t *testing.T) {
	svc := &fakeSync{renameDet: core.SyncedPlaylistDetail{SyncedPlaylist: core.SyncedPlaylist{ID: "p1", Name: "New Name"}}}
	srv, cookie := syncTestServer(t, svc)
	body, _ := json.Marshal(map[string]string{"name": "New Name"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/p1", bytes.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if svc.renameID != "p1" {
		t.Errorf("renameID = %q", svc.renameID)
	}
	if svc.renameName != "New Name" {
		t.Errorf("renameName = %q", svc.renameName)
	}
}

func TestRenameSyncedPlaylistEmptyName(t *testing.T) {
	svc := &fakeSync{}
	srv, cookie := syncTestServer(t, svc)
	body, _ := json.Marshal(map[string]string{"name": "   "})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/p1", bytes.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRenameSyncedPlaylistNotFound(t *testing.T) {
	svc := &fakeSync{renameErr: errors.New("not found")}
	srv, cookie := syncTestServer(t, svc)
	body, _ := json.Marshal(map[string]string{"name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/synced-playlists/missing", bytes.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
