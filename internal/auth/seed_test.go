package auth

import (
	"context"
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
