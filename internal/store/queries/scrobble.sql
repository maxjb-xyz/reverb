-- name: UpsertScrobbleLink :exec
INSERT INTO scrobble_link (user_id, provider, session_key, username, status, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (user_id, provider) DO UPDATE SET
  session_key = excluded.session_key,
  username    = excluded.username,
  status      = excluded.status;

-- name: GetScrobbleLink :one
SELECT user_id, provider, session_key, username, status, created_at
FROM scrobble_link
WHERE user_id = ? AND provider = ?;

-- name: ListScrobbleLinks :many
SELECT user_id, provider, session_key, username, status, created_at
FROM scrobble_link
WHERE user_id = ?
ORDER BY provider;

-- name: DeleteScrobbleLink :exec
DELETE FROM scrobble_link
WHERE user_id = ? AND provider = ?;

-- name: SetScrobbleLinkStatus :exec
UPDATE scrobble_link
SET status = ?
WHERE user_id = ? AND provider = ?;

-- name: InsertScrobbleQueue :exec
INSERT INTO scrobble_queue (id, user_id, provider, catalog_id, title, artist, album, duration_ms, played_at, status, attempts, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: SelectDueScrobbles :many
SELECT id, user_id, provider, catalog_id, title, artist, album, duration_ms, played_at, status, attempts, next_attempt_at, created_at
FROM scrobble_queue
WHERE status = 'pending' AND next_attempt_at <= ?
ORDER BY next_attempt_at
LIMIT ?;

-- name: MarkScrobbleDone :exec
UPDATE scrobble_queue
SET status = 'done'
WHERE id = ?;

-- name: MarkScrobbleRetry :exec
UPDATE scrobble_queue
SET attempts = ?, next_attempt_at = ?
WHERE id = ?;

-- name: MarkScrobbleFailed :exec
UPDATE scrobble_queue
SET status = 'failed'
WHERE id = ?;
