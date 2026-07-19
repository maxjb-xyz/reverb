package store

import "testing"

// TestMigration0023_Lyrics verifies the lyrics cache table exists with the
// expected columns after migrating a fresh DB.
func TestMigration0023_Lyrics(t *testing.T) {
	st := openMigrated(t)
	rows, err := st.DB().Query(`SELECT track_key, synced, body, source, fetched_at FROM lyrics LIMIT 1`)
	if err != nil {
		t.Fatalf("lyrics table must exist with expected columns (migration 0023): %v", err)
	}
	rows.Close()
}
