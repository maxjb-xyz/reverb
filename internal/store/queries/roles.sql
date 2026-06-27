-- name: ListRoles :many
SELECT * FROM roles ORDER BY is_system DESC, name;

-- name: GetRole :one
SELECT * FROM roles WHERE id = ?;

-- name: CreateRole :exec
INSERT INTO roles (id, name, is_system, capabilities) VALUES (?, ?, ?, ?);

-- name: UpdateRole :exec
UPDATE roles SET name = ?, capabilities = ?, updated_at = unixepoch() WHERE id = ?;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = ?;

-- name: CountUsersWithRole :one
SELECT COUNT(*) FROM users WHERE role_id = ?;
