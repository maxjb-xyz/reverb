-- name: CreateInvite :exec
INSERT INTO invites (id, code, role_id, created_by, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: GetInviteByCode :one
SELECT * FROM invites WHERE code = ?;

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC;

-- name: MarkInviteUsed :exec
UPDATE invites SET used_by = ?, used_at = unixepoch() WHERE id = ?;

-- name: DeleteInvite :exec
DELETE FROM invites WHERE id = ?;
