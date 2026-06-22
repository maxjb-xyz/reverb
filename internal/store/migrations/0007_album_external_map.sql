-- +goose Up
CREATE TABLE album_external_map (
  library_album_id   TEXT NOT NULL,
  source             TEXT NOT NULL,
  external_album_id  TEXT NOT NULL,
  confidence         REAL NOT NULL,
  created_at         INTEGER NOT NULL,
  PRIMARY KEY (library_album_id, source)
);

-- +goose Down
DROP TABLE album_external_map;
