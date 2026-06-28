-- +goose Up
ALTER TABLE requests ADD COLUMN kind TEXT NOT NULL DEFAULT 'track';
-- +goose Down
ALTER TABLE requests DROP COLUMN kind;
