-- +goose Up
-- Per-user favorited IPTV channels.
--
-- Kept separate from the `user_data` table (which targets `items` — movies,
-- episodes, etc.) because channels live in a different table and a join
-- on a polymorphic `target_id` would muddy the existing queries. A narrow
-- table with a composite primary key is simpler and faster for the two
-- hot operations: "is this channel favorited?" and "list this user's
-- favorite channels".
--
-- CASCADE on both foreign keys so favorites disappear cleanly when a user
-- is deleted or a channel is rotated out during an M3U refresh.
CREATE TABLE user_channel_favorites (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, channel_id)
);

-- Index supports the "list my favorites" query ordered by created_at.
-- (SQLite's composite primary key already covers (user_id) prefix lookups,
-- but an explicit index with created_at lets the optimizer skip a sort.)
CREATE INDEX idx_user_channel_favorites_user_created
    ON user_channel_favorites(user_id, created_at DESC);
