-- name: GetMatchCache :one
SELECT source, external_id, library_track_id, method, confidence, isrc, mbid, duration_ms, library_version, matched_at
FROM match_cache
WHERE source = ? AND external_id = ?;

-- name: UpsertMatchCache :exec
INSERT INTO match_cache (
    source, external_id, library_track_id, method, confidence, isrc, mbid, duration_ms, library_version, matched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
ON CONFLICT(source, external_id) DO UPDATE SET
    library_track_id = excluded.library_track_id,
    method           = excluded.method,
    confidence       = excluded.confidence,
    isrc             = excluded.isrc,
    mbid             = excluded.mbid,
    duration_ms      = excluded.duration_ms,
    library_version  = excluded.library_version,
    matched_at       = excluded.matched_at;

-- name: DeleteMatchCacheBySource :exec
DELETE FROM match_cache WHERE source = ?;

-- name: ClearMatchCache :exec
DELETE FROM match_cache;
