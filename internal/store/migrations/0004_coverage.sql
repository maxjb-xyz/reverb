-- +goose Up
CREATE TABLE artist_external_map (
  library_artist_id  TEXT NOT NULL,
  source             TEXT NOT NULL,
  external_artist_id TEXT NOT NULL,
  confidence         REAL NOT NULL,
  created_at         INTEGER NOT NULL,
  PRIMARY KEY (library_artist_id, source)
);
CREATE TABLE discography_cache (
  source             TEXT NOT NULL,
  external_artist_id TEXT NOT NULL,
  albums_json        TEXT NOT NULL,
  fetched_at         INTEGER NOT NULL,
  PRIMARY KEY (source, external_artist_id)
);
CREATE TABLE album_coverage (
  source            TEXT NOT NULL,
  external_album_id TEXT NOT NULL,
  coverage_json     TEXT NOT NULL,
  library_album_id  TEXT NOT NULL DEFAULT '',
  fetched_at        INTEGER NOT NULL,
  PRIMARY KEY (source, external_album_id)
);

-- +goose Down
DROP TABLE album_coverage;
DROP TABLE discography_cache;
DROP TABLE artist_external_map;
