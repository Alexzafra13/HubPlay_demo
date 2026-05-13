-- Image assets (poster, backdrop, thumb, logo, banner) per item.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE images)
-- + migration 013_image_lock.sql (is_locked column).
-- NOTE: GetPrimaryURLs uses dynamic IN() and remains raw SQL in the adapter.

-- name: CreateImage :exec
INSERT INTO images (id, item_id, type, path, width, height, blurhash, provider, is_primary, is_locked, added_at, dominant_color, dominant_color_muted)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: GetImageByID :one
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE id = $1;

-- name: GetPrimaryImage :one
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE item_id = $1 AND type = $2 AND is_primary;

-- name: ListImagesByItem :many
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE item_id = $1
ORDER BY is_primary DESC, type;

-- name: DeleteImagesByItem :exec
DELETE FROM images WHERE item_id = $1;

-- name: DeleteImageByID :exec
DELETE FROM images WHERE id = $1;

-- name: UnsetPrimaryImages :exec
UPDATE images SET is_primary = FALSE WHERE item_id = $1 AND type = $2;

-- name: SetImagePrimary :exec
UPDATE images SET is_primary = TRUE WHERE id = $1 AND item_id = $2 AND type = $3;

-- name: SetImageLocked :exec
UPDATE images SET is_locked = $1 WHERE id = $2;

-- name: HasLockedImageForKind :one
SELECT COUNT(*) > 0 AS has_lock FROM images
WHERE item_id = $1 AND type = $2 AND is_locked;
