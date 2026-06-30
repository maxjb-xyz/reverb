-- name: InsertPlay :exec
INSERT INTO plays (id, user_id, catalog_id, played_at, ms_played, completed, created_at)
VALUES (?,?,?,?,?,?,?);

-- name: RepointPlays :exec
UPDATE plays SET catalog_id = ? WHERE catalog_id = ?;

-- name: ListRecentPlays :many
SELECT p.id, p.catalog_id, p.played_at, e.title, e.artist, e.album
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at < ?
ORDER BY p.played_at DESC LIMIT ?;
