-- +goose Up
ALTER TABLE download_jobs ADD COLUMN cover_art_id TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN on older versions; leave the column in place.
-- The column is nullable with no index, so reverting the application is safe.
