-- Per-user IPTV channel favorites.
--
-- Table schema: migrations/sqlite/006_channel_favorites.sql.
-- PK: (user_id, channel_id).

-- name: AddChannelFavorite :exec
INSERT INTO user_channel_favorites (user_id, channel_id, created_at)
VALUES (?, ?, ?)
ON CONFLICT(user_id, channel_id) DO NOTHING;

-- name: RemoveChannelFavorite :exec
DELETE FROM user_channel_favorites
WHERE user_id = ? AND channel_id = ?;

-- name: ListChannelFavorites :many
SELECT channel_id, created_at
FROM user_channel_favorites
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: IsChannelFavorite :one
SELECT 1
FROM user_channel_favorites
WHERE user_id = ? AND channel_id = ?
LIMIT 1;

-- name: ListChannelFavoritesWithChannel :many
SELECT c.id, c.library_id, c.name, c.number,
       COALESCE(c.group_name, '') AS group_name,
       COALESCE(c.logo_url, '') AS logo_url, c.stream_url,
       COALESCE(c.tvg_id, '') AS tvg_id,
       COALESCE(c.language, '') AS language,
       COALESCE(c.country, '') AS country,
       c.is_active, c.added_at, f.created_at AS favorited_at
FROM user_channel_favorites f
JOIN channels c ON c.id = f.channel_id
WHERE f.user_id = ? AND c.is_active = 1
ORDER BY f.created_at DESC;
