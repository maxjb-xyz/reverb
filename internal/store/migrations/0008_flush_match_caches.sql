-- +goose Up
-- Flush the match-dependent derived caches so the composite-artist + duration-drift
-- matcher fix takes effect on deploy. These caches hold STALE NEGATIVES keyed by the
-- current library_version, so the new fuzzy logic would not surface until they are
-- invalidated. The stable resolution/Spotify caches (artist_external_map,
-- album_external_map, discography_cache) are intentionally left untouched.
DELETE FROM match_cache;
DELETE FROM album_coverage;

-- +goose Down
-- No-op: a cache flush is not reversible (the derived rows simply recompute on demand).
