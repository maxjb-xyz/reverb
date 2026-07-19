-- name: GetLyrics :one
SELECT * FROM lyrics WHERE track_key = ?;

-- name: UpsertLyrics :exec
INSERT INTO lyrics (track_key, synced, body, source, fetched_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(track_key) DO UPDATE SET
  synced = excluded.synced,
  body = excluded.body,
  source = excluded.source,
  fetched_at = excluded.fetched_at;
