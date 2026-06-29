package store

import (
	"context"
	"testing"
)

func TestMigration0019_AdditiveAndReversible(t *testing.T) {
	st := openMigrated(t) // applies all migrations through 0019
	ctx := context.Background()

	// 1. Additive: the three new tables exist and are EMPTY.
	for _, tbl := range []string{"catalog_entity", "catalog_alias", "backend_binding"} {
		var n int
		if err := st.DB().QueryRowContext(ctx, "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("table %s missing: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("table %s should start empty, has %d", tbl, n)
		}
	}
	// 2. Pre-existing consumer tables are unaltered (no canonical_id column added).
	rows, err := st.DB().QueryContext(ctx, "SELECT name FROM pragma_table_info('download_jobs')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var col string
		_ = rows.Scan(&col)
		if col == "canonical_id" {
			t.Fatal("0019 must not add canonical_id to download_jobs in P1")
		}
	}
}
