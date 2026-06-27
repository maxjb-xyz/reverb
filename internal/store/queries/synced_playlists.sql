-- name: UpsertSyncedPlaylist :one
INSERT INTO synced_playlists (id, source, external_id, name, cover_url, tracks_json, mode, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, external_id) DO UPDATE SET
  name = excluded.name, cover_url = excluded.cover_url, tracks_json = excluded.tracks_json
RETURNING *;

-- name: GetSyncedPlaylist :one
SELECT * FROM synced_playlists WHERE id = ?;

-- name: GetSyncedPlaylistBySource :one
SELECT * FROM synced_playlists WHERE source = ? AND external_id = ?;

-- name: ListSyncedPlaylists :many
SELECT * FROM synced_playlists ORDER BY created_at DESC;

-- name: ListSyncedPlaylistsCount :many
-- Returns all playlists for the list view with track_count derived via
-- json_array_length so the Go service never unmarshals the full tracks_json blob.
SELECT id, source, external_id, name, cover_url, mode,
       sync_enabled, sync_interval_sec, auto_download,
       last_synced_at, created_at,
       CAST(json_array_length(tracks_json) AS INTEGER) AS track_count
FROM synced_playlists ORDER BY created_at DESC;

-- name: ListSyncedPlaylistsCountForOwner :many
-- Owner-scoped variant of ListSyncedPlaylistsCount for the API list view:
-- returns only playlists owned by the given user. Used by the API list handler
-- for non-admin callers so the list is scoped to the caller's own playlists.
SELECT id, source, external_id, name, cover_url, mode,
       sync_enabled, sync_interval_sec, auto_download,
       last_synced_at, created_at,
       CAST(json_array_length(tracks_json) AS INTEGER) AS track_count
FROM synced_playlists WHERE owner_user_id = ? ORDER BY created_at DESC;

-- name: GetSyncedPlaylistOwner :one
-- Returns the owner_user_id for a playlist id (NULL when unowned/legacy). Used by
-- the API mutation/detail handlers to enforce ownership (admins bypass).
SELECT owner_user_id FROM synced_playlists WHERE id = ?;

-- name: SetSyncedPlaylistOwner :exec
-- Stamps the owner on a freshly created playlist. Called by the API create/import
-- handlers immediately after the playlist row is created.
UPDATE synced_playlists SET owner_user_id = ? WHERE id = ?;

-- name: BackfillSyncedPlaylistOwners :exec
-- Assigns the given owner to every playlist that has no owner yet. Used by the
-- legacy single-admin migration in EnsureSeed (idempotent: only touches NULLs).
UPDATE synced_playlists SET owner_user_id = ? WHERE owner_user_id IS NULL;

-- name: ListDueSyncedPlaylists :many
SELECT * FROM synced_playlists
WHERE sync_enabled = 1 AND sync_interval_sec > 0 AND (last_synced_at + sync_interval_sec) <= ?;

-- name: UpdateSyncedPlaylistTracks :exec
UPDATE synced_playlists SET name = ?, cover_url = ?, tracks_json = ?, last_synced_at = ? WHERE id = ?;

-- name: UpdateSyncedPlaylistSettings :exec
UPDATE synced_playlists SET sync_enabled = ?, sync_interval_sec = ?, auto_download = ? WHERE id = ?;

-- name: DeleteSyncedPlaylist :exec
DELETE FROM synced_playlists WHERE id = ?;
