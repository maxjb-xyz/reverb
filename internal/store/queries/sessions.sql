-- name: CreateSession :exec
INSERT INTO sessions (id, token_hash, user_id, expires_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ?;

-- name: DeleteSessionsForUserExcept :exec
DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?;

-- name: BackfillSessionUser :exec
UPDATE sessions SET user_id = ? WHERE user_id IS NULL;
