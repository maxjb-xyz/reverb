-- +goose Up
ALTER TABLE download_jobs ADD COLUMN downloader_ref TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN on older versions; leave the column in place.
-- The column has a NOT NULL default and no index, so reverting the application is safe.
