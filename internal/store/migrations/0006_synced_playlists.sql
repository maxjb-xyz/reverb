-- +goose Up
CREATE TABLE synced_playlists (
  id                TEXT PRIMARY KEY,
  source            TEXT NOT NULL,
  external_id       TEXT NOT NULL,
  name              TEXT NOT NULL,
  cover_url         TEXT NOT NULL DEFAULT '',
  tracks_json       TEXT NOT NULL,
  sync_enabled      INTEGER NOT NULL DEFAULT 0,
  sync_interval_sec INTEGER NOT NULL DEFAULT 0,
  auto_download     INTEGER NOT NULL DEFAULT 0,
  last_synced_at    INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_synced_playlists_source_external ON synced_playlists(source, external_id);

-- +goose Down
DROP TABLE synced_playlists;
