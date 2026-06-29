-- name: InsertCatalogEntity :exec
INSERT INTO catalog_entity (id, kind, title, artist, album, duration_ms, isrc, mbid, source, external_id, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?);

-- name: GetCatalogEntity :one
SELECT * FROM catalog_entity WHERE id = ?;

-- name: InsertCatalogAlias :exec
INSERT INTO catalog_alias (alias_kind, alias_value, catalog_id, created_at)
VALUES (?,?,?,?) ON CONFLICT(alias_kind, alias_value) DO NOTHING;

-- name: GetAliasCatalogID :one
SELECT catalog_id FROM catalog_alias WHERE alias_kind = ? AND alias_value = ?;

-- name: ListAliasesForCatalog :many
SELECT alias_kind, alias_value FROM catalog_alias WHERE catalog_id = ?;

-- name: RepointAliases :exec
UPDATE catalog_alias SET catalog_id = ? WHERE catalog_id = ?;

-- name: DeleteCatalogEntity :exec
DELETE FROM catalog_entity WHERE id = ?;

-- name: GetBackendBinding :one
SELECT * FROM backend_binding WHERE catalog_id = ? AND library_identity = ?;

-- name: UpsertBackendBinding :exec
INSERT INTO backend_binding (catalog_id, library_identity, backend_id, cover_art_id, known_absent, binding_epoch, resolved_at)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(catalog_id, library_identity) DO UPDATE SET
  backend_id=excluded.backend_id, cover_art_id=excluded.cover_art_id,
  known_absent=excluded.known_absent, binding_epoch=excluded.binding_epoch, resolved_at=excluded.resolved_at;

-- name: DeleteBindingsForCatalog :exec
DELETE FROM backend_binding WHERE catalog_id = ?;

-- name: RepointBindings :exec
UPDATE backend_binding SET catalog_id = ? WHERE catalog_id = ?;
