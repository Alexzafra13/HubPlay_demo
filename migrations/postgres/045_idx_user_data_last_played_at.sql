-- +goose Up
--
-- 045_idx_user_data_last_played_at.sql — Postgres twin of
-- migrations/sqlite/045. See the sqlite file for the design
-- rationale (hot path #2 of the 2026-05-17 perf baseline).
--
-- Postgres also supports partial indexes with `WHERE` clauses; the
-- planner picks them automatically when the query's WHERE matches.

CREATE INDEX IF NOT EXISTS idx_user_data_last_played_at
    ON user_data(last_played_at)
    WHERE last_played_at IS NOT NULL;
