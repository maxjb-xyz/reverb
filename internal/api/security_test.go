package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postWithOrigin issues a POST carrying an explicit Origin header and Host.
func postWithOrigin(t *testing.T, srv *Server, path, origin, host, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCSRFBlocksCrossOrigin(t *testing.T) {
	srv := newTestServer(t)
	rr := postWithOrigin(t, srv, "/api/v1/auth/login", "http://evil.example", "reverb.local",
		`{"username":"x","password":"whatever1"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST = %d, want 403", rr.Code)
	}
}

func TestCSRFAllowsSameOrigin(t *testing.T) {
	srv := newTestServer(t)
	// Same host in Origin and Host → passes the guard; login then fails on creds (401), not 403.
	rr := postWithOrigin(t, srv, "/api/v1/auth/login", "http://reverb.local", "reverb.local",
		`{"username":"x","password":"whatever1"}`)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("same-origin POST was blocked (%d); should reach the handler", rr.Code)
	}
}

func TestCSRFAllowsMissingOrigin(t *testing.T) {
	srv := newTestServer(t)
	// No Origin/Referer (curl / native client) → not a CSRF vector → allowed through.
	rr := postWithOrigin(t, srv, "/api/v1/auth/login", "", "reverb.local",
		`{"username":"x","password":"whatever1"}`)
	if rr.Code == http.StatusForbidden {
		t.Fatalf("origin-less POST was blocked (%d); should reach the handler", rr.Code)
	}
}

func TestCSRFDoesNotBlockGET(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	req.Header.Set("Origin", "http://evil.example")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-origin GET = %d, want 200 (reads are exempt)", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	srv.Handler().ServeHTTP(rec, req)
	h := rec.Header()
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy header")
	}
}

// TestSetupWeakPasswordRejected asserts the first-run wizard enforces the policy.
func TestSetupWeakPasswordRejected(t *testing.T) {
	srv := newTestServer(t)
	rr := doPOST(t, srv, "/api/v1/setup/admin", "", `{"username":"owner","password":"short12"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("weak setup password = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "at least 8") {
		t.Fatalf("expected a helpful policy message, got: %s", rr.Body.String())
	}
}

// TestRequesterCannotControlQueue asserts a role-requester (no auto_approve) is
// forbidden from the global download-queue controls.
func TestRequesterCannotControlQueue(t *testing.T) {
	srv := newTestServer(t)
	_, rtok := setupRequesterSession(t, srv)
	for _, path := range []string{
		"/api/v1/downloads/pause",
		"/api/v1/downloads/resume",
		"/api/v1/downloads/clear",
		"/api/v1/downloads/j1/cancel",
		"/api/v1/downloads/j1/retry",
	} {
		if rr := doPOST(t, srv, path, rtok, ""); rr.Code != http.StatusForbidden {
			t.Errorf("requester POST %s = %d, want 403", path, rr.Code)
		}
	}
}

// TestPasswordChangeRevokesOtherSessions asserts that changing a password
// invalidates the user's other sessions but preserves the caller's own.
func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	current := mustLogin(t, srv, "owner", "pw123456")
	other := mustLogin(t, srv, "owner", "pw123456")

	if rr := doPOST(t, srv, "/api/v1/account/password", current,
		`{"current":"pw123456","new":"brandnewpw1"}`); rr.Code != http.StatusOK {
		t.Fatalf("change password = %d: %s", rr.Code, rr.Body)
	}
	if rr := doGET(t, srv, "/api/v1/me", other); rr.Code != http.StatusUnauthorized {
		t.Fatalf("other session after password change = %d, want 401 (revoked)", rr.Code)
	}
	if rr := doGET(t, srv, "/api/v1/me", current); rr.Code != http.StatusOK {
		t.Fatalf("current session after password change = %d, want 200 (preserved)", rr.Code)
	}
}
