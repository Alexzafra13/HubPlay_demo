-- +goose Up
-- Per-user "continue watching" history for LiveTV channels. See
-- SQLite sibling for the full design notes (stream_url key vs
-- channel_id, no FK to channels, CASCADE only on user).
CREATE TABLE channel_watch_history (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stream_url      TEXT NOT NULL,
    last_watched_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, stream_url)
);

CREATE INDEX idx_watch_history_user_recent
    ON channel_watch_history(user_id, last_watched_at DESC);
