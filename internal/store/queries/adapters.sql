-- name: CreateAdapterInstance :exec
INSERT INTO adapter_instances (id, type, name, enabled, priority, config_json)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAdapterInstances :many
SELECT * FROM adapter_instances ORDER BY type, priority;

-- name: DeleteAdapterInstance :exec
DELETE FROM adapter_instances WHERE id = ?;
