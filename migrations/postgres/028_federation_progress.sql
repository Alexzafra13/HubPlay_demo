-- +goose Up
-- Federation Phase 5 follow-up: cross-peer Continue Watching. See
-- SQLite sibling for the full rationale.
--
-- Postgres translation:
--   • position_ticks / duration_ticks: BIGINT (movies easily exceed
--     2^31 in 10M-ticks-per-second units — a 220 minute movie is
--     ~1.3 * 10^14 ticks).
--   • DATETIME → TIMESTAMPTZ
--   • BOOLEAN DEFAULT 0 → DEFAULT FALSE

CREATE TABLE federation_progress (
    user_id          TEXT NOT NULL,
    peer_id          TEXT NOT NULL,
    remote_item_id   TEXT NOT NULL,
    position_ticks   BIGINT NOT NULL,
    duration_ticks   BIGINT NOT NULL DEFAULT 0,
    completed        BOOLEAN NOT NULL DEFAULT FALSE,
    last_played_at   TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, peer_id, remote_item_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (peer_id) REFERENCES federation_peers(id) ON DELETE CASCADE
);

CREATE INDEX idx_fed_progress_user_recent
    ON federation_progress(user_id, last_played_at DESC);
