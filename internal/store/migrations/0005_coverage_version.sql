-- +goose Up
ALTER TABLE album_coverage ADD COLUMN library_version INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE album_coverage DROP COLUMN library_version;
