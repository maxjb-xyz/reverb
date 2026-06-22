-- name: InsertDownloadJob :exec
INSERT INTO download_jobs (
    id, dedup_key, request_json, downloader_name, status, progress, error,
    output_path, library_track_id, priority, requested_by, attempts,
    created_at, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), NULL, NULL);

-- name: GetDownloadJob :one
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, cover_art_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs WHERE id = ?;

-- name: GetActiveDownloadJobByDedup :one
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, cover_art_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
WHERE dedup_key = ? AND status IN ('queued', 'running')
ORDER BY created_at ASC
LIMIT 1;

-- name: ListDownloadJobs :many
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, cover_art_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
ORDER BY created_at DESC;

-- name: ListDownloadJobsByStatus :many
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, cover_art_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
WHERE status = ?
ORDER BY created_at DESC;

-- name: UpdateDownloadJobStatus :exec
UPDATE download_jobs
SET status     = @status,
    started_at  = CASE WHEN @status = 'running' AND started_at IS NULL THEN unixepoch() ELSE started_at END,
    finished_at = CASE WHEN @status = 'completed' OR @status = 'failed' OR @status = 'canceled' THEN unixepoch() ELSE finished_at END
WHERE id = @id;

-- name: UpdateDownloadJobProgress :exec
UPDATE download_jobs SET progress = ? WHERE id = ?;

-- name: UpdateDownloadJobError :exec
UPDATE download_jobs SET error = ? WHERE id = ?;

-- name: UpdateDownloadJobOutputPath :exec
UPDATE download_jobs SET output_path = ? WHERE id = ?;

-- name: UpdateDownloadJobLibraryTrackID :exec
UPDATE download_jobs SET library_track_id = ? WHERE id = ?;

-- name: UpdateDownloadJobCoverArtID :exec
UPDATE download_jobs SET cover_art_id = ? WHERE id = ?;

-- name: IncrementDownloadJobAttempts :exec
UPDATE download_jobs SET attempts = attempts + 1 WHERE id = ?;
