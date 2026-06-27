package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doPOST issues a POST with an optional session token string (empty = no cookie)
// and a JSON body. It mirrors doGET's signature for consistency.
func doPOST(t *testing.T, srv *Server, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestChangeOwnPassword(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"pw12345","new":"newpw678"}`)
	if rr.Code != 200 {
		t.Fatalf("change pw = %d", rr.Code)
	}
	// old password no longer works; new one does
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"pw12345"}`).Code != 401 {
		t.Fatal("old password should be rejected")
	}
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"newpw678"}`).Code != 200 {
		t.Fatal("new password should work")
	}
}

func TestChangeOwnPasswordWrongCurrent(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	if doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"WRONG","new":"newpw678"}`).Code != 400 {
		t.Fatal("wrong current should 400")
	}
}

func TestChangeOwnPasswordEmptyCurrentReturns400(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	// empty current with non-empty new must be rejected with 400, not delegated to bcrypt
	if rr := doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"","new":"newpw678"}`); rr.Code != 400 {
		t.Fatalf("empty current password = %d, want 400 (body: %s)", rr.Code, rr.Body)
	}
}
