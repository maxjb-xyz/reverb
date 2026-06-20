-- +goose Up
CREATE TABLE download_jobs (
    id               TEXT PRIMARY KEY,
    dedup_key        TEXT NOT NULL,
    request_json     TEXT NOT NULL DEFAULT '{}',
    downloader_name  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'queued',
    progress         INTEGER NOT NULL DEFAULT 0,
    error            TEXT NOT NULL DEFAULT '',
    output_path      TEXT NOT NULL DEFAULT '',
    library_track_id TEXT,                       -- NULL until re-matched after scan
    priority         INTEGER NOT NULL DEFAULT 0,
    requested_by     TEXT,                       -- NULL: P3 multi-user stub
    attempts         INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL DEFAULT (unixepoch()),
    started_at       INTEGER,
    finished_at      INTEGER
);

-- Active-job lookup for dedup-join (queued or running rows share a dedup_key).
CREATE INDEX idx_download_jobs_dedup_active ON download_jobs (dedup_key, status);

-- +goose Down
DROP INDEX idx_download_jobs_dedup_active;
DROP TABLE download_jobs;
