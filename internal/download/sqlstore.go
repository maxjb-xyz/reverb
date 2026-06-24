package download

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// sqlStore adapts *db.Queries to JobStore, mapping core.DownloadJob ⇄ db rows.
type sqlStore struct{ q *db.Queries }

// Compile-time assertion: sqlStore must satisfy JobStore.
var _ JobStore = (*sqlStore)(nil)

// NewSQLStore wraps generated queries as a Manager JobStore.
func NewSQLStore(q *db.Queries) JobStore { return &sqlStore{q: q} }

// rowFields is a flattened view of the fields sqlc SELECT queries return. All
// three query row types (GetDownloadJobRow, GetActiveDownloadJobByDedupRow,
// ListDownloadJobsRow) have the same shape; toCoreFlatRow converts from this.
type rowFields struct {
	id             string
	dedupKey       string
	requestJson    string
	downloaderName string
	status         string
	progress       int64
	errStr         string
	outputPath     string
	libraryTrackID sql.NullString
	coverArtID     sql.NullString
	priority       int64
	attempts       int64
	createdAt      int64
	startedAt      sql.NullInt64
	finishedAt     sql.NullInt64
}

// toCoreFlatRow converts a rowFields to a core.DownloadJob, rehydrating all
// request fields from request_json so a job loaded from SQLite can run.
// Returns an error if request_json is non-empty but cannot be decoded — a
// corrupted row must not silently yield a job with empty request fields.
func toCoreFlatRow(r rowFields) (core.DownloadJob, error) {
	j := core.DownloadJob{
		ID:             r.id,
		DedupKey:       r.dedupKey,
		Status:         core.DownloadStatus(r.status),
		Progress:       int(r.progress),
		Error:          r.errStr,
		OutputPath:     r.outputPath,
		DownloaderName: r.downloaderName,
		Priority:       int(r.priority),
		Attempts:       int(r.attempts),
		CreatedAt:      r.createdAt,
	}
	if r.libraryTrackID.Valid {
		j.LibraryTrackID = r.libraryTrackID.String
	}
	if r.coverArtID.Valid {
		j.CoverArtID = r.coverArtID.String
	}
	if r.startedAt.Valid {
		j.StartedAt = r.startedAt.Int64
	}
	if r.finishedAt.Valid {
		j.FinishedAt = r.finishedAt.Int64
	}
	// The FULL request is carried in request_json; rehydrate every field so a job
	// loaded from SQLite has enough to run (artist/title/album/source/externalId/
	// isrc/playWhenReady — the explicit downloader is reflected by DownloaderName).
	var req core.DownloadRequest
	if r.requestJson != "" {
		if err := jsonUnmarshal(r.requestJson, &req); err != nil {
			return core.DownloadJob{}, fmt.Errorf("download job %s: decode request_json: %w", r.id, err)
		}
	}
	j.Source = req.Source
	j.ExternalID = req.ExternalID
	j.Artist = req.Artist
	j.Title = req.Title
	j.Album = req.Album
	j.ISRC = req.ISRC
	j.DurationMs = req.DurationMs
	j.PlayWhenReady = req.PlayWhenReady
	j.AddToPlaylistID = req.AddToPlaylistID
	return j, nil
}

func fromGetRow(r db.GetDownloadJobRow) rowFields {
	return rowFields{
		id: r.ID, dedupKey: r.DedupKey, requestJson: r.RequestJson,
		downloaderName: r.DownloaderName, status: r.Status, progress: r.Progress,
		errStr: r.Error, outputPath: r.OutputPath,
		libraryTrackID: r.LibraryTrackID, coverArtID: r.CoverArtID,
		priority: r.Priority, attempts: r.Attempts, createdAt: r.CreatedAt,
		startedAt: r.StartedAt, finishedAt: r.FinishedAt,
	}
}

func fromGetDedupRow(r db.GetActiveDownloadJobByDedupRow) rowFields {
	return rowFields{
		id: r.ID, dedupKey: r.DedupKey, requestJson: r.RequestJson,
		downloaderName: r.DownloaderName, status: r.Status, progress: r.Progress,
		errStr: r.Error, outputPath: r.OutputPath,
		libraryTrackID: r.LibraryTrackID, coverArtID: r.CoverArtID,
		priority: r.Priority, attempts: r.Attempts, createdAt: r.CreatedAt,
		startedAt: r.StartedAt, finishedAt: r.FinishedAt,
	}
}

func fromListRow(r db.ListDownloadJobsRow) rowFields {
	return rowFields{
		id: r.ID, dedupKey: r.DedupKey, requestJson: r.RequestJson,
		downloaderName: r.DownloaderName, status: r.Status, progress: r.Progress,
		errStr: r.Error, outputPath: r.OutputPath,
		libraryTrackID: r.LibraryTrackID, coverArtID: r.CoverArtID,
		priority: r.Priority, attempts: r.Attempts, createdAt: r.CreatedAt,
		startedAt: r.StartedAt, finishedAt: r.FinishedAt,
	}
}

// Insert persists the job lifecycle row AND marshals the COMPLETE originating
// core.DownloadRequest into request_json, so toCoreFlatRow can rehydrate a runnable job.
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
	j, err := toCoreFlatRow(fromGetRow(r))
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
	j, err := toCoreFlatRow(fromGetDedupRow(r))
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
		j, err := toCoreFlatRow(fromListRow(r))
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
// UpdateDownloadJobStatus; progress/error/output_path/library_track_id/cover_art_id
// each have a dedicated update so callers can set them independently.
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
	if err := s.q.UpdateDownloadJobLibraryTrackID(ctx, db.UpdateDownloadJobLibraryTrackIDParams{
		LibraryTrackID: nullString(j.LibraryTrackID), ID: j.ID,
	}); err != nil {
		return err
	}
	return s.q.UpdateDownloadJobCoverArtID(ctx, db.UpdateDownloadJobCoverArtIDParams{
		CoverArtID: nullString(j.CoverArtID), ID: j.ID,
	})
}

// UpdateRequest re-persists the originating DownloadRequest for the given job
// into request_json. Called by Retry when a ManualURL is provided so the field
// survives a server restart between the Retry call and the worker picking up the job.
func (s *sqlStore) UpdateRequest(ctx context.Context, id string, req core.DownloadRequest) error {
	return s.q.UpdateDownloadJobRequestJson(ctx, db.UpdateDownloadJobRequestJsonParams{
		RequestJson: requestJSON(req),
		ID:          id,
	})
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	return s.q.DeleteDownloadJob(ctx, id)
}

func (s *sqlStore) DeleteFinished(ctx context.Context) ([]string, error) {
	return s.q.DeleteFinishedDownloadJobs(ctx)
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
