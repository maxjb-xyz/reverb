package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func openMigrated(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLibraryVersionDefaultsToOne(t *testing.T) {
	st := openMigrated(t)
	v, err := st.LibraryVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("library_version = %d, want 1", v)
	}
}

func TestLibraryVersionSetAndGet(t *testing.T) {
	st := openMigrated(t)
	if err := st.Q().SetLibraryVersion(context.Background(), "5"); err != nil {
		t.Fatal(err)
	}
	v, err := st.LibraryVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("library_version = %d, want 5", v)
	}
}

func TestSetAndGetLibraryVersion(t *testing.T) {
	st, err := Open(t.TempDir() + "/lv.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.SetLibraryVersion(ctx, 7); err != nil {
		t.Fatal(err)
	}
	v, err := st.LibraryVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 7 {
		t.Fatalf("library_version = %d, want 7", v)
	}
}

func TestDownloadJobRoundTrip(t *testing.T) {
	st, err := Open(t.TempDir() + "/dj.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()

	if err := q.InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID: "j1", DedupKey: "dk1", RequestJson: `{"title":"Song"}`, DownloaderName: "spotdl",
		Status: "queued", Progress: 0, Error: "", OutputPath: "",
		LibraryTrackID: sql.NullString{}, Priority: 0, RequestedBy: sql.NullString{}, Attempts: 0,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := q.GetDownloadJob(ctx, "j1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DedupKey != "dk1" || got.Status != "queued" || got.DownloaderName != "spotdl" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Dedup-join lookup finds the active (queued) job.
	active, err := q.GetActiveDownloadJobByDedup(ctx, "dk1")
	if err != nil {
		t.Fatalf("active lookup: %v", err)
	}
	if active.ID != "j1" {
		t.Fatalf("active = %q, want j1", active.ID)
	}

	// Move to running, then completed; finished_at must be set.
	if err := q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: "running", ID: "j1",
	}); err != nil {
		t.Fatalf("status running: %v", err)
	}
	if err := q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: "completed", ID: "j1",
	}); err != nil {
		t.Fatalf("status completed: %v", err)
	}
	done, _ := q.GetDownloadJob(ctx, "j1")
	if !done.FinishedAt.Valid || !done.StartedAt.Valid {
		t.Fatalf("started/finished not set: %+v", done)
	}

	// A completed job is no longer "active" for dedup-join.
	if _, err := q.GetActiveDownloadJobByDedup(ctx, "dk1"); err != sql.ErrNoRows {
		t.Fatalf("completed job should not be active, err=%v", err)
	}
}

func TestMatchCacheUpsertPositiveAndNegative(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	// Positive match.
	if err := q.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
		Source: "spotify", ExternalID: "sp1",
		LibraryTrackID: sql.NullString{String: "t1", Valid: true},
		Method:         "isrc", Confidence: 1, Isrc: "USX1", Mbid: "", DurationMs: 210000, LibraryVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp1"})
	if err != nil {
		t.Fatal(err)
	}
	if !row.LibraryTrackID.Valid || row.LibraryTrackID.String != "t1" || row.Method != "isrc" {
		t.Fatalf("positive row: %+v", row)
	}

	// Negative match (library_track_id NULL).
	if err := q.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
		Source: "spotify", ExternalID: "sp2",
		LibraryTrackID: sql.NullString{Valid: false},
		Method:         "none", Confidence: 0, LibraryVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	neg, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp2"})
	if err != nil {
		t.Fatal(err)
	}
	if neg.LibraryTrackID.Valid {
		t.Fatalf("negative row should have NULL library_track_id: %+v", neg)
	}

	// DeleteBySource clears both.
	if err := q.DeleteMatchCacheBySource(ctx, "spotify"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp1"}); err == nil {
		t.Fatal("expected ErrNoRows after delete")
	}
}

func TestAlbumCoverageRoundTrip(t *testing.T) {
	st := openMigrated(t)
	q := st.Q()
	ctx := context.Background()
	if err := q.UpsertAlbumCoverage(ctx, db.UpsertAlbumCoverageParams{
		Source: "spotify", ExternalAlbumID: "AL", CoverageJson: `{"state":"full"}`,
		LibraryAlbumID: "L1", FetchedAt: 123,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := q.GetAlbumCoverage(ctx, db.GetAlbumCoverageParams{Source: "spotify", ExternalAlbumID: "AL"})
	if err != nil || row.CoverageJson != `{"state":"full"}` || row.LibraryAlbumID != "L1" {
		t.Fatalf("round-trip failed: %+v err=%v", row, err)
	}
}

func TestSyncedPlaylistRoundTrip(t *testing.T) {
	st := openMigrated(t)
	q := st.Q()
	ctx := context.Background()
	row, err := q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID: "sp1", Source: "spotify", ExternalID: "ext1", Name: "Chill",
		CoverUrl: "http://img", TracksJson: `[]`, CreatedAt: 100,
	})
	if err != nil || row.Name != "Chill" {
		t.Fatalf("upsert: %+v err=%v", row, err)
	}
	// Upsert again with same (source, external_id) updates, not duplicates.
	if _, err := q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID: "sp1", Source: "spotify", ExternalID: "ext1", Name: "Renamed", TracksJson: `[]`, CreatedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := q.ListSyncedPlaylists(ctx)
	if len(all) != 1 || all[0].Name != "Renamed" {
		t.Fatalf("want 1 row 'Renamed', got %+v", all)
	}
}

func TestAdapterInstanceCRUD(t *testing.T) {
	st, err := Open(t.TempDir() + "/ai.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()

	id := uuid.NewString()
	if err := q.CreateAdapterInstance(ctx, db.CreateAdapterInstanceParams{
		ID: id, Type: "search", Name: "spotify", Enabled: 1, Priority: 0,
		ConfigJson: `{"client_id":"abc","client_secret":"shh"}`,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := q.GetAdapterInstance(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "spotify" || got.Enabled != 1 {
		t.Fatalf("get mismatch: %+v", got)
	}

	if err := q.UpdateAdapterInstance(ctx, db.UpdateAdapterInstanceParams{
		Name: "spotify", Enabled: 1, Priority: 5, ConfigJson: `{"client_id":"new"}`, ID: id,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Priority != 5 || got.ConfigJson != `{"client_id":"new"}` {
		t.Fatalf("update did not persist: %+v", got)
	}

	if err := q.SetAdapterInstanceEnabled(ctx, db.SetAdapterInstanceEnabledParams{Enabled: 0, ID: id}); err != nil {
		t.Fatalf("set-enabled: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Enabled != 0 {
		t.Fatalf("enabled not toggled: %+v", got)
	}

	if err := q.SetAdapterInstancePriority(ctx, db.SetAdapterInstancePriorityParams{Priority: 9, ID: id}); err != nil {
		t.Fatalf("set-priority: %v", err)
	}
	got, _ = q.GetAdapterInstance(ctx, id)
	if got.Priority != 9 {
		t.Fatalf("priority not set: %+v", got)
	}

	if err := q.DeleteAdapterInstance(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := q.GetAdapterInstance(ctx, id); err == nil {
		t.Fatal("expected error getting a deleted instance")
	}
}
