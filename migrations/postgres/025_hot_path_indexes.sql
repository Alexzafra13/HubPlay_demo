-- +goose Up
-- Hot-path indexes for browse, search, filter and "now playing"
-- queries. See SQLite sibling for the full audit + reasoning.
-- Translation is 1:1 — all index syntax, partial-index WHERE
-- predicates, and DESC ordering are identical between dialects.

CREATE INDEX IF NOT EXISTS idx_items_browse_sort
    ON items(library_id, type, sort_title);

CREATE INDEX IF NOT EXISTS idx_items_added_at
    ON items(library_id, type, added_at DESC);

CREATE INDEX IF NOT EXISTS idx_items_year
    ON items(library_id, type, year)
    WHERE year IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_items_rating
    ON items(library_id, type, community_rating)
    WHERE community_rating IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_epg_channel_end_time
    ON epg_programs(channel_id, end_time);

CREATE INDEX IF NOT EXISTS idx_user_data_continue
    ON user_data(user_id, completed, last_played_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_streams_item
    ON media_streams(item_id);
