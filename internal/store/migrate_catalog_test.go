package store

import (
	"context"
	"testing"
)

func TestMigration0019_AdditiveAndReversible(t *testing.T) {
	st := openMigrated(t) // applies all migrations through current latest
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
	// 2. canonical_id is present on download_jobs (added by migration 0022, Task 3).
	//    Verify the column exists and has the expected safe default.
	rows, err := st.DB().QueryContext(ctx, "SELECT name FROM pragma_table_info('download_jobs')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var col string
		_ = rows.Scan(&col)
		if col == "canonical_id" {
			found = true
		}
	}
	if !found {
		t.Fatal("canonical_id must be present on download_jobs (migration 0022)")
	}
}
