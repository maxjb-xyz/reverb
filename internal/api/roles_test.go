package api

import (
	"encoding/json"
	"testing"
)

func TestRolesCrudAndProtection(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// create custom role (using current valid cap keys)
	rr := doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["can_create_playlists","auto_approve"]}`)
	if rr.Code != 201 {
		t.Fatalf("create role = %d (%s)", rr.Code, rr.Body)
	}
	// invalid capability rejected
	if doPOST(t, srv, "/api/v1/roles", tok, `{"name":"Bad","capabilities":["can_teleport"]}`).Code != 400 {
		t.Fatal("invalid cap must 400")
	}
	// editing a default role is now allowed (no longer 409 — anti-lockout guards instead)
	rr = doPATCH(t, srv, "/api/v1/roles/role-user", tok, `{"name":"Member","capabilities":["request","can_create_playlists"]}`)
	if rr.Code != 200 {
		t.Fatalf("editing a default (non-admin) role should now succeed, got %d (%s)", rr.Code, rr.Body)
	}
	// deleting role-user is blocked because it is the registration default, not because it is a system role
	if doDELETE(t, srv, "/api/v1/roles/role-user", tok).Code != 409 {
		t.Fatal("deleting the registration-default role must 409 (ErrRoleIsDefault)")
	}
}

func TestDefaultRolesAreEditable(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	// rename + retag a SYSTEM role (was 409 before) — now allowed
	rr := doPATCH(t, srv, "/api/v1/roles/role-user", tok, `{"name":"Member","capabilities":["request","can_create_playlists"]}`)
	if rr.Code != 200 {
		t.Fatalf("editing a default role should succeed now, got %d (%s)", rr.Code, rr.Body)
	}
}

func TestAutoApproveImpliesRequest(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["auto_approve"]}`)
	rr := doGET(t, srv, "/api/v1/roles", tok)
	var roles []struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &roles); err != nil {
		t.Fatalf("failed to decode roles: %v", err)
	}
	var djCaps []string
	for _, r := range roles {
		if r.Name == "DJ" {
			djCaps = r.Capabilities
			break
		}
	}
	if djCaps == nil {
		t.Fatalf("DJ role not found in response: %s", rr.Body)
	}
	hasRequest, hasAutoApprove := false, false
	for _, c := range djCaps {
		if c == "request" {
			hasRequest = true
		}
		if c == "auto_approve" {
			hasAutoApprove = true
		}
	}
	if !hasRequest || !hasAutoApprove {
		t.Fatalf("DJ role caps = %v; want both auto_approve and request", djCaps)
	}
}

func TestAntiLockout(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")
	// owner is the only admin; removing is_admin from the Admin role must 409
	rr := doPATCH(t, srv, "/api/v1/roles/role-admin", tok, `{"name":"Admin","capabilities":["can_manage_users","can_manage_library","request","auto_approve","can_create_playlists"]}`)
	if rr.Code != 409 {
		t.Fatalf("stripping is_admin from the only admin role must 409, got %d", rr.Code)
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

func TestUpdateRoleEmptyNameReturns400(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	tok := mustLogin(t, srv, "owner", "pw12345")

	// Create a custom role first
	rr := doPOST(t, srv, "/api/v1/roles", tok, `{"name":"DJ","capabilities":["can_create_playlists"]}`)
	if rr.Code != 201 {
		t.Fatalf("create role = %d (%s)", rr.Code, rr.Body)
	}
	var created struct{ ID string }
	json.Unmarshal(rr.Body.Bytes(), &created)

	// PATCH with empty name → 400
	rr = doPATCH(t, srv, "/api/v1/roles/"+created.ID, tok, `{"name":"","capabilities":["can_create_playlists"]}`)
	if rr.Code != 400 {
		t.Fatalf("update role empty name = %d, want 400 (body: %s)", rr.Code, rr.Body)
	}
}
