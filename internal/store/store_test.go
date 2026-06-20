package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maximusjb/crate/internal/store/db"
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
