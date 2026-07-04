package auth

// Gap 1: anti-lockout guard tests for UpdateUserRole, DeleteUser, SetUserDisabled.
//
// Strategy: seed roles only (no SetupOwner / no owner row), then insert a
// non-owner user assigned role-admin via the store's CreateUser directly so
// there is exactly one enabled admin and no owner. The guard must fire on all
// three mutation sites.

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// makeSoleNonOwnerAdmin seeds a fresh service+store (roles present, no owner),
// inserts a single user with role-admin (is_owner=0), and returns that user's ID.
func makeSoleNonOwnerAdmin(t *testing.T) (*Service, *db.Queries, string) {
	t.Helper()
	s, q := newTestService(t) // seeds roles; no owner yet
	ctx := context.Background()
	// We bypass s.CreateUser because it forces is_owner=0 (fine) but we want to
	// use the db query directly to avoid any future guards that might be added to
	// the service layer. is_owner=0 so no owner row exists.
	const id = "user-sole-admin"
	h, err := HashPassword("pw123456")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           id,
		Username:     "soleadmin",
		PasswordHash: h,
		RoleID:       "role-admin",
		IsOwner:      0,
	}); err != nil {
		t.Fatal(err)
	}
	return s, q, id
}

// TestAntiLockoutDeleteUser: deleting the sole enabled non-owner admin returns ErrLastAdmin.
func TestAntiLockoutDeleteUser(t *testing.T) {
	s, _, id := makeSoleNonOwnerAdmin(t)
	if err := s.DeleteUser(context.Background(), id); err != ErrLastAdmin {
		t.Fatalf("DeleteUser sole admin: want ErrLastAdmin, got %v", err)
	}
}

// TestAntiLockoutSetUserDisabled: disabling the sole enabled non-owner admin returns ErrLastAdmin.
func TestAntiLockoutSetUserDisabled(t *testing.T) {
	s, _, id := makeSoleNonOwnerAdmin(t)
	if err := s.SetUserDisabled(context.Background(), id, true); err != ErrLastAdmin {
		t.Fatalf("SetUserDisabled sole admin: want ErrLastAdmin, got %v", err)
	}
}

// TestAntiLockoutUpdateUserRole: demoting the sole enabled non-owner admin to a
// non-admin role returns ErrLastAdmin.
func TestAntiLockoutUpdateUserRole(t *testing.T) {
	s, _, id := makeSoleNonOwnerAdmin(t)
	ctx := context.Background()
	if err := s.UpdateUserRole(ctx, id, "role-user"); err != ErrLastAdmin {
		t.Fatalf("UpdateUserRole sole admin to role-user: want ErrLastAdmin, got %v", err)
	}
	if err := s.UpdateUserRole(ctx, id, "role-requester"); err != ErrLastAdmin {
		t.Fatalf("UpdateUserRole sole admin to role-requester: want ErrLastAdmin, got %v", err)
	}
}

// TestAntiLockoutNoFalsePositiveDeleteUser: when two non-owner admins exist,
// deleting one must SUCCEED (no false-positive lockout).
func TestAntiLockoutNoFalsePositiveDeleteUser(t *testing.T) {
	s, q, id1 := makeSoleNonOwnerAdmin(t)
	ctx := context.Background()
	// add a second admin
	const id2 = "user-second-admin"
	h, _ := HashPassword("pw2")
	if err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           id2,
		Username:     "secondadmin",
		PasswordHash: h,
		RoleID:       "role-admin",
		IsOwner:      0,
	}); err != nil {
		t.Fatal(err)
	}
	// now deleting the first admin must succeed
	if err := s.DeleteUser(ctx, id1); err != nil {
		t.Fatalf("DeleteUser with two admins: want nil, got %v", err)
	}
}
