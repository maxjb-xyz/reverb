-- name: UpsertSyncedPlaylist :one
INSERT INTO synced_playlists (id, source, external_id, name, cover_url, tracks_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, external_id) DO UPDATE SET
  name = excluded.name, cover_url = excluded.cover_url, tracks_json = excluded.tracks_json
RETURNING *;

-- name: GetSyncedPlaylist :one
SELECT * FROM synced_playlists WHERE id = ?;

-- name: GetSyncedPlaylistBySource :one
SELECT * FROM synced_playlists WHERE source = ? AND external_id = ?;

-- name: ListSyncedPlaylists :many
SELECT * FROM synced_playlists ORDER BY created_at DESC;

-- name: ListDueSyncedPlaylists :many
SELECT * FROM synced_playlists
WHERE sync_enabled = 1 AND sync_interval_sec > 0 AND (last_synced_at + sync_interval_sec) <= ?;

-- name: UpdateSyncedPlaylistTracks :exec
UPDATE synced_playlists SET name = ?, cover_url = ?, tracks_json = ?, last_synced_at = ? WHERE id = ?;

-- name: UpdateSyncedPlaylistSettings :exec
UPDATE synced_playlists SET sync_enabled = ?, sync_interval_sec = ?, auto_download = ? WHERE id = ?;

-- name: DeleteSyncedPlaylist :exec
DELETE FROM synced_playlists WHERE id = ?;
