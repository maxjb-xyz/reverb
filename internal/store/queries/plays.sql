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

-- name: StatsSummary :one
SELECT
    COUNT(*)                    AS plays,
    COUNT(DISTINCT p.catalog_id) AS distinct_tracks,
    COUNT(DISTINCT e.artist)    AS distinct_artists,
    COUNT(DISTINCT e.album)     AS distinct_albums,
    COALESCE(SUM(p.ms_played), 0) AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?;

-- name: StatsTopTracks :many
SELECT
    p.catalog_id,
    e.title,
    e.artist,
    e.album,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY p.catalog_id
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;

-- name: StatsTopArtists :many
SELECT
    e.artist,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY e.artist
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;

-- name: StatsTopAlbums :many
SELECT
    e.album,
    e.artist,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY e.album, e.artist
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;

-- name: StatsPlaysInWindow :many
SELECT p.played_at, p.ms_played
FROM plays p
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
ORDER BY p.played_at ASC;

-- name: StatsEntityArtist :one
SELECT
    COUNT(*)         AS plays,
    COALESCE(SUM(p.ms_played), 0) AS ms_played,
    MIN(p.played_at) AS first_played,
    MAX(p.played_at) AS last_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND e.artist = ? AND p.played_at >= ? AND p.played_at < ?;

-- name: StatsEntityTrack :one
SELECT
    COUNT(*)         AS plays,
    COALESCE(SUM(p.ms_played), 0) AS ms_played,
    MIN(p.played_at) AS first_played,
    MAX(p.played_at) AS last_played
FROM plays p
WHERE p.user_id = ? AND p.catalog_id = ? AND p.played_at >= ? AND p.played_at < ?;

-- name: StatsTopTracksByArtist :many
SELECT
    p.catalog_id,
    e.title,
    e.artist,
    e.album,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND e.artist = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY p.catalog_id
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;

-- name: StatsTopTracksByCatalogID :many
SELECT
    p.catalog_id,
    e.title,
    e.artist,
    e.album,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.catalog_id = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY p.catalog_id
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;
