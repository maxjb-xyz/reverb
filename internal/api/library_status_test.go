package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestLibraryStatus_ReportsSupervisorState(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	// Rebuild with LibraryStatus closure, reusing auth from libTestServer.
	srv2 := NewServer(Deps{
		Auth:          srv.deps.Auth,
		Library:       &fakeLibrary{},
		Search:        srv.deps.Search,
		Downloader:    srv.deps.Downloader,
		LibraryStatus: func() (string, string) { return "built-in", "starting" },
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/status", nil)
	req.AddCookie(cookie)
	srv2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var dto struct{ Mode, State string }
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Mode != "built-in" || dto.State != "starting" {
		t.Fatalf("got %+v, want built-in/starting", dto)
	}
}

func TestLibraryStatus_FallbackLibraryPresent(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/status", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var dto struct{ Mode, State string }
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Mode != "external" || dto.State != "ready" {
		t.Fatalf("got %+v, want external/ready", dto)
	}
}

func TestLibraryStatus_FallbackNoLibrary(t *testing.T) {
	// Build a server with no library configured (Library: nil).
	st, err := store.Open(t.TempDir() + "/ls.db")
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
	tok, err := authSvc.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{
		Auth:       authSvc,
		Library:    nil,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/status", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var dto struct{ Mode, State string }
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Mode != "external" || dto.State != "unconfigured" {
		t.Fatalf("got %+v, want external/unconfigured", dto)
	}
}

func TestLibraryStatus_RequiresAuth(t *testing.T) {
	srv, _ := libTestServer(t, &fakeLibrary{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/status", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
