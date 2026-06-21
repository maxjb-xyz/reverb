package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Auth:       auth.NewService(st.Q(), time.Now),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
}

func TestSetupThenProtectedAccess(t *testing.T) {
	srv := testServer(t)
	h := srv.Handler()

	// setup required initially
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if rec.Body.String() == "" || rec.Code != 200 {
		t.Fatalf("setup status failed: %d %s", rec.Code, rec.Body.String())
	}

	// /me is 401 before auth
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/me before auth = %d, want 401", rec.Code)
	}

	// complete setup → expect a session cookie
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"password":"pw"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup admin = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected session cookie")
	}

	// /me with cookie is 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with cookie = %d", rec.Code)
	}

	// the session cookie must be HttpOnly
	if !cookie[0].HttpOnly {
		t.Fatal("session cookie should be HttpOnly")
	}

	// a second setup/admin must be rejected (can't reset password after setup)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"password":"pw2"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup/admin = %d, want 409", rec.Code)
	}
}

func TestAuthDisabledAllowsProtectedRoutes(t *testing.T) {
	srv := testServer(t)
	if err := srv.deps.Auth.SetAuthDisabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with auth disabled = %d, want 200", rec.Code)
	}
}
