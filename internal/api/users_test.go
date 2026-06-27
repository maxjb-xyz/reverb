package api

import "testing"

func TestAdminCreatesUserAndOwnerProtected(t *testing.T) {
	srv := newTestServer(t)
	ownerID := mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// create a regular user
	rr := doPOST(t, srv, "/api/v1/users", tok, `{"username":"bob","password":"bobpw123","roleId":"role-user"}`)
	if rr.Code != 201 {
		t.Fatalf("create user = %d (%s)", rr.Code, rr.Body)
	}
	// bob can log in
	if doPOST(t, srv, "/api/v1/auth/login", "", `{"username":"bob","password":"bobpw123"}`).Code != 200 {
		t.Fatal("bob should be able to log in")
	}
	// owner cannot be deleted
	if doDELETE(t, srv, "/api/v1/users/"+ownerID, tok).Code != 409 {
		t.Fatal("owner delete must 409")
	}
	// owner cannot be demoted
	if doPATCH(t, srv, "/api/v1/users/"+ownerID, tok, `{"roleId":"role-user"}`).Code != 409 {
		t.Fatal("owner demotion must 409")
	}
}

func TestNonAdminCannotManageUsers(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"req","password":"reqpw123","roleId":"role-requester"}`)
	rtok := mustLogin(t, srv, "req", "reqpw123")
	if doGET(t, srv, "/api/v1/users", rtok).Code != 403 {
		t.Fatal("requester must be forbidden from listing users")
	}
}
