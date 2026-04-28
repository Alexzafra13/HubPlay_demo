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
-- Two extra filters vs. the obvious "started but not completed" rail:
--   1. Near-complete drop: position >= 90% of duration. Treat as
--      effectively done — the user almost certainly finished and the
--      rail showing it as in-progress is noise.
--   2. Abandoned drop: last_played_at older than the caller-supplied
--      threshold AND position < 50%. The user moved on; the rail
--      should not keep nagging about the same start-of-S1E1 forever.
-- Both checks are integer-safe (ticks * 100 / 90 and ticks * 2 / 1)
-- and both require a known duration; rows with duration_ticks = 0
-- are kept (we can't reason about progress without it).
SELECT ud.item_id, ud.position_ticks, ud.last_played_at,
       i.title, i.type, i.duration_ticks, COALESCE(i.parent_id, '') AS parent_id,
       COALESCE(i.container, '') AS container,
       COALESCE(i.season_number, 0) AS season_number,
       COALESCE(i.episode_number, 0) AS episode_number,
       COALESCE(season.parent_id, '') AS series_id
FROM user_data ud
JOIN items i ON i.id = ud.item_id
LEFT JOIN items season ON season.id = i.parent_id
WHERE ud.user_id = ? AND ud.completed = 0 AND ud.position_ticks > 0
  AND i.is_available = 1
  AND NOT (
    i.duration_ticks > 0
    AND ud.position_ticks * 100 >= i.duration_ticks * 90
  )
  AND NOT (
    ud.last_played_at < ?
    AND i.duration_ticks > 0
    AND ud.position_ticks * 2 < i.duration_ticks
  )
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
