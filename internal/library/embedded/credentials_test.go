package embedded

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestEnsureInternalCredentials_GeneratesOnceAndPersists(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	c1, err := EnsureInternalCredentials(ctx, st.Q())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if c1.Username != AdminUsername {
		t.Errorf("username = %q, want %q", c1.Username, AdminUsername)
	}
	if len(c1.Password) < 16 {
		t.Errorf("password too short: %q", c1.Password)
	}

	c2, err := EnsureInternalCredentials(ctx, st.Q())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c2.Password != c1.Password {
		t.Errorf("password changed across calls: %q vs %q", c1.Password, c2.Password)
	}
}
