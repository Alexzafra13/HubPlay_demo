-- +goose Up
--
-- 058_drop_redundant_indexes.sql — remove two indexes that are fully
-- covered by their table's primary key, so every write maintained them
-- for nothing.
--
--   * idx_user_data_user(user_id) — user_data's PK is
--     (user_id, item_id). The PK index already answers every
--     `WHERE user_id = ?` lookup via its leftmost-prefix, so this
--     standalone index is pure write amplification on UpdateProgress,
--     the hottest write in the app.
--
--   * idx_media_streams_item(item_id) — media_streams' PK is
--     (item_id, stream_index). Same story: the PK prefix already serves
--     the detail-page `WHERE item_id = ?` lookup. (Added in 025 under a
--     comment that incorrectly assumed there was no PK on the table.)
--
-- NOT dropped: idx_user_data_last_played_at (045) — although it overlaps
-- idx_user_data_last_played (026) on the same column, it is a deliberate
-- partial index purpose-built for the admin DailyWatchActivity / TopItems
-- queries (WHERE last_played_at IS NOT NULL AND last_played_at >= ?);
-- keeping it avoids a measurable regression on those panels.

DROP INDEX IF EXISTS idx_user_data_user;
DROP INDEX IF EXISTS idx_media_streams_item;
