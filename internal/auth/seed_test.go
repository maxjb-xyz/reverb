package auth

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func TestEnsureSeedIsIdempotentAndSeedsRoles(t *testing.T) {
	s, q := newTestServiceNoSeed(t) // migrated store, NO seed yet
	ctx := context.Background()
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSeed(ctx); err != nil { // second call must be a no-op, not a UNIQUE error
		t.Fatalf("second seed failed: %v", err)
	}
	roles, _ := q.ListRoles(ctx)
	if len(roles) != 3 {
		t.Fatalf("want 3 system roles, got %d", len(roles))
	}
	if v, _ := q.GetSetting(ctx, "default_role_id"); v != "role-user" {
		t.Errorf("default_role_id = %q", v)
	}
}

func TestEnsureSeedRemapsLegacyCapabilities(t *testing.T) {
	s, q := newTestServiceNoSeed(t)
	ctx := context.Background()
	// simulate an SP1-era install: a role carrying the OLD keys
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "role-user", Name: "User", IsSystem: 1, Capabilities: `["can_download","can_request","can_create_playlists"]`}); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateRole(ctx, db.CreateRoleParams{ID: "role-requester", Name: "Requester", IsSystem: 1, Capabilities: `["can_request"]`}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	caps := func(id string) []string {
		r, _ := q.GetRole(ctx, id)
		var c []string
		_ = json.Unmarshal([]byte(r.Capabilities), &c)
		return c
	}
	user := caps("role-user")
	if contains(user, "can_download") || contains(user, "can_request") {
		t.Errorf("old keys not remapped: %v", user)
	}
	if !contains(user, "auto_approve") || !contains(user, "request") {
		t.Errorf("new keys missing: %v", user)
	}
	if !contains(caps("role-requester"), "can_create_playlists") {
		t.Errorf("requester did not gain create_playlists: %v", caps("role-requester"))
	}
	// idempotent: a second run is a no-op, not an error
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatalf("second EnsureSeed failed: %v", err)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func TestEnsureSeedMigratesLegacyAdmin(t *testing.T) {
	s, q := newTestServiceNoSeed(t)
	ctx := context.Background()
	h, _ := HashPassword("legacy-pw")
	_ = q.UpsertSetting(ctx, db.UpsertSettingParams{Key: "admin_password_hash", Value: h})
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}
	uid, err := s.Login(ctx, "admin", "legacy-pw") // existing password keeps working
	if err != nil {
		t.Fatalf("legacy login failed: %v", err)
	}
	u, _ := q.GetUserByID(ctx, uid)
	if u.IsOwner != 1 || u.RoleID != "role-admin" {
		t.Errorf("migrated admin not owner/admin: %+v", u)
	}
}
