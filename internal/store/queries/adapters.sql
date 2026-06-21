-- name: CreateAdapterInstance :exec
INSERT INTO adapter_instances (id, type, name, enabled, priority, config_json)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAdapterInstances :many
SELECT * FROM adapter_instances ORDER BY type, priority;

-- name: DeleteAdapterInstance :exec
DELETE FROM adapter_instances WHERE id = ?;

-- name: GetAdapterInstance :one
SELECT * FROM adapter_instances WHERE id = ?;

-- name: UpdateAdapterInstance :exec
UPDATE adapter_instances
SET name = @name,
    enabled = @enabled,
    priority = @priority,
    config_json = @config_json,
    updated_at = unixepoch()
WHERE id = @id;

-- name: SetAdapterInstanceEnabled :exec
UPDATE adapter_instances SET enabled = @enabled, updated_at = unixepoch() WHERE id = @id;

-- name: SetAdapterInstancePriority :exec
UPDATE adapter_instances SET priority = @priority, updated_at = unixepoch() WHERE id = @id;
