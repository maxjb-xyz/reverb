-- +goose Up
CREATE TABLE roles (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    is_system    INTEGER NOT NULL DEFAULT 0,
    capabilities TEXT NOT NULL DEFAULT '[]',
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at   INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role_id       TEXT NOT NULL REFERENCES roles(id),
    is_owner      INTEGER NOT NULL DEFAULT 0,
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    last_seen     INTEGER
);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    role_id    TEXT REFERENCES roles(id),
    created_by TEXT REFERENCES users(id),
    expires_at INTEGER,
    used_by    TEXT REFERENCES users(id),
    used_at    INTEGER,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

ALTER TABLE sessions       ADD COLUMN user_id       TEXT REFERENCES users(id);
ALTER TABLE download_jobs  ADD COLUMN initiated_by  TEXT REFERENCES users(id);
ALTER TABLE synced_playlists ADD COLUMN owner_user_id TEXT REFERENCES users(id);

-- +goose Down
ALTER TABLE synced_playlists DROP COLUMN owner_user_id;
ALTER TABLE download_jobs    DROP COLUMN initiated_by;
ALTER TABLE sessions         DROP COLUMN user_id;
DROP TABLE invites;
DROP TABLE users;
DROP TABLE roles;
