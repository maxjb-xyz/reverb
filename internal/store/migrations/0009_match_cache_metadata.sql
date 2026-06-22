-- +goose Up
-- Persist the matched library candidate's artist/album/cover ids alongside the
-- match decision so a cache HIT can reconstruct clickable artist + album links and
-- a real cover for the synthesized owned LibraryTrack (instead of losing them).
-- 0008 already flushed match_cache, so existing rows carry no stale metadata.
ALTER TABLE match_cache ADD COLUMN artist_id TEXT NOT NULL DEFAULT '';
ALTER TABLE match_cache ADD COLUMN album_id TEXT NOT NULL DEFAULT '';
ALTER TABLE match_cache ADD COLUMN cover_art_id TEXT NOT NULL DEFAULT '';

-- +goose Down
-- No-op: SQLite ADD COLUMN is not cleanly reversible (DROP COLUMN support varies);
-- the columns are additive with safe defaults and recompute on demand.
