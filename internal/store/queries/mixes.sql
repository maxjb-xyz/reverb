-- name: ListMixes :many
SELECT * FROM mixes WHERE user_id = ? ORDER BY CASE kind WHEN 'daily' THEN 0 ELSE 1 END, position, created_at;

-- name: GetMix :one
SELECT * FROM mixes WHERE id = ? AND user_id = ?;

-- name: InsertMix :exec
INSERT INTO mixes (id, user_id, kind, position, name, description, recipe, items, cover_art_id, generated_at, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?);

-- name: UpdateMixRecipe :exec
UPDATE mixes SET name = ?, recipe = ?, updated_at = ? WHERE id = ? AND user_id = ?;

-- name: UpdateMixItems :exec
UPDATE mixes SET description = ?, items = ?, cover_art_id = ?, generated_at = ?, updated_at = ? WHERE id = ? AND user_id = ?;

-- name: DeleteMix :execrows
DELETE FROM mixes WHERE id = ? AND user_id = ?;

-- name: DeleteMixesByKind :exec
DELETE FROM mixes WHERE user_id = ? AND kind = ?;
