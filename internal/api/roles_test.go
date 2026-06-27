package api

import (
	"encoding/json"
	"testing"
)

func TestRolesCrudAndProtection(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// create custom role
	rr := doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["can_create_playlists","can_download"]}`)
	if rr.Code != 201 {
		t.Fatalf("create role = %d (%s)", rr.Code, rr.Body)
	}
	// invalid capability rejected
	if doPOST(t, srv, "/api/v1/roles", tok, `{"name":"Bad","capabilities":["can_teleport"]}`).Code != 400 {
		t.Fatal("invalid cap must 400")
	}
	// system role capabilities are read-only
	if doPATCH(t, srv, "/api/v1/roles/role-admin", tok, `{"name":"Admin","capabilities":["can_request"]}`).Code != 409 {
		t.Fatal("editing system role must 409")
	}
	// system role undeletable
	if doDELETE(t, srv, "/api/v1/roles/role-user", tok).Code != 409 {
		t.Fatal("deleting system role must 409")
	}
}

func TestCapabilitiesMetadata(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	rr := doGET(t, srv, "/api/v1/capabilities", tok)
	var caps []struct{ Key, Label string }
	json.Unmarshal(rr.Body.Bytes(), &caps)
	if len(caps) != 6 {
		t.Fatalf("want 6 capabilities, got %d", len(caps))
	}
}
