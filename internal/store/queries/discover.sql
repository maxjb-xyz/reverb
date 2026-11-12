-- name: GetDiscoverPrefs :one
SELECT * FROM discover_prefs WHERE user_id = ?;

-- name: UpsertDiscoverPrefs :exec
INSERT INTO discover_prefs (user_id, enabled, size, new_share, keep_rule, updated_at) VALUES (?,?,?,?,?,?)
ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled, size = excluded.size,
  new_share = excluded.new_share, keep_rule = excluded.keep_rule, updated_at = excluded.updated_at;

-- name: ListDiscoverUsers :many
-- Active users with the capabilities of their role: who gets a Discover
-- Weekly and who may have songs downloaded for it.
SELECT u.id, u.is_owner, CAST(COALESCE(r.capabilities, '[]') AS TEXT) AS capabilities
FROM users u LEFT JOIN roles r ON r.id = u.role_id
WHERE u.disabled = 0;

-- name: ListDiscoverItems :many
SELECT * FROM discover_items WHERE user_id = ? AND week = ? ORDER BY position;

-- name: InsertDiscoverItem :exec
INSERT INTO discover_items (id, user_id, week, position, owned, library_track_id, source, external_id, title, artist,
  album, cover_url, isrc, duration_ms, item_json, job_id, preexisting, state, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);

-- name: UpdateDiscoverItemJob :exec
UPDATE discover_items SET job_id = ?, preexisting = ?, state = ? WHERE id = ?;

-- name: UpdateDiscoverItemState :exec
UPDATE discover_items SET state = ?, decided_at = ? WHERE id = ?;

-- name: ListUndecidedDiscoverItems :many
-- Earlier weeks' downloaded songs that the keep rule hasn't ruled on yet.
SELECT * FROM discover_items
WHERE user_id = ? AND week < ? AND job_id != '' AND preexisting = 0 AND state IN ('downloading', 'suggested');

-- name: ListDiscoveredExternalIDs :many
-- Every external song a user has ever been offered, so Discover doesn't repeat itself.
SELECT DISTINCT source || ':' || external_id AS ref FROM discover_items WHERE user_id = ? AND external_id != '';

-- name: CountDiscoverDownloadsSince :one
SELECT COUNT(*) FROM discover_items WHERE user_id = ? AND job_id != '' AND preexisting = 0 AND created_at >= ?;

-- name: CountPlaylistsWithTrack :one
-- How many playlists (any user's) hold this song, by library id or by its
-- source id. tracks_json is Go-encoded, so an entry reads "externalId":"<id>".
-- (A LIKE wildcard in an id can only over-match, which errs towards keeping.)
SELECT COUNT(*) FROM synced_playlists
WHERE (sqlc.arg(library_track_id) != '' AND tracks_json LIKE '%"externalId":"' || sqlc.arg(library_track_id) || '"%')
   OR (sqlc.arg(external_id) != '' AND tracks_json LIKE '%"externalId":"' || sqlc.arg(external_id) || '"%');

-- name: CountCompletedListens :one
-- Finished listens of a song, by anyone (user_id '') or one user.
SELECT COUNT(*) FROM listen_events
WHERE outcome = 'completed'
  AND (library_track_id = sqlc.arg(library_track_id) OR (sqlc.arg(catalog_id) != '' AND catalog_id = sqlc.arg(catalog_id)))
  AND (sqlc.arg(user_id) = '' OR user_id = sqlc.arg(user_id));

-- name: CountOtherUsersCompletedListens :one
SELECT COUNT(*) FROM listen_events
WHERE outcome = 'completed' AND user_id != sqlc.arg(user_id)
  AND (library_track_id = sqlc.arg(library_track_id) OR (sqlc.arg(catalog_id) != '' AND catalog_id = sqlc.arg(catalog_id)));

-- name: CountPlaylistAddSignals :one
SELECT COUNT(*) FROM taste_signals WHERE kind = 'playlist_add' AND catalog_id = ? AND catalog_id != '';

-- name: CountOtherDiscoverHolds :one
-- Other users whose Discover still holds (or kept) the same download.
SELECT COUNT(*) FROM discover_items
WHERE job_id = sqlc.arg(job_id) AND job_id != '' AND user_id != sqlc.arg(user_id) AND state IN ('suggested', 'downloading', 'kept');
