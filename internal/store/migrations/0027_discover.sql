-- +goose Up
-- discover_prefs: each user's Discover Weekly settings. keep_rule decides what
-- happens at the end of the week to songs Reverb downloaded for Discover:
-- 'playlist' (default) keeps a song only if it was added to a playlist,
-- 'played' also keeps songs the user played through, 'always' keeps all.
CREATE TABLE discover_prefs (
  user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  enabled    INTEGER NOT NULL DEFAULT 1,
  size       INTEGER NOT NULL DEFAULT 30,
  new_share  INTEGER NOT NULL DEFAULT 70,     -- % of songs not in the library
  keep_rule  TEXT NOT NULL DEFAULT 'playlist',
  updated_at INTEGER NOT NULL
);

-- discover_items: one row per song in a user's Discover Weekly. External
-- songs carry the download job Reverb started for them; preexisting = 1 when
-- that job (or the song) was already there, so the cleanup never deletes it.
CREATE TABLE discover_items (
  id               TEXT PRIMARY KEY,
  user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  week             TEXT NOT NULL,             -- ISO week, e.g. 2027-W02
  position         INTEGER NOT NULL,
  owned            INTEGER NOT NULL DEFAULT 0,  -- already in the library when picked
  library_track_id TEXT NOT NULL DEFAULT '',
  source           TEXT NOT NULL DEFAULT '',
  external_id      TEXT NOT NULL DEFAULT '',
  title            TEXT NOT NULL,
  artist           TEXT NOT NULL,
  album            TEXT NOT NULL DEFAULT '',
  cover_url        TEXT NOT NULL DEFAULT '',
  isrc             TEXT NOT NULL DEFAULT '',
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  item_json        TEXT NOT NULL DEFAULT '{}',  -- the engine candidate (reason, track)
  job_id           TEXT NOT NULL DEFAULT '',
  preexisting      INTEGER NOT NULL DEFAULT 0,
  state            TEXT NOT NULL DEFAULT 'suggested', -- suggested|downloading|kept|removed
  decided_at       INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL
);
CREATE INDEX idx_discover_items_user_week ON discover_items(user_id, week, position);
CREATE INDEX idx_discover_items_external ON discover_items(user_id, source, external_id);

-- +goose Down
DROP TABLE discover_items;
DROP TABLE discover_prefs;
