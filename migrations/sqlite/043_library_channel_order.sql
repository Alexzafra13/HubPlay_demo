-- +goose Up
--
-- 043_library_channel_order.sql — admin override of the channel
-- list a library exports. M3U import fills channels.number with the
-- order the playlist provided; this table lets the admin curate
-- that default (reorder, hide bad / inappropriate channels) without
-- mutating the channel rows themselves.
--
-- This is the admin counterpart of 042_user_channel_order. Both
-- overlays apply at read time and compose:
--   (1) start from channels.number;
--   (2) admin overlay (this table) reorders + hides — hidden HERE
--       is a hard constraint, users cannot un-hide what the admin
--       removed;
--   (3) user overlay (042) reorders + hides on top of what survived
--       (2). Users can only hide more, never less.

CREATE TABLE library_channel_order (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    hidden     INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1)),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, channel_id)
);

CREATE INDEX idx_library_channel_order_library
    ON library_channel_order (library_id, position);
