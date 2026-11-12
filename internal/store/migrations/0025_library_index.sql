-- +goose Up
-- library_tracks is the recommendation engine's local index of the library:
-- every track with the facets candidates are drawn by (artist, genre, year,
-- when it was added). Rebuilt from the library adapter; rows not seen in the
-- latest full walk (epoch) are dropped. Library data still flows through the
-- adapter for browsing and playback — this is a derived index, not a copy of
-- record.
CREATE TABLE library_tracks (
  library_track_id TEXT PRIMARY KEY,
  title            TEXT NOT NULL,
  artist           TEXT NOT NULL DEFAULT '',
  artist_key       TEXT NOT NULL DEFAULT '',
  artist_id        TEXT NOT NULL DEFAULT '',
  album            TEXT NOT NULL DEFAULT '',
  album_id         TEXT NOT NULL DEFAULT '',
  cover_art_id     TEXT NOT NULL DEFAULT '',
  track_number     INTEGER NOT NULL DEFAULT 0,
  disc_number      INTEGER NOT NULL DEFAULT 0,
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  bit_rate         INTEGER NOT NULL DEFAULT 0,
  suffix           TEXT NOT NULL DEFAULT '',
  content_type     TEXT NOT NULL DEFAULT '',
  isrc             TEXT NOT NULL DEFAULT '',
  year             INTEGER NOT NULL DEFAULT 0,
  genres           TEXT NOT NULL DEFAULT '', -- display form, "; "-joined
  added_at         INTEGER NOT NULL DEFAULT 0,
  epoch            INTEGER NOT NULL
);
CREATE INDEX idx_library_tracks_artist ON library_tracks(artist_key);
CREATE INDEX idx_library_tracks_year   ON library_tracks(year);
CREATE INDEX idx_library_tracks_added  ON library_tracks(added_at);
CREATE INDEX idx_library_tracks_epoch  ON library_tracks(epoch);

CREATE TABLE library_track_genres (
  library_track_id TEXT NOT NULL,
  genre_key        TEXT NOT NULL,
  PRIMARY KEY (library_track_id, genre_key)
);
CREATE INDEX idx_library_track_genres_genre ON library_track_genres(genre_key);

-- artist_external caches which artist on an external source (Deezer, Last.fm)
-- a library artist is. external_id '' records "looked up, no confident match"
-- so the refresher doesn't retry every run.
CREATE TABLE artist_external (
  artist_key  TEXT NOT NULL,
  source      TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  name        TEXT NOT NULL DEFAULT '',
  fetched_at  INTEGER NOT NULL,
  PRIMARY KEY (artist_key, source)
);

-- artist_similar is the artist-similarity map: "fans of artist_key also play
-- similar_key", per source, ranked (0 = most similar). similar_key need not be
-- in the library — those are the artists Discover can suggest.
CREATE TABLE artist_similar (
  artist_key          TEXT NOT NULL,
  source              TEXT NOT NULL,
  similar_key         TEXT NOT NULL,
  similar_name        TEXT NOT NULL,
  similar_external_id TEXT NOT NULL DEFAULT '',
  similar_image       TEXT NOT NULL DEFAULT '',
  rank                INTEGER NOT NULL,
  fetched_at          INTEGER NOT NULL,
  PRIMARY KEY (artist_key, source, similar_key)
);
CREATE INDEX idx_artist_similar_similar ON artist_similar(similar_key);

-- +goose Down
DROP TABLE artist_similar;
DROP TABLE artist_external;
DROP TABLE library_track_genres;
DROP TABLE library_tracks;
