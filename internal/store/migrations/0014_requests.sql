-- +goose Up
CREATE TABLE requests (
    id              TEXT PRIMARY KEY,
    requested_by    TEXT NOT NULL REFERENCES users(id),
    source          TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    title           TEXT NOT NULL,
    artist          TEXT NOT NULL,
    album           TEXT,
    isrc            TEXT,
    duration_ms     INTEGER,
    cover_art_id    TEXT,
    status          TEXT NOT NULL,
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
    decided_by      TEXT REFERENCES users(id),
    decided_at      INTEGER,
    download_job_id TEXT,
    deny_reason     TEXT
);
CREATE INDEX idx_requests_status ON requests(status);
CREATE INDEX idx_requests_requested_by ON requests(requested_by);
-- +goose Down
DROP TABLE requests;
