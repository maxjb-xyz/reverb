-- +goose Up
-- listen_events records the OUTCOME of every track a user started: finished,
-- skipped early, skipped later, or stopped. Unlike `plays` (scrobble-qualified
-- plays only), skips land here too — they are the strongest negative signal.
-- context_kind/context_id say where the track was played from (album,
-- playlist, radio, dj, mix, ...) so a skip can count against a station or lane.
CREATE TABLE listen_events (
  id               TEXT PRIMARY KEY,
  user_id          TEXT NOT NULL,
  catalog_id       TEXT NOT NULL REFERENCES catalog_entity(id),
  library_track_id TEXT NOT NULL DEFAULT '',
  artist_key       TEXT NOT NULL DEFAULT '',
  started_at       INTEGER NOT NULL,
  ms_played        INTEGER NOT NULL,
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  outcome          TEXT NOT NULL, -- completed | skip_early | skip_late | stopped
  context_kind     TEXT NOT NULL DEFAULT '',
  context_id       TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL
);
CREATE INDEX idx_listen_events_user_time    ON listen_events(user_id, started_at);
CREATE INDEX idx_listen_events_user_catalog ON listen_events(user_id, catalog_id);
CREATE INDEX idx_listen_events_user_artist  ON listen_events(user_id, artist_key);

-- taste_signals are discrete "I want this" actions taken outside playback:
-- adding a track to a playlist, downloading it, requesting it. A row targets a
-- track (catalog_id set) or a whole artist (catalog_id empty, artist_key set).
CREATE TABLE taste_signals (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL,
  kind        TEXT NOT NULL, -- playlist_add | download | request
  catalog_id  TEXT NOT NULL DEFAULT '',
  artist_key  TEXT NOT NULL DEFAULT '',
  artist_name TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_taste_signals_user ON taste_signals(user_id, created_at);

-- taste_feedback is explicit thumbs-down: "not this song" (target_kind track,
-- target_key = catalog id) or "less of this artist" (target_kind artist,
-- target_key = artist key). label/library_id are display-only, for the
-- Settings list that lets a user undo them.
CREATE TABLE taste_feedback (
  user_id     TEXT NOT NULL,
  target_kind TEXT NOT NULL, -- track | artist
  target_key  TEXT NOT NULL,
  label       TEXT NOT NULL DEFAULT '',
  sublabel    TEXT NOT NULL DEFAULT '',
  library_id  TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, target_kind, target_key)
);

-- +goose Down
DROP TABLE taste_feedback;
DROP TABLE taste_signals;
DROP TABLE listen_events;
