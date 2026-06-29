-- +goose Up
ALTER TABLE requests ADD COLUMN track_count INTEGER NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE requests DROP COLUMN track_count;
