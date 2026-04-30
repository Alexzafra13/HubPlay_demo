-- +goose Up
-- Hot-path indexes for browse, search, filter and "now playing" queries.
--
-- The audit was: look at every query that runs in the page-render
-- critical path with a `LIMIT N` clause, and confirm the planner has
-- a covering index for the WHERE + ORDER BY combination. Where it
-- didn't, we'd see SCAN TABLE in EXPLAIN, which scales linearly with
-- table size.
--
-- The indexes below are the minimum to keep these queries on a B-tree
-- lookup regardless of how big the library gets (50k movies, 5000
-- channels, 100k EPG programmes/day):
--
--   1. Movies/Series browse with title sort.
--   2. "Recently Added" rail.
--   3. Filter by year / rating (the MediaBrowse filter UI).
--   4. EPG "now playing" — pinpoint a channel's currently-airing slot.
--   5. ContinueWatching — find the most recent paused-not-finished item.
--   6. /items/{id} detail — fetch the media-streams row(s) for it.
--
-- Cost: each index is bytes-per-row × N rows on disk + a bit of write
-- amplification on INSERT/UPDATE. Acceptable for a server profile
-- where reads dominate writes by orders of magnitude.

-- 1. Movies / Series browse with sort by title.
--    Existing `idx_items_library_parent_type` covers (library_id, parent_id, type)
--    but not the ORDER BY sort_title — without this, a `WHERE library_id=? AND
--    type='movie' ORDER BY sort_title LIMIT 50` does the lookup but then has
--    to read all matched rows and sort them. With this index, the rows come
--    back already in sort order and we stop after LIMIT.
CREATE INDEX IF NOT EXISTS idx_items_browse_sort
    ON items(library_id, type, sort_title);

-- 2. "Recently Added" rail and admin "added in last 7 days" queries.
--    DESC ordering matches the natural query — newest first — so the
--    planner walks the index backwards without an extra sort step.
CREATE INDEX IF NOT EXISTS idx_items_added_at
    ON items(library_id, type, added_at DESC);

-- 3. Filter by year. Partial index on rows where year IS NOT NULL —
--    NULL year is the dominant value during a fresh scan (until provider
--    enrichment fills it in), and we never filter for "year IS NULL"
--    in the UI, so excluding those rows shrinks the index materially.
CREATE INDEX IF NOT EXISTS idx_items_year
    ON items(library_id, type, year)
    WHERE year IS NOT NULL;

-- 4. Filter by minimum rating. Same partial-index reasoning — most
--    items have NULL rating until enrichment runs, and the UI filter
--    is "rating >= X", which uses non-null values only.
CREATE INDEX IF NOT EXISTS idx_items_rating
    ON items(library_id, type, community_rating)
    WHERE community_rating IS NOT NULL;

-- 5. EPG "now playing" lookup.
--    Existing `idx_epg_channel_time` covers (channel_id, start_time)
--    which finds programmes by start. But "what's on right now" needs
--    `start_time <= now AND end_time > now`. With only the start-time
--    index, the planner finds candidates by start_time and then has
--    to filter end_time row-by-row. This index lets the planner do
--    the end_time filter on the index itself.
CREATE INDEX IF NOT EXISTS idx_epg_channel_end_time
    ON epg_programs(channel_id, end_time);

-- 6. ContinueWatching: WHERE user_id=? AND completed=0 ORDER BY last_played_at DESC.
--    Existing `idx_user_data_completed` covers (user_id, completed) which finds
--    the rows; this extends with last_played_at DESC so the planner skips the
--    sort entirely and walks the index in reverse to LIMIT.
CREATE INDEX IF NOT EXISTS idx_user_data_continue
    ON user_data(user_id, completed, last_played_at DESC);

-- 7. /items/{id} detail loads `SELECT * FROM media_streams WHERE item_id=?`.
--    There's no PK on media_streams (item_id, stream_index would be the
--    natural one but the schema didn't pin it). Without an index on
--    item_id, every detail page scans the entire table. This is the
--    worst-scaling missing index: a library with 50k items × 3-5 streams
--    per item × every detail click = full scan of 200k rows.
CREATE INDEX IF NOT EXISTS idx_media_streams_item
    ON media_streams(item_id);

-- +goose Down
DROP INDEX IF EXISTS idx_media_streams_item;
DROP INDEX IF EXISTS idx_user_data_continue;
DROP INDEX IF EXISTS idx_epg_channel_end_time;
DROP INDEX IF EXISTS idx_items_rating;
DROP INDEX IF EXISTS idx_items_year;
DROP INDEX IF EXISTS idx_items_added_at;
DROP INDEX IF EXISTS idx_items_browse_sort;
