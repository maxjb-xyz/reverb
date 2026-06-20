-- +goose Up
CREATE TABLE match_cache (
    source           TEXT NOT NULL,
    external_id      TEXT NOT NULL,
    library_track_id TEXT,                 -- NULL = negative match (not in library)
    method           TEXT NOT NULL DEFAULT 'none',
    confidence       REAL NOT NULL DEFAULT 0,
    isrc             TEXT NOT NULL DEFAULT '',
    mbid             TEXT NOT NULL DEFAULT '',
    duration_ms      INTEGER NOT NULL DEFAULT 0,
    library_version  INTEGER NOT NULL DEFAULT 1,
    matched_at       INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (source, external_id)
);

-- Seed library_version = 1 so a fresh DB has a deterministic baseline.
INSERT INTO settings (key, value) VALUES ('library_version', '1')
    ON CONFLICT(key) DO NOTHING;

-- +goose Down
DROP TABLE match_cache;
DELETE FROM settings WHERE key = 'library_version';
