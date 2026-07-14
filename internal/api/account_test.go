package api

import (
	"bytes"
	"context"
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
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	rr := doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"pw123456","new":"newpw678"}`)
	if rr.Code != 200 {
		t.Fatalf("change pw = %d", rr.Code)
	}
	// old password no longer works; new one does
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"pw123456"}`).Code != 401 {
		t.Fatal("old password should be rejected")
	}
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"newpw678"}`).Code != 200 {
		t.Fatal("new password should work")
	}
}

func TestChangeOwnPasswordWrongCurrent(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	if doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"WRONG","new":"newpw678"}`).Code != 400 {
		t.Fatal("wrong current should 400")
	}
}

func TestChangeOwnPasswordEmptyCurrentReturns400(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	// empty current with non-empty new must be rejected with 400, not delegated to bcrypt
	if rr := doPOST(t, srv, "/api/v1/account/password", tok, `{"current":"","new":"newpw678"}`); rr.Code != 400 {
		t.Fatalf("empty current password = %d, want 400 (body: %s)", rr.Code, rr.Body)
	}
}

func TestChangeOwnUsername(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")

	if rr := doPATCH(t, srv, "/api/v1/account/profile", tok, `{"username":"renamed"}`); rr.Code != 200 {
		t.Fatalf("rename = %d: %s", rr.Code, rr.Body)
	}
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"owner","password":"pw123456"}`).Code != 401 {
		t.Fatal("old username should no longer log in")
	}
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"renamed","password":"pw123456"}`).Code != 200 {
		t.Fatal("new username should log in")
	}
}

func TestChangeOwnUsernameRejectsBlankAndDuplicate(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw123456")
	tok := mustLogin(t, srv, "owner", "pw123456")
	if _, err := srv.deps.Auth.CreateUser(context.Background(), "other", "otherpw1", "role-user"); err != nil {
		t.Fatal(err)
	}

	if rr := doPATCH(t, srv, "/api/v1/account/profile", tok, `{"username":"  "}`); rr.Code != 400 {
		t.Fatalf("blank username = %d, want 400", rr.Code)
	}
	if rr := doPATCH(t, srv, "/api/v1/account/profile", tok, `{"username":"OTHER"}`); rr.Code != 409 {
		t.Fatalf("duplicate username = %d, want 409", rr.Code)
	}
}
