-- +goose Up
--
-- 042_user_channel_order.sql — per-user personalisation of the IPTV
-- channel list. Postgres twin of migrations/sqlite/042. See the
-- sqlite file for the design rationale.
--
-- Dialect notes:
--   - hidden is a real BOOLEAN (vs sqlite's INTEGER+CHECK).
--   - updated_at is TIMESTAMPTZ with NOW() as the default, matching
--     the rest of the postgres schema.

CREATE TABLE user_channel_order (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    hidden     BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, channel_id)
);

CREATE INDEX idx_user_channel_order_user
    ON user_channel_order (user_id, position);
