-- name: CreateRequest :exec
INSERT INTO requests (id, requested_by, source, external_id, title, artist, album, isrc, duration_ms, cover_art_id, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRequest :one
SELECT * FROM requests WHERE id = ?;

-- name: GetOpenRequestByItem :one
SELECT * FROM requests WHERE requested_by = ? AND source = ? AND external_id = ? AND status IN ('pending','approved') LIMIT 1;

-- name: GetRequestByDownloadJob :one
SELECT * FROM requests WHERE download_job_id = ?;

-- name: ListRequestsForOwner :many
SELECT * FROM requests WHERE requested_by = ? ORDER BY created_at DESC;

-- name: ListRequests :many
SELECT * FROM requests ORDER BY created_at DESC;

-- name: ListRequestsByStatus :many
SELECT * FROM requests WHERE status = ? ORDER BY created_at DESC;

-- name: UpdateRequestStatus :exec
UPDATE requests SET status = ?, decided_by = ?, decided_at = ?, download_job_id = ?, deny_reason = ? WHERE id = ?;

-- name: DeleteRequest :exec
DELETE FROM requests WHERE id = ?;
