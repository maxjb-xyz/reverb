package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

var errActiveClear = errors.New("cannot clear active job")

// fakeManager is an in-memory DownloadManager.
type fakeManager struct {
	jobs          map[string]core.DownloadJob
	lastReq       core.DownloadRequest
	enqueueCalls  int // incremented on every Enqueue call
	canceled      []string
	retried       []string
	lastRetryURL  string // manualURL from the most recent Retry call
	paused        bool
	cleared       []string
	clearedFinish int
}

func newFakeManager() *fakeManager { return &fakeManager{jobs: map[string]core.DownloadJob{}} }

func (m *fakeManager) Enqueue(_ context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	m.enqueueCalls++
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
func (m *fakeManager) Retry(_ context.Context, id string, manualURL string) (core.DownloadJob, error) {
	m.retried = append(m.retried, id)
	m.lastRetryURL = manualURL
	return core.DownloadJob{ID: id, Status: core.DownloadQueued, Attempts: 1}, nil
}
func (m *fakeManager) Stop()          {}
func (m *fakeManager) Pause()         { m.paused = true }
func (m *fakeManager) Resume()        { m.paused = false }
func (m *fakeManager) IsPaused() bool { return m.paused }
func (m *fakeManager) Clear(_ context.Context, id string) error {
	if j, ok := m.jobs[id]; ok && (j.Status == core.DownloadQueued || j.Status == core.DownloadRunning) {
		return errActiveClear
	}
	m.cleared = append(m.cleared, id)
	delete(m.jobs, id)
	return nil
}
func (m *fakeManager) ClearFinished(context.Context) ([]string, error) {
	var ids []string
	for id, j := range m.jobs {
		if j.Status == core.DownloadCompleted || j.Status == core.DownloadFailed || j.Status == core.DownloadCanceled {
			ids = append(ids, id)
			delete(m.jobs, id)
		}
	}
	m.clearedFinish = len(ids)
	return ids, nil
}

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
	authSvc, tok := seededAuthToken(t, st)
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
	// plain retry (no body) → manualURL must be empty
	if mgr.lastRetryURL != "" {
		t.Fatalf("plain retry should pass manualURL=\"\", got %q", mgr.lastRetryURL)
	}
}

func TestRetryDownloadWithManualURL(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	body := `{"manualUrl":"https://www.youtube.com/watch?v=XYZ"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j7/retry", bytes.NewBufferString(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(mgr.retried) != 1 || mgr.retried[0] != "j7" {
		t.Fatalf("retried = %v", mgr.retried)
	}
	if mgr.lastRetryURL != "https://www.youtube.com/watch?v=XYZ" {
		t.Fatalf("manualURL not passed through: got %q", mgr.lastRetryURL)
	}
}

func TestRetryDownloadNoBodyIsPlainRetry(t *testing.T) {
	// Regression: an absent body (nil) must not cause a decode error; manualURL="".
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j8/retry", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if mgr.lastRetryURL != "" {
		t.Fatalf("nil-body retry should pass manualURL=\"\", got %q", mgr.lastRetryURL)
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

func TestPauseResumeQueue(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)

	post := func(path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if post("/api/v1/downloads/pause") != http.StatusOK || !mgr.paused {
		t.Fatal("pause should set paused=true")
	}
	if post("/api/v1/downloads/resume") != http.StatusOK || mgr.paused {
		t.Fatal("resume should set paused=false")
	}

	// GET /downloads/queue reflects state.
	mgr.paused = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/queue", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue status = %d", rec.Code)
	}
	var q struct {
		Paused bool `json:"paused"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if !q.Paused {
		t.Fatal("GET /downloads/queue should report paused=true")
	}
}

func TestClearSingleAndFinished(t *testing.T) {
	mgr := newFakeManager()
	mgr.jobs["done"] = core.DownloadJob{ID: "done", Status: core.DownloadCompleted}
	mgr.jobs["run"] = core.DownloadJob{ID: "run", Status: core.DownloadRunning}
	srv, cookie := downloadTestServer(t, mgr)

	// Clear a terminal job → 200.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/done/clear", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear terminal = %d", rec.Code)
	}

	// Clear an active job → 422.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/downloads/run/clear", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("clear active = %d, want 422", rec.Code)
	}

	// Bulk clear finished (no ids) → removes the remaining terminal jobs.
	mgr.jobs["f"] = core.DownloadJob{ID: "f", Status: core.DownloadFailed}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/downloads/clear", bytes.NewBufferString(`{}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk clear = %d", rec.Code)
	}
	var resp struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Removed < 1 {
		t.Fatalf("bulk clear removed %d, want >=1", resp.Removed)
	}
}
