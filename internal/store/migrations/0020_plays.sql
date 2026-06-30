-- +goose Up
CREATE TABLE plays (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL,
  catalog_id  TEXT NOT NULL REFERENCES catalog_entity(id),
  played_at   INTEGER NOT NULL,
  ms_played   INTEGER NOT NULL,
  completed   INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_plays_user_time    ON plays(user_id, played_at);
CREATE INDEX idx_plays_user_catalog ON plays(user_id, catalog_id);

-- +goose Down
DROP TABLE plays;
