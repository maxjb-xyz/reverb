-- +goose Up
ALTER TABLE download_jobs ADD COLUMN canonical_id TEXT NOT NULL DEFAULT '';

-- +goose Down
-- No-op: SQLite ADD COLUMN is not cleanly reversible (DROP COLUMN support varies);
-- the column is additive with a safe empty-string default and recomputes on demand.
