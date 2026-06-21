package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// fakeManager is an in-memory DownloadManager.
type fakeManager struct {
	jobs     map[string]core.DownloadJob
	lastReq  core.DownloadRequest
	canceled []string
	retried  []string
}

func newFakeManager() *fakeManager { return &fakeManager{jobs: map[string]core.DownloadJob{}} }

func (m *fakeManager) Enqueue(_ context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	m.lastReq = req
	j := core.DownloadJob{ID: "job-" + req.ExternalID, DedupKey: "dk", Status: core.DownloadQueued, Source: req.Source, ExternalID: req.ExternalID, PlayWhenReady: req.PlayWhenReady}
	m.jobs[j.ID] = j
	return j, nil
}
func (m *fakeManager) List(context.Context) ([]core.DownloadJob, error) {
	out := []core.DownloadJob{}
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out, nil
}
func (m *fakeManager) Cancel(_ context.Context, id string) error {
	m.canceled = append(m.canceled, id)
	return nil
}
func (m *fakeManager) Retry(_ context.Context, id string) (core.DownloadJob, error) {
	m.retried = append(m.retried, id)
	return core.DownloadJob{ID: id, Status: core.DownloadQueued, Attempts: 1}, nil
}
func (m *fakeManager) Stop() {}

func downloadTestServer(t *testing.T, mgr DownloadManager) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
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
		Downloads:  mgr,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestCreateDownloadEnqueues(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	body := `{"source":"spotify","externalId":"sp1","artist":"A","title":"T","album":"Al","playWhenReady":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", bytes.NewBufferString(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var job core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ExternalID != "sp1" || job.Status != core.DownloadQueued {
		t.Fatalf("job = %+v", job)
	}
	if !mgr.lastReq.PlayWhenReady {
		t.Fatal("playWhenReady not forwarded to Enqueue")
	}
}

func TestListDownloads(t *testing.T) {
	mgr := newFakeManager()
	mgr.jobs["j1"] = core.DownloadJob{ID: "j1", Status: core.DownloadRunning}
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var jobs []core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "j1" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestCancelDownload(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j9/cancel", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(mgr.canceled) != 1 || mgr.canceled[0] != "j9" {
		t.Fatalf("canceled = %v", mgr.canceled)
	}
}

func TestRetryDownload(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j5/retry", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(mgr.retried) != 1 || mgr.retried[0] != "j5" {
		t.Fatalf("retried = %v", mgr.retried)
	}
}

func TestDownloadsRequireAuth(t *testing.T) {
	srv, _ := downloadTestServer(t, newFakeManager())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateDownloadNilManager503(t *testing.T) {
	srv, cookie := downloadTestServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", bytes.NewBufferString(`{"externalId":"x"}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
