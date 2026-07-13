-- name: DistinctDurableCanonicalIDs :many
SELECT DISTINCT catalog_id FROM plays WHERE catalog_id != ''
UNION
SELECT DISTINCT canonical_id FROM download_jobs WHERE canonical_id != ''
LIMIT ?;

-- name: InsertPlay :exec
INSERT INTO plays (id, user_id, catalog_id, played_at, ms_played, completed, created_at)
VALUES (?,?,?,?,?,?,?);

-- name: RepointPlays :exec
UPDATE plays SET catalog_id = ? WHERE catalog_id = ?;

-- name: DeletePlay :exec
DELETE FROM plays WHERE id = ? AND user_id = ?;

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
    e.source,
    e.external_id,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
GROUP BY p.catalog_id
ORDER BY COUNT(*) DESC, SUM(p.ms_played) DESC
LIMIT ?;

-- name: StatsTopArtists :many
WITH aggregated AS (
    SELECT
        MIN(p.catalog_id) AS catalog_id,
        e.artist,
        COUNT(*)         AS plays,
        SUM(p.ms_played) AS ms_played
    FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
    WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
    GROUP BY e.artist
)
SELECT
    CAST(a.catalog_id AS TEXT) AS catalog_id,
    a.artist,
    CAST(e.source AS TEXT) AS source,
    CAST(e.external_id AS TEXT) AS external_id,
    a.plays,
    a.ms_played
FROM aggregated a JOIN catalog_entity e ON e.id = a.catalog_id
ORDER BY a.plays DESC, a.ms_played DESC
LIMIT ?;

-- name: StatsTopAlbums :many
WITH aggregated AS (
    SELECT
        MIN(p.catalog_id) AS catalog_id,
        e.album,
        e.artist,
        COUNT(*)         AS plays,
        SUM(p.ms_played) AS ms_played
    FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
    WHERE p.user_id = ? AND p.played_at >= ? AND p.played_at < ?
    GROUP BY e.album, e.artist
)
SELECT
    CAST(a.catalog_id AS TEXT) AS catalog_id,
    a.album,
    a.artist,
    CAST(e.source AS TEXT) AS source,
    CAST(e.external_id AS TEXT) AS external_id,
    a.plays,
    a.ms_played
FROM aggregated a JOIN catalog_entity e ON e.id = a.catalog_id
ORDER BY a.plays DESC, a.ms_played DESC
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

-- name: StatsEntityAlbum :one
SELECT
    COUNT(*)         AS plays,
    COALESCE(SUM(p.ms_played), 0) AS ms_played,
    MIN(p.played_at) AS first_played,
    MAX(p.played_at) AS last_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND e.album = ? AND e.artist = ? AND p.played_at >= ? AND p.played_at < ?;

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

-- name: StatsTopTracksByAlbum :many
SELECT
    p.catalog_id,
    e.title,
    e.artist,
    e.album,
    COUNT(*)          AS plays,
    SUM(p.ms_played)  AS ms_played
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND e.album = ? AND e.artist = ? AND p.played_at >= ? AND p.played_at < ?
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

-- name: CountPlaysByCatalog :one
SELECT COUNT(*) FROM plays WHERE user_id = ? AND catalog_id = ?;
