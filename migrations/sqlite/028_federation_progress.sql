-- +goose Up
-- Federation Phase 5 follow-up: cross-peer Continue Watching.
--
-- Local watch state lives in `user_data` keyed by (user_id, item_id),
-- where item_id is a row in the local `items` table. Federated items
-- never get an `items` row -- they are only ever materialised in the
-- per-peer `federation_item_cache`. Reusing user_data would require
-- either inserting fake items rows (pollutes the catalog) or relaxing
-- the FK on user_data (loses the strong locality the local rails rely
-- on). Neither is worth it; we keep federation watch state in its own
-- table and join the cache for title / duration when rendering.
--
-- Each row is the most recent playback position for one (user, peer,
-- remote_item) triple. duration_ticks is snapshotted at upsert time
-- so the Continue Watching rail can compute percentage without a
-- second hop, and so the "near-complete" filter (>= 90%) survives
-- cache eviction. last_played_at drives ORDER BY for the rail.
--
-- ON DELETE CASCADE on peer_id matches the federation_peers contract
-- (revoking a peer wipes everything tied to it). user_id cascades to
-- match user_data.

CREATE TABLE federation_progress (
    user_id          TEXT NOT NULL,
    peer_id          TEXT NOT NULL,
    remote_item_id   TEXT NOT NULL,
    position_ticks   INTEGER NOT NULL,
    duration_ticks   INTEGER NOT NULL DEFAULT 0,
    completed        BOOLEAN NOT NULL DEFAULT 0,
    last_played_at   DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    PRIMARY KEY (user_id, peer_id, remote_item_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (peer_id) REFERENCES federation_peers(id) ON DELETE CASCADE
);

-- Continue Watching rail orders by last_played_at desc per user. The
-- composite index lets the planner skip a sort on the hot path.
CREATE INDEX idx_fed_progress_user_recent
    ON federation_progress(user_id, last_played_at DESC);


