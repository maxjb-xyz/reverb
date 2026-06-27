package download

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store"
	_ "modernc.org/sqlite"
)

func newSQLStore(t *testing.T) JobStore {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/ss.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewSQLStore(st.Q())
}

func TestSQLStoreInsertGetUpdate(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	job := core.DownloadJob{
		ID: "j1", DedupKey: "dk1", Status: core.DownloadQueued, DownloaderName: "spotdl",
		Source: "spotify", ExternalID: "e1", Progress: 0, PlayWhenReady: true,
	}
	req := core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "Daft Punk", Title: "One More Time",
		Album: "Discovery", ISRC: "US-XYZ-01", Downloader: "spotdl", PlayWhenReady: true,
	}
	if err := s.Insert(ctx, job, req); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "j1")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	// The FULL request rehydrates from request_json (so a loaded job can run).
	if got.Artist != "Daft Punk" || got.Title != "One More Time" || got.Album != "Discovery" {
		t.Fatalf("full request not rehydrated from request_json: %+v", got)
	}
	if got.ISRC != "US-XYZ-01" || !got.PlayWhenReady {
		t.Fatalf("request fields not rehydrated: %+v", got)
	}
	if got.DedupKey != "dk1" || got.Status != core.DownloadQueued {
		t.Fatalf("mismatch: %+v", got)
	}

	got.Status = core.DownloadRunning
	got.Progress = 60
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	active, ok, err := s.ActiveByDedup(ctx, "dk1")
	if err != nil || !ok {
		t.Fatalf("active: %v ok=%v", err, ok)
	}
	if active.Progress != 60 || active.Status != core.DownloadRunning {
		t.Fatalf("active mismatch: %+v", active)
	}
	// Re-Get to confirm started_at was populated on the running transition.
	running, _, err := s.Get(ctx, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if running.StartedAt == 0 {
		t.Fatalf("StartedAt must be non-zero after transitioning to running: %+v", running)
	}

	got.Status = core.DownloadCompleted
	got.LibraryTrackID = "t9"
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveByDedup(ctx, "dk1"); ok {
		t.Fatal("completed job must not be active")
	}
	fin, _, _ := s.Get(ctx, "j1")
	if fin.LibraryTrackID != "t9" {
		t.Fatalf("library_track_id not persisted: %+v", fin)
	}
	// Re-Get to confirm finished_at was populated on the completed transition.
	completed, _, err := s.Get(ctx, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if completed.FinishedAt == 0 {
		t.Fatalf("FinishedAt must be non-zero after transitioning to completed: %+v", completed)
	}

	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}

// mustSeedUser inserts a minimal role + user row (raw SQL on a second connection)
// so a download_jobs.initiated_by FK reference to userID is satisfiable.
func mustSeedUser(t *testing.T, dbPath, userID string) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`INSERT OR IGNORE INTO roles (id, name, is_system, capabilities) VALUES ('role-test', 'Test', 0, '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO users (id, username, password_hash, role_id, is_owner) VALUES (?, ?, 'x', 'role-test', 0)`, userID, "u-"+userID); err != nil {
		t.Fatal(err)
	}
}

// TestSQLStoreInsertPersistsInitiatedBy verifies the request's InitiatedBy is
// written to download_jobs.initiated_by (the attribution column). The column is
// write-only (not read back into core.DownloadJob), so this asserts on the raw row.
func TestSQLStoreInsertPersistsInitiatedBy(t *testing.T) {
	path := t.TempDir() + "/attr.db"
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	s := NewSQLStore(st.Q())
	ctx := context.Background()
	// initiated_by is a FK → users(id); seed a real user (matches production, where
	// the id comes from the resolved session) so the attribution insert satisfies it.
	mustSeedUser(t, path, "user-123")
	job := core.DownloadJob{ID: "j-attr", DedupKey: "dk-attr", Status: core.DownloadQueued, DownloaderName: "spotdl", Source: "spotify", ExternalID: "e-attr"}
	req := core.DownloadRequest{Source: "spotify", ExternalID: "e-attr", InitiatedBy: "user-123"}
	if err := s.Insert(ctx, job, req); err != nil {
		t.Fatal(err)
	}
	// Read the raw column via a second connection to the same DB file.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var initiatedBy sql.NullString
	if err := raw.QueryRowContext(ctx, "SELECT initiated_by FROM download_jobs WHERE id = ?", "j-attr").Scan(&initiatedBy); err != nil {
		t.Fatal(err)
	}
	if !initiatedBy.Valid || initiatedBy.String != "user-123" {
		t.Fatalf("initiated_by = %+v, want valid 'user-123'", initiatedBy)
	}
}

func TestSQLStoreDownloaderRefRoundTrip(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	job := core.DownloadJob{ID: "r1", DedupKey: "dk", Status: core.DownloadRunning, DownloaderName: "lidarr", Source: "spotify", ExternalID: "e1"}
	if err := s.Insert(ctx, job, core.DownloadRequest{Source: "spotify", ExternalID: "e1", Album: "Discovery", Artist: "Daft Punk"}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, "r1"); got.DownloaderRef != "" {
		t.Fatalf("new job should have empty ref, got %q", got.DownloaderRef)
	}
	if err := s.UpdateRef(ctx, "r1", "lidarr-album-42"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if got.DownloaderRef != "lidarr-album-42" {
		t.Fatalf("ref not persisted: %q", got.DownloaderRef)
	}
}

func TestSQLStoreDeleteAndDeleteFinished(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	mk := func(id, status string) {
		j := core.DownloadJob{ID: id, DedupKey: "dk-" + id, Status: core.DownloadStatus(status), DownloaderName: "spotdl", Source: "spotify", ExternalID: id}
		if err := s.Insert(ctx, j, core.DownloadRequest{Source: "spotify", ExternalID: id}); err != nil {
			t.Fatal(err)
		}
		// Insert persists status via the status column on the row's initial write;
		// set the non-queued statuses explicitly through Update.
		j.Status = core.DownloadStatus(status)
		if err := s.Update(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "completed")
	mk("b", "failed")
	mk("c", "queued")
	mk("d", "canceled")

	// Delete a single job.
	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "a"); ok {
		t.Fatal("job a should be deleted")
	}

	// DeleteFinished removes terminal jobs (b failed, d canceled), keeps queued c.
	ids, err := s.DeleteFinished(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("DeleteFinished returned %v, want 2 ids (b,d)", ids)
	}
	// Assert the returned ids are EXACTLY {"b","d"} — not just the count.
	sortedIDs := make([]string, len(ids))
	copy(sortedIDs, ids)
	sort.Strings(sortedIDs)
	if len(sortedIDs) != 2 || sortedIDs[0] != "b" || sortedIDs[1] != "d" {
		t.Fatalf("DeleteFinished returned %v, want exactly [b d]", ids)
	}
	if _, ok, _ := s.Get(ctx, "c"); !ok {
		t.Fatal("queued job c must survive DeleteFinished")
	}
	if _, ok, _ := s.Get(ctx, "b"); ok {
		t.Fatal("failed job b should be gone")
	}
}
