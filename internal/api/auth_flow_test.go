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
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Auth:       authSvc,
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"username":"owner","password":"pw123456"}`))
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

	// a second setup/admin must be rejected (can't reset after setup)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"username":"owner2","password":"pw2"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup/admin = %d, want 409", rec.Code)
	}
}

func TestLoginWithUsernameAndMe(t *testing.T) {
	srv := newTestServer(t) // existing helper; ensure its store is seeded (EnsureSeed)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	rr := doGET(t, srv, "/api/v1/me", tok)
	if rr.Code != 200 {
		t.Fatalf("me = %d", rr.Code)
	}
	var me struct {
		Username     string   `json:"username"`
		IsOwner      bool     `json:"isOwner"`
		Capabilities []string `json:"capabilities"`
	}
	json.Unmarshal(rr.Body.Bytes(), &me)
	if me.Username != "owner" || !me.IsOwner || !contains(me.Capabilities, "is_admin") {
		t.Fatalf("me payload wrong: %+v", me)
	}
}

func TestProtectedRouteRejectsNoSession(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	rr := doGET(t, srv, "/api/v1/me", "")
	if rr.Code != 401 {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}
