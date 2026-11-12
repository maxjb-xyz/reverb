-- +goose Up
-- mixes are saved recipes for the recommendation engine: what to draw from
-- (seeds, a lane, a period), how big, how often to refresh, and the songs the
-- last refresh produced. Daily mixes are made by Reverb from the user's taste
-- (kind 'daily', rebuilt every day); custom ones are the user's own.
CREATE TABLE mixes (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL,             -- daily | custom
  position     INTEGER NOT NULL DEFAULT 0,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  recipe       TEXT NOT NULL,             -- JSON mixes.Recipe
  items        TEXT NOT NULL DEFAULT '[]',-- JSON []engine.Candidate from the last refresh
  cover_art_id TEXT NOT NULL DEFAULT '',
  generated_at INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX idx_mixes_user ON mixes(user_id, kind, position);

-- +goose Down
DROP TABLE mixes;
