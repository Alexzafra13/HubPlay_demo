-- +goose Up
-- Per-user "continue watching" history for LiveTV channels.
--
-- Powers the rail at the top of Discover that lets a user resume the
-- channel they last watched from any device.
--
-- Keyed by (user_id, stream_url) — NOT (user_id, channel_id) — because
-- the IPTV refresher regenerates channel UUIDs on every M3U import.
-- A channel_id FK would CASCADE-wipe the entire rail the morning after
-- a scheduled playlist refresh. Same lesson the channel_overrides
-- table learned (migration 009): stream_url is the stable attribute
-- of a channel, the UUID isn't.
--
-- The read path JOINs `channel_watch_history.stream_url` against
-- `channels.stream_url` at query time. The JOIN handles the "stream
-- was removed from the playlist" case silently (the row stays but
-- joins to nothing; it'll reappear if the URL returns).
--
-- CASCADE on user_id only: history survives channel rewrites but dies
-- with the user.
--
-- Index on (user_id, last_watched_at DESC): the rail's hot query is
-- "top N for current user ordered by recency". The composite PK
-- already prefixes on user_id, but the ORDER BY needs its own sort —
-- the dedicated index lets SQLite walk the index in order rather than
-- sorting in memory.
CREATE TABLE channel_watch_history (
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stream_url      TEXT NOT NULL,
    last_watched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, stream_url)
);

CREATE INDEX idx_watch_history_user_recent
    ON channel_watch_history(user_id, last_watched_at DESC);
