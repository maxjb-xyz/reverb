-- name: GetLibraryVersion :one
SELECT value FROM settings WHERE key = 'library_version';

-- name: SetLibraryVersion :exec
INSERT INTO settings (key, value) VALUES ('library_version', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
