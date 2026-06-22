package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	detail    core.SyncedPlaylistDetail
	list      []core.SyncedPlaylist
	jobs      []core.DownloadJob
	importErr error
	listErr   error
	detailErr error
	syncErr   error
	dlErr     error

	lastURL     string
	lastDL      bool
	lastID      string
	settings    syncedSettingsBody
	settingsID  string
	deletedID   string
	settingsErr error
	deleteErr   error
}

func (f *fakeSync) Import(_ context.Context, url string, downloadMissing bool) (core.SyncedPlaylistDetail, error) {
	f.lastURL, f.lastDL = url, downloadMissing
	return f.detail, f.importErr
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

// syncTestServer builds a Server with a fake sync service.
func syncTestServer(t *testing.T, svc SyncService) (*Server, *http.Cookie) {
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
