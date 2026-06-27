package api

// Gap 2: 403 coverage for admin-only routes that lack a non-admin negative test.
//
// A requester-role user (role-requester) must be rejected with 403 on all
// invite management endpoints and the roles list/create endpoints.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupRequesterSession creates an owner (admin), then a requester-role user,
// and returns a logged-in session token for the requester.
func setupRequesterSession(t *testing.T, srv *Server) (ownerTok, requesterTok string) {
	t.Helper()
	mustSetupOwner(t, srv, "owner", "pw12345")
	ownerTok = mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", ownerTok, `{"username":"req","password":"reqpw123","roleId":"role-requester"}`)
	requesterTok = mustLogin(t, srv, "req", "reqpw123")
	return ownerTok, requesterTok
}

// TestNonAdminCannotListInvites asserts GET /invites returns 403 for a non-admin.
func TestNonAdminCannotListInvites(t *testing.T) {
	srv := newTestServer(t)
	_, rtok := setupRequesterSession(t, srv)
	if rr := doGET(t, srv, "/api/v1/invites", rtok); rr.Code != http.StatusForbidden {
		t.Fatalf("GET /invites with requester = %d, want 403: %s", rr.Code, rr.Body)
	}
}

// TestNonAdminCannotCreateInvite asserts POST /invites returns 403 for a non-admin.
func TestNonAdminCannotCreateInvite(t *testing.T) {
	srv := newTestServer(t)
	_, rtok := setupRequesterSession(t, srv)
	if rr := doPOST(t, srv, "/api/v1/invites", rtok, `{}`); rr.Code != http.StatusForbidden {
		t.Fatalf("POST /invites with requester = %d, want 403: %s", rr.Code, rr.Body)
	}
}

// TestNonAdminCannotDeleteInvite asserts DELETE /invites/{id} returns 403 for a non-admin.
func TestNonAdminCannotDeleteInvite(t *testing.T) {
	srv := newTestServer(t)
	otok, rtok := setupRequesterSession(t, srv)

	// Owner creates an invite so we have a valid ID to delete.
	rr := doPOST(t, srv, "/api/v1/invites", otok, `{}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("owner create invite = %d: %s", rr.Code, rr.Body)
	}
	var inv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &inv); err != nil || inv.ID == "" {
		t.Fatalf("decode invite id: %v / %s", err, rr.Body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/invites/%s", inv.ID), nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: rtok})
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE /invites/{id} with requester = %d, want 403: %s", rec.Code, rec.Body)
	}
}

// TestNonAdminCannotListRoles asserts GET /roles returns 403 for a non-admin.
func TestNonAdminCannotListRoles(t *testing.T) {
	srv := newTestServer(t)
	_, rtok := setupRequesterSession(t, srv)
	if rr := doGET(t, srv, "/api/v1/roles", rtok); rr.Code != http.StatusForbidden {
		t.Fatalf("GET /roles with requester = %d, want 403: %s", rr.Code, rr.Body)
	}
}

// TestNonAdminCannotCreateRole asserts POST /roles returns 403 for a non-admin.
func TestNonAdminCannotCreateRole(t *testing.T) {
	srv := newTestServer(t)
	_, rtok := setupRequesterSession(t, srv)
	if rr := doPOST(t, srv, "/api/v1/roles", rtok, `{"name":"DJ","capabilities":["request"]}`); rr.Code != http.StatusForbidden {
		t.Fatalf("POST /roles with requester = %d, want 403: %s", rr.Code, rr.Body)
	}
}
