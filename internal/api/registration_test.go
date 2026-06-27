package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// bytesContain reports whether b contains the string sub.
func bytesContain(b []byte, sub string) bool {
	return bytes.Contains(b, []byte(sub))
}

func TestSignupGatedByPolicy(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	// signup disabled by default
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"carol","password":"carolpw1"}`).Code != 403 {
		t.Fatal("signup should be disabled by default")
	}
	// admin enables open signup
	tok := mustLogin(t, srv, "owner", "pw12345")
	if doPATCH(t, srv, "/api/v1/settings/registration", tok, `{"signupEnabled":true,"invitesEnabled":false,"defaultRoleId":"role-user"}`).Code != 200 {
		t.Fatal("policy update failed")
	}
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"carol","password":"carolpw1"}`).Code != 200 {
		t.Fatal("signup should now succeed")
	}
	// carol got the default role
	ctok := mustLogin(t, srv, "carol", "carolpw1")
	rr := doGET(t, srv, "/api/v1/me", ctok)
	if !bytesContain(rr.Body.Bytes(), `"roleId":"role-user"`) {
		t.Fatalf("carol role wrong: %s", rr.Body)
	}
}

func TestInviteRedemptionAssignsRole(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	doPATCH(t, srv, "/api/v1/settings/registration", tok, `{"signupEnabled":false,"invitesEnabled":true,"defaultRoleId":"role-user"}`)
	rr := doPOST(t, srv, "/api/v1/invites", tok, `{"roleId":"role-requester"}`)
	var inv struct{ Code string }
	json.Unmarshal(rr.Body.Bytes(), &inv)
	// signup with invite works and assigns the invite's role
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"dave","password":"davepw12","invite":"`+inv.Code+`"}`).Code != 200 {
		t.Fatal("invite signup should succeed")
	}
	// invite cannot be reused
	if doPOST(t, srv, "/api/v1/auth/signup", "", `{"username":"erin","password":"erinpw12","invite":"`+inv.Code+`"}`).Code != 403 {
		t.Fatal("used invite must be rejected")
	}
}

func TestListInvites(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/invites", tok, `{}`)
	rr := doGET(t, srv, "/api/v1/invites", tok)
	if rr.Code != 200 {
		t.Fatalf("list invites = %d %s", rr.Code, rr.Body)
	}
	var items []map[string]any
	json.Unmarshal(rr.Body.Bytes(), &items)
	if len(items) == 0 {
		t.Fatal("expected at least one invite")
	}
}

func TestDeleteInvite(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doPOST(t, srv, "/api/v1/invites", tok, `{}`)
	var inv struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	json.Unmarshal(rr.Body.Bytes(), &inv)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/invites/"+inv.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("delete invite = %d %s", rec.Code, rec.Body)
	}
}

// TestCreateInviteReturnsIDAndCode verifies that POST /invites returns a body
// with non-empty id and code fields (no ListInvites scan).
func TestCreateInviteReturnsIDAndCode(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doPOST(t, srv, "/api/v1/invites", tok, `{}`)
	if rr.Code != 201 {
		t.Fatalf("create invite = %d (%s)", rr.Code, rr.Body)
	}
	var body struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode invite body: %v / %s", err, rr.Body)
	}
	if body.ID == "" {
		t.Fatalf("invite response missing non-empty id: %s", rr.Body)
	}
	if body.Code == "" {
		t.Fatalf("invite response missing non-empty code: %s", rr.Body)
	}
}

// TestRegistrationStatus verifies that GET /auth/registration-status is
// reachable WITHOUT a session and returns the expected JSON shape.
func TestRegistrationStatus(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")

	// Must be accessible without any auth cookie.
	rr := doGET(t, srv, "/api/v1/auth/registration-status", "")
	if rr.Code != 200 {
		t.Fatalf("registration-status without auth = %d %s", rr.Code, rr.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %v / %s", err, rr.Body)
	}
	// Both fields must be present in the response (default: false).
	if _, ok := got["signupEnabled"]; !ok {
		t.Fatal("signupEnabled missing from registration-status response")
	}
	if _, ok := got["invitesEnabled"]; !ok {
		t.Fatal("invitesEnabled missing from registration-status response")
	}
	// Defaults must be false.
	if got["signupEnabled"] != false {
		t.Fatalf("expected signupEnabled=false, got %v", got["signupEnabled"])
	}
	if got["invitesEnabled"] != false {
		t.Fatalf("expected invitesEnabled=false, got %v", got["invitesEnabled"])
	}
}
