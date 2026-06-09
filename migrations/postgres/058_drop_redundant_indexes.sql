-- +goose Up
--
-- 058_drop_redundant_indexes.sql — Postgres twin of the SQLite migration.
-- Removes two indexes fully covered by their table's primary key, so
-- every write no longer maintains them for nothing:
--
--   * idx_user_data_user(user_id) — covered by PK (user_id, item_id).
--   * idx_media_streams_item(item_id) — covered by PK (item_id, stream_index).
--
-- NOT dropped: idx_user_data_last_played_at (045), a deliberate partial
-- index for the admin DailyWatchActivity / TopItems queries.

DROP INDEX IF EXISTS idx_user_data_user;
DROP INDEX IF EXISTS idx_media_streams_item;
