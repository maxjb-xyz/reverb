package download

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store"
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
