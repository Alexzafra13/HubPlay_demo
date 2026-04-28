-- Image assets (poster, backdrop, thumb, logo, banner) per item.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE images)
-- + migration 013_image_lock.sql (is_locked column).
-- NOTE: GetPrimaryURLs uses dynamic IN() and remains raw SQL in the adapter.

-- name: CreateImage :exec
INSERT INTO images (id, item_id, type, path, width, height, blurhash, provider, is_primary, is_locked, added_at, dominant_color, dominant_color_muted)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetImageByID :one
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE id = ?;

-- name: GetPrimaryImage :one
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE item_id = ? AND type = ? AND is_primary = 1;

-- name: ListImagesByItem :many
SELECT id, item_id, type, path, COALESCE(width, 0) AS width, COALESCE(height, 0) AS height,
       COALESCE(blurhash, '') AS blurhash, COALESCE(provider, '') AS provider,
       is_primary, is_locked, added_at,
       COALESCE(dominant_color, '') AS dominant_color,
       COALESCE(dominant_color_muted, '') AS dominant_color_muted
FROM images
WHERE item_id = ?
ORDER BY is_primary DESC, type;

-- name: DeleteImagesByItem :exec
DELETE FROM images WHERE item_id = ?;

-- name: DeleteImageByID :exec
DELETE FROM images WHERE id = ?;

-- name: UnsetPrimaryImages :exec
UPDATE images SET is_primary = 0 WHERE item_id = ? AND type = ?;

-- name: SetImagePrimary :exec
UPDATE images SET is_primary = 1 WHERE id = ? AND item_id = ? AND type = ?;

-- name: SetImageLocked :exec
UPDATE images SET is_locked = ? WHERE id = ?;

-- name: HasLockedImageForKind :one
SELECT COUNT(*) > 0 AS has_lock FROM images
WHERE item_id = ? AND type = ? AND is_locked = 1;
