package download

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store/db"
)

// sqlStore adapts *db.Queries to JobStore, mapping core.DownloadJob ⇄ db rows.
type sqlStore struct{ q *db.Queries }

// Compile-time assertion: sqlStore must satisfy JobStore.
var _ JobStore = (*sqlStore)(nil)

// NewSQLStore wraps generated queries as a Manager JobStore.
func NewSQLStore(q *db.Queries) JobStore { return &sqlStore{q: q} }

// toCore converts a db.DownloadJob row to a core.DownloadJob, rehydrating all
// request fields from request_json so a job loaded from SQLite can run.
// Returns an error if request_json is non-empty but cannot be decoded — a
// corrupted row must not silently yield a job with empty request fields.
func toCore(r db.DownloadJob) (core.DownloadJob, error) {
	j := core.DownloadJob{
		ID:             r.ID,
		DedupKey:       r.DedupKey,
		Status:         core.DownloadStatus(r.Status),
		Progress:       int(r.Progress),
		Error:          r.Error,
		OutputPath:     r.OutputPath,
		DownloaderName: r.DownloaderName,
		Priority:       int(r.Priority),
		Attempts:       int(r.Attempts),
		CreatedAt:      r.CreatedAt,
	}
	if r.LibraryTrackID.Valid {
		j.LibraryTrackID = r.LibraryTrackID.String
	}
	if r.StartedAt.Valid {
		j.StartedAt = r.StartedAt.Int64
	}
	if r.FinishedAt.Valid {
		j.FinishedAt = r.FinishedAt.Int64
	}
	// The FULL request is carried in request_json; rehydrate every field so a job
	// loaded from SQLite has enough to run (artist/title/album/source/externalId/
	// isrc/playWhenReady — the explicit downloader is reflected by DownloaderName).
	var req core.DownloadRequest
	if r.RequestJson != "" {
		if err := jsonUnmarshal(r.RequestJson, &req); err != nil {
			return core.DownloadJob{}, fmt.Errorf("download job %s: decode request_json: %w", r.ID, err)
		}
	}
	j.Source = req.Source
	j.ExternalID = req.ExternalID
	j.Artist = req.Artist
	j.Title = req.Title
	j.Album = req.Album
	j.ISRC = req.ISRC
	j.PlayWhenReady = req.PlayWhenReady
	return j, nil
}

// Insert persists the job lifecycle row AND marshals the COMPLETE originating
// core.DownloadRequest into request_json, so toCore can rehydrate a runnable job.
func (s *sqlStore) Insert(ctx context.Context, j core.DownloadJob, req core.DownloadRequest) error {
	return s.q.InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID:             j.ID,
		DedupKey:       j.DedupKey,
		RequestJson:    requestJSON(req),
		DownloaderName: j.DownloaderName,
		Status:         string(j.Status),
		Progress:       int64(j.Progress),
		Error:          j.Error,
		OutputPath:     j.OutputPath,
		LibraryTrackID: nullString(j.LibraryTrackID),
		Priority:       int64(j.Priority),
		RequestedBy:    sql.NullString{},
		Attempts:       int64(j.Attempts),
	})
}

func (s *sqlStore) Get(ctx context.Context, id string) (core.DownloadJob, bool, error) {
	r, err := s.q.GetDownloadJob(ctx, id)
	if err == sql.ErrNoRows {
		return core.DownloadJob{}, false, nil
	}
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	j, err := toCore(r)
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	return j, true, nil
}

func (s *sqlStore) ActiveByDedup(ctx context.Context, dedup string) (core.DownloadJob, bool, error) {
	r, err := s.q.GetActiveDownloadJobByDedup(ctx, dedup)
	if err == sql.ErrNoRows {
		return core.DownloadJob{}, false, nil
	}
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	j, err := toCore(r)
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	return j, true, nil
}

func (s *sqlStore) List(ctx context.Context) ([]core.DownloadJob, error) {
	rows, err := s.q.ListDownloadJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.DownloadJob, 0, len(rows))
	for _, r := range rows {
		j, err := toCore(r)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// Update persists all mutable fields of j by calling the appropriate sqlc queries.
// NOTE: these statements are issued non-transactionally. A mid-Update crash can
// leave the row internally inconsistent (e.g. status updated but progress not).
// This is acceptable for M3 — cross-restart job recovery is deferred. Revisit
// with a transaction when recovery lands.
// Status drives started_at/finished_at via the SQL CASE expression in
// UpdateDownloadJobStatus; progress/error/output_path/library_track_id each have
// a dedicated update so callers can set them independently.
func (s *sqlStore) Update(ctx context.Context, j core.DownloadJob) error {
	if err := s.q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: string(j.Status), ID: j.ID,
	}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobProgress(ctx, db.UpdateDownloadJobProgressParams{
		Progress: int64(j.Progress), ID: j.ID,
	}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobError(ctx, db.UpdateDownloadJobErrorParams{
		Error: j.Error, ID: j.ID,
	}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobOutputPath(ctx, db.UpdateDownloadJobOutputPathParams{
		OutputPath: j.OutputPath, ID: j.ID,
	}); err != nil {
		return err
	}
	return s.q.UpdateDownloadJobLibraryTrackID(ctx, db.UpdateDownloadJobLibraryTrackIDParams{
		LibraryTrackID: nullString(j.LibraryTrackID), ID: j.ID,
	})
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
