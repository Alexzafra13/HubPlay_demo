-- Per-user per-item interaction data (progress, favorites, play history).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE user_data).
-- PK: (user_id, item_id).

-- name: UpsertUserData :exec
INSERT INTO user_data (user_id, item_id, position_ticks, play_count, completed,
    is_favorite, liked, audio_stream_index, subtitle_stream_index, last_played_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, item_id) DO UPDATE SET
    position_ticks = excluded.position_ticks,
    play_count = excluded.play_count,
    completed = excluded.completed,
    is_favorite = excluded.is_favorite,
    liked = excluded.liked,
    audio_stream_index = excluded.audio_stream_index,
    subtitle_stream_index = excluded.subtitle_stream_index,
    last_played_at = excluded.last_played_at,
    updated_at = excluded.updated_at;

-- name: GetUserData :one
SELECT user_id, item_id, position_ticks, play_count, completed,
       is_favorite, liked, audio_stream_index, subtitle_stream_index,
       last_played_at, updated_at
FROM user_data
WHERE user_id = ? AND item_id = ?;

-- name: UpdateProgress :exec
INSERT INTO user_data (user_id, item_id, position_ticks, completed, last_played_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, item_id) DO UPDATE SET
    position_ticks = excluded.position_ticks,
    completed = excluded.completed,
    last_played_at = excluded.last_played_at,
    updated_at = excluded.updated_at;

-- name: MarkPlayed :exec
INSERT INTO user_data (user_id, item_id, play_count, completed, last_played_at, updated_at)
VALUES (?, ?, 1, 1, ?, ?)
ON CONFLICT(user_id, item_id) DO UPDATE SET
    play_count = user_data.play_count + 1,
    completed = 1,
    position_ticks = 0,
    last_played_at = excluded.last_played_at,
    updated_at = excluded.updated_at;

-- name: SetFavorite :exec
INSERT INTO user_data (user_id, item_id, is_favorite, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, item_id) DO UPDATE SET
    is_favorite = excluded.is_favorite,
    updated_at = excluded.updated_at;

-- name: DeleteUserData :exec
DELETE FROM user_data WHERE user_id = ? AND item_id = ?;

-- name: ContinueWatching :many
SELECT ud.item_id, ud.position_ticks, ud.last_played_at,
       i.title, i.type, i.duration_ticks, COALESCE(i.parent_id, '') AS parent_id,
       COALESCE(i.container, '') AS container
FROM user_data ud
JOIN items i ON i.id = ud.item_id
WHERE ud.user_id = ? AND ud.completed = 0 AND ud.position_ticks > 0
  AND i.is_available = 1
ORDER BY ud.last_played_at DESC
LIMIT ?;

-- name: ListFavorites :many
SELECT ud.item_id, ud.updated_at,
       i.title, i.type, i.year, i.duration_ticks
FROM user_data ud
JOIN items i ON i.id = ud.item_id
WHERE ud.user_id = ? AND ud.is_favorite = 1
  AND i.is_available = 1
ORDER BY ud.updated_at DESC
LIMIT ? OFFSET ?;

-- NOTE: NextUp uses a CTE with duplicate user_id params that sqlc can't handle
-- for SQLite. It remains as raw SQL in the repository adapter.
