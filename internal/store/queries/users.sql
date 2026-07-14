-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :exec
INSERT INTO users (id, username, password_hash, role_id, is_owner) VALUES (?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ? COLLATE NOCASE;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at;

-- name: UpdateUserRole :exec
UPDATE users SET role_id = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetUserDisabled :exec
UPDATE users SET disabled = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetUsername :exec
UPDATE users SET username = ?, updated_at = unixepoch() WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- name: TouchUserLastSeen :exec
UPDATE users SET last_seen = unixepoch() WHERE id = ?;
