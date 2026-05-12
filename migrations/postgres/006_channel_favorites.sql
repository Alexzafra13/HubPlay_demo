-- +goose Up
-- Per-user favorited IPTV channels. See SQLite sibling for the full
-- rationale (composite PK over polymorphic FK, etc.).
--
-- Translation note: the SQLite version uses TIMESTAMP NOT NULL with
-- no default, expecting the caller to set it. We keep that shape in
-- Postgres for parity with the existing repo writes.
CREATE TABLE user_channel_favorites (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, channel_id)
);

CREATE INDEX idx_user_channel_favorites_user_created
    ON user_channel_favorites(user_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_user_channel_favorites_user_created;
DROP TABLE IF EXISTS user_channel_favorites;
