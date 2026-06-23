-- +goose Up
ALTER TABLE synced_playlists ADD COLUMN mode TEXT NOT NULL DEFAULT 'synced';
-- Values: 'synced' = auto-mirror, 'once' = editable snapshot

-- +goose Down
-- No-op: SQLite does not support DROP COLUMN easily; the DEFAULT makes this safe.
SELECT 1;
