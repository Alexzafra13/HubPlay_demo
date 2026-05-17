-- +goose Up
--
-- 045_idx_user_data_last_played_at.sql — closes hot path #2 of the
-- 2026-05-17 perf baseline (docs/memory/perf-benchmarks-2026-05-17.md).
-- ActivityRepository.DailyWatchActivity and TopItems both filter
-- `user_data` rows by
--
--   WHERE last_played_at IS NOT NULL AND last_played_at >= ?
--
-- The bench measured 5 000 rows → 10 ms for DailyWatchActivity and
-- 6.9 ms for TopItems against the unindexed column. Both queries are
-- admin-only (`/admin/system/stream-activity`, `/admin/system/top-items`)
-- so the latency hits the panel-open path, not user-facing requests —
-- but the planner is doing a full table scan that an index trivially
-- avoids.
--
-- Partial index because the NULL set (rows for items the user never
-- played) is huge in catalogs with engaged libraries — we don't want
-- to widen the index to cover them. SQLite supports
-- `CREATE INDEX ... WHERE expr` since 3.8 (we require 3.35+ for
-- WITHOUT ROWID and other features in this repo).

CREATE INDEX IF NOT EXISTS idx_user_data_last_played_at
    ON user_data(last_played_at)
    WHERE last_played_at IS NOT NULL;
