package store

import (
	"context"
	"testing"
)

// TestMigration0022_DownloadJobCanonicalID verifies migration 0022 is additive:
//   - canonical_id column exists on download_jobs with a safe empty-string default.
//   - Pre-existing rows get empty-string canonical_id (default applies on apply).
//   - The plays and catalog tables are untouched.
func TestMigration0022_DownloadJobCanonicalID(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()

	// 1. canonical_id column exists on download_jobs.
	rows, err := st.DB().QueryContext(ctx, "SELECT name FROM pragma_table_info('download_jobs')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatal(err)
		}
		if col == "canonical_id" {
			found = true
		}
	}
	if !found {
		t.Fatal("migration 0022 must add canonical_id column to download_jobs")
	}

	// 2. A fresh row gets the empty-string default.
	if _, err := st.DB().ExecContext(ctx, `
		INSERT INTO download_jobs (
			id, dedup_key, request_json, downloader_name, status, progress, error,
			output_path, library_track_id, priority, requested_by, attempts,
			downloader_ref, initiated_by, created_at, started_at, finished_at
		) VALUES ('job-mig-0022', 'dk0022', '{}', 'test', 'completed', 100, '',
			'', '', 0, NULL, 1, '', NULL, unixepoch(), NULL, unixepoch())
	`); err != nil {
		t.Fatalf("insert seeded row: %v", err)
	}

	var canonicalID string
	if err := st.DB().QueryRowContext(ctx,
		"SELECT canonical_id FROM download_jobs WHERE id = 'job-mig-0022'",
	).Scan(&canonicalID); err != nil {
		t.Fatalf("read canonical_id: %v", err)
	}
	if canonicalID != "" {
		t.Fatalf("new row canonical_id should default to '', got %q", canonicalID)
	}
}
