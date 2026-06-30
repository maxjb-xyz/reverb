-- +goose Up
CREATE TABLE scrobble_link (
  user_id     TEXT NOT NULL,
  provider    TEXT NOT NULL,
  session_key TEXT NOT NULL,
  username    TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'active', -- active | broken
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, provider)
);
CREATE TABLE scrobble_queue (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL,
  provider        TEXT NOT NULL,
  catalog_id      TEXT NOT NULL DEFAULT '',
  title           TEXT NOT NULL,
  artist          TEXT NOT NULL,
  album           TEXT NOT NULL DEFAULT '',
  duration_ms     INTEGER NOT NULL DEFAULT 0,
  played_at       INTEGER NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending', -- pending | done | failed
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL
);
CREATE INDEX idx_scrobble_queue_due ON scrobble_queue (status, next_attempt_at);

-- +goose Down
DROP TABLE scrobble_queue;
DROP TABLE scrobble_link;
