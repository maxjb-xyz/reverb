-- +goose Up
CREATE TABLE catalog_entity (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  artist      TEXT NOT NULL DEFAULT '',
  album       TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER NOT NULL DEFAULT 0,
  isrc        TEXT NOT NULL DEFAULT '',
  mbid        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT '',
  external_id TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE TABLE catalog_alias (
  alias_kind  TEXT NOT NULL,
  alias_value TEXT NOT NULL,
  catalog_id  TEXT NOT NULL REFERENCES catalog_entity(id),
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (alias_kind, alias_value)
);
CREATE INDEX idx_catalog_alias_catalog ON catalog_alias(catalog_id);
CREATE TABLE backend_binding (
  catalog_id       TEXT NOT NULL REFERENCES catalog_entity(id),
  library_identity TEXT NOT NULL,
  backend_id       TEXT NOT NULL DEFAULT '',
  cover_art_id     TEXT NOT NULL DEFAULT '',
  known_absent     INTEGER NOT NULL DEFAULT 0,
  binding_epoch    INTEGER NOT NULL,
  resolved_at      INTEGER NOT NULL,
  PRIMARY KEY (catalog_id, library_identity)
);

-- +goose Down
DROP TABLE backend_binding;
DROP TABLE catalog_alias;
DROP TABLE catalog_entity;
