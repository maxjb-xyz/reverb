-- +goose Up
-- track_features holds what the optional audio analyzer heard in each library
-- song: an embedding (songs that sound alike sit close together) and a few
-- plain features. Rows for a different model are redone; failed rows keep
-- their error and are retried after a while.
CREATE TABLE track_features (
  library_track_id TEXT PRIMARY KEY,
  model            TEXT NOT NULL DEFAULT '',
  embedding        BLOB,                      -- little-endian float32, unit length
  tempo            REAL NOT NULL DEFAULT 0,
  energy           REAL NOT NULL DEFAULT -1,
  error            TEXT NOT NULL DEFAULT '',
  analyzed_at      INTEGER NOT NULL
);
CREATE INDEX idx_track_features_model ON track_features(model, error);

-- +goose Down
DROP TABLE track_features;
