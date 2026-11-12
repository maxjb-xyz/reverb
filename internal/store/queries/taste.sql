-- name: InsertListenEvent :exec
INSERT INTO listen_events (id, user_id, catalog_id, library_track_id, artist_key, started_at, ms_played, duration_ms, outcome, context_kind, context_id, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?);

-- name: ListListenEventsSince :many
SELECT l.catalog_id, l.library_track_id, l.artist_key, l.started_at, l.ms_played, l.duration_ms,
       l.outcome, l.context_kind, l.context_id, e.title, e.artist
FROM listen_events l JOIN catalog_entity e ON e.id = l.catalog_id
WHERE l.user_id = ? AND l.started_at >= ?
ORDER BY l.started_at;

-- name: EarliestListenEvent :one
SELECT CAST(COALESCE(MIN(started_at), 0) AS INTEGER) AS earliest
FROM listen_events WHERE user_id = ?;

-- name: ListPlaysForTaste :many
-- Qualified plays recorded before the listen log existed, so history that
-- predates it still counts toward taste.
SELECT p.catalog_id, e.title, e.artist, p.played_at, p.completed
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?;

-- name: InsertTasteSignal :exec
INSERT INTO taste_signals (id, user_id, kind, catalog_id, artist_key, artist_name, created_at)
VALUES (?,?,?,?,?,?,?);

-- name: ListTasteSignalsSince :many
SELECT s.kind, s.catalog_id, s.artist_key, s.created_at,
       COALESCE(e.title, '') AS title, COALESCE(e.artist, s.artist_name) AS artist
FROM taste_signals s LEFT JOIN catalog_entity e ON e.id = s.catalog_id
WHERE s.user_id = ? AND s.created_at >= ?
ORDER BY s.created_at;

-- name: UpsertTasteFeedback :exec
INSERT INTO taste_feedback (user_id, target_kind, target_key, label, sublabel, library_id, created_at)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT (user_id, target_kind, target_key) DO UPDATE SET
  label = excluded.label, sublabel = excluded.sublabel, library_id = excluded.library_id;

-- name: DeleteTasteFeedback :exec
DELETE FROM taste_feedback WHERE user_id = ? AND target_kind = ? AND target_key = ?;

-- name: ListTasteFeedback :many
SELECT target_kind, target_key, label, sublabel, library_id, created_at
FROM taste_feedback WHERE user_id = ?
ORDER BY created_at DESC;

-- name: RepointListenEvents :exec
UPDATE listen_events SET catalog_id = ? WHERE catalog_id = ?;

-- name: RepointTasteSignals :exec
UPDATE taste_signals SET catalog_id = ? WHERE catalog_id = ?;

-- name: RepointTrackFeedback :exec
-- OR IGNORE: if the user already thumbed-down the winner, the loser row stays
-- behind and DeleteTrackFeedbackFor removes it.
UPDATE OR IGNORE taste_feedback SET target_key = ?
WHERE target_kind = 'track' AND target_key = ?;

-- name: DeleteTrackFeedbackFor :exec
DELETE FROM taste_feedback WHERE target_kind = 'track' AND target_key = ?;
