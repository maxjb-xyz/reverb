-- +goose Up
CREATE TABLE lyrics (
  track_key  TEXT PRIMARY KEY,
  synced     INTEGER NOT NULL DEFAULT 0,
  body       TEXT NOT NULL,
  source     TEXT NOT NULL, -- 'sidecar' | 'tags' | 'lrclib' | 'none'
  fetched_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE lyrics;
