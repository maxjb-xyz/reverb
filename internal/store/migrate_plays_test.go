package store

import (
	"context"
	"testing"
)

func TestMigration0020_Additive(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	var n int
	if err := st.DB().QueryRowContext(ctx, "SELECT count(*) FROM plays").Scan(&n); err != nil {
		t.Fatalf("plays table missing: %v", err)
	}
	if n != 0 {
		t.Fatalf("plays should start empty, got %d", n)
	}
}
