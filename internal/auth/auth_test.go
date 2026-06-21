package auth

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewService(st.Q(), time.Now)
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
	s := newTestService(t)
	ctx := context.Background()
	req, _ := s.IsSetupRequired(ctx)
	if !req {
		t.Fatal("fresh DB should require setup")
	}
	if err := s.SetAdminPassword(ctx, "pw"); err != nil {
		t.Fatal(err)
	}
	req, _ = s.IsSetupRequired(ctx)
	if req {
		t.Fatal("setup should be complete after password set")
	}
}

func TestLoginAndSession(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	_ = s.SetAdminPassword(ctx, "pw")

	ok, _ := s.CheckLogin(ctx, "pw")
	if !ok {
		t.Fatal("login should succeed")
	}
	if bad, _ := s.CheckLogin(ctx, "nope"); bad {
		t.Fatal("login should fail")
	}

	tok, err := s.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := s.ValidateToken(ctx, tok); !valid {
		t.Fatal("token should validate")
	}
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if valid, _ := s.ValidateToken(ctx, tok); valid {
		t.Fatal("token should be invalid after logout")
	}
}

func TestSessionExpires(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/exp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	current := time.Unix(1_000_000, 0)
	s := NewService(st.Q(), func() time.Time { return current })
	ctx := context.Background()

	tok, err := s.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ValidateToken(ctx, tok); !ok {
		t.Fatal("token should be valid before expiry")
	}
	current = current.Add(sessionTTL + time.Hour)
	if ok, _ := s.ValidateToken(ctx, tok); ok {
		t.Fatal("token should be invalid after expiry")
	}
}

func TestCheckLoginNoPasswordSet(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if ok, err := s.CheckLogin(ctx, "anything"); ok || err == nil {
		t.Fatalf("CheckLogin with no admin password: want (false, error), got (%v, %v)", ok, err)
	}
}

var _ = db.Session{}
