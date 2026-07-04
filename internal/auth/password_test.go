package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		want error
	}{
		{"empty", "", ErrPasswordTooShort},
		{"7 chars is too short", "short12", ErrPasswordTooShort},
		{"8 chars is exactly the minimum", "abcdefgh", nil},
		{"comfortable passphrase", "a-good-passphrase", nil},
		{"over the bcrypt limit", string(make([]byte, 73)), ErrPasswordTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidatePassword(c.pw); !errors.Is(got, c.want) {
				t.Fatalf("ValidatePassword(%q) = %v, want %v", c.pw, got, c.want)
			}
		})
	}
}

func newAuthSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/pw.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st.Q(), time.Now)
	if err := svc.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestSetupOwnerRejectsWeakPassword(t *testing.T) {
	svc := newAuthSvc(t)
	if _, err := svc.SetupOwner(context.Background(), "owner", "short12"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("SetupOwner weak = %v, want ErrPasswordTooShort", err)
	}
	// A compliant password still succeeds.
	if _, err := svc.SetupOwner(context.Background(), "owner", "goodpassword"); err != nil {
		t.Fatalf("SetupOwner strong = %v, want nil", err)
	}
}

func TestChangeOwnPasswordRejectsWeakNext(t *testing.T) {
	svc := newAuthSvc(t)
	ctx := context.Background()
	uid, err := svc.SetupOwner(ctx, "owner", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	// Correct current password, but the new one is too short → policy error (not ErrInvalidCreds).
	if err := svc.ChangeOwnPassword(ctx, uid, "goodpassword", "weak12"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("ChangeOwnPassword weak next = %v, want ErrPasswordTooShort", err)
	}
}
