package auth

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// newTestServiceNoSeed opens a migrated store WITHOUT seeding roles, exposing the
// underlying *db.Queries so seed tests can inspect/manipulate raw rows.
func newTestServiceNoSeed(t *testing.T) (*Service, *db.Queries) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewService(st.Q(), time.Now), st.Q()
}

// newTestService opens a migrated store and seeds the system roles + registration
// policy defaults, so SetupOwner/Login/ResolveSession work end to end.
func newTestService(t *testing.T) (*Service, *db.Queries) {
	t.Helper()
	s, q := newTestServiceNoSeed(t)
	if err := s.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, q
}

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "hunter2") || VerifyPassword(h, "wrong") {
		t.Fatal("verify mismatch")
	}
}

func TestSetupRequiredLifecycle(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	req, _ := s.IsSetupRequired(ctx)
	if !req {
		t.Fatal("fresh DB should require setup")
	}
	if _, err := s.SetupOwner(ctx, "owner", "pw123456"); err != nil {
		t.Fatal(err)
	}
	req, _ = s.IsSetupRequired(ctx)
	if req {
		t.Fatal("setup should be complete after owner created")
	}
}

func TestSetupOwnerThenLogin(t *testing.T) {
	s, _ := newTestService(t) // helper that wires a migrated store + seeded system roles
	ctx := context.Background()
	uid, err := s.SetupOwner(ctx, "owner", "pw123456")
	if err != nil {
		t.Fatal(err)
	}
	if req, _ := s.IsSetupRequired(ctx); req {
		t.Fatal("setup should no longer be required")
	}
	got, err := s.Login(ctx, "OWNER", "pw123456") // username is case-insensitive
	if err != nil || got != uid {
		t.Fatalf("login failed: %v %s", err, got)
	}
	if _, err := s.Login(ctx, "owner", "wrong"); err != ErrInvalidCreds {
		t.Fatalf("want ErrInvalidCreds, got %v", err)
	}
}

func TestResolveSessionCarriesCaps(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	uid, _ := s.SetupOwner(ctx, "owner", "pw123456")
	tok, err := s.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	cu, err := s.ResolveSession(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ID != uid || !cu.IsOwner || !cu.Has(CapAdmin) {
		t.Fatalf("owner session wrong: %+v", cu)
	}
	if _, err := s.ResolveSession(ctx, "garbage"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestSessionExpires(t *testing.T) {
	s, _ := newTestServiceNoSeed(t)
	ctx := context.Background()
	current := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return current }
	if err := s.EnsureSeed(ctx); err != nil {
		t.Fatal(err)
	}

	uid, err := s.SetupOwner(ctx, "owner", "pw123456")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveSession(ctx, tok); err != nil {
		t.Fatalf("token should be valid before expiry: %v", err)
	}
	current = current.Add(sessionTTL + time.Hour)
	if _, err := s.ResolveSession(ctx, tok); err == nil {
		t.Fatal("token should be invalid after expiry")
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	uid, _ := s.SetupOwner(ctx, "owner", "pw123456")
	tok, err := s.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveSession(ctx, tok); err != nil {
		t.Fatalf("token should validate: %v", err)
	}
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveSession(ctx, tok); err == nil {
		t.Fatal("token should be invalid after logout")
	}
}

var _ = db.Session{}
