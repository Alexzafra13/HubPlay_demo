-- +goose Up
--
-- 042_user_channel_order.sql — per-user personalisation of the IPTV
-- channel list. The admin uploads M3U lists and the resulting
-- channel.number is the initial order everyone sees; this table
-- lets each user override that for their own account.
--
-- Design: COALESCE-based overlay, not a copy. We don't snapshot the
-- entire channel list per user — that would either:
--   - drift (we'd never update a user's snapshot when the admin
--     re-scans the list), or
--   - require constant reconciliation (each scan vs. each user).
--
-- Instead, the user's view is
--     SELECT *, COALESCE(uco.position, c.number) AS effective_position
--     FROM channels c
--     LEFT JOIN user_channel_order uco
--       ON uco.user_id = ? AND uco.channel_id = c.id
--     WHERE uco.hidden IS NULL OR uco.hidden = 0
--     ORDER BY effective_position
--
-- A user with no row in this table sees the admin's order verbatim.
-- A user who reorders a single channel writes ONE row; everything
-- else still inherits the admin order. ON DELETE CASCADE keeps the
-- table self-cleaning when channels or users disappear.

CREATE TABLE user_channel_order (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    -- position is the user's preferred channel number. Free-form
    -- INTEGER; the reorder endpoint assigns sequential values but
    -- nothing in the schema enforces uniqueness, so future features
    -- (groups, decimal sub-orderings) don't have to fight the
    -- constraint.
    position   INTEGER NOT NULL,
    -- hidden = 1 makes the channel disappear from this user's
    -- view. Useful for paring down a 500-channel list to the 20
    -- the user actually watches. Default 0 so reordering doesn't
    -- accidentally hide a channel.
    hidden     INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1)),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, channel_id)
);

CREATE INDEX idx_user_channel_order_user
    ON user_channel_order (user_id, position);
