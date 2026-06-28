-- +goose Up
ALTER TABLE requests ADD COLUMN cover_url TEXT;
-- +goose Down
ALTER TABLE requests DROP COLUMN cover_url;
