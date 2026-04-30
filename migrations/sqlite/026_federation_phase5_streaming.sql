-- +goose Up
-- Federation Phase 5 schema additions: remote-user identity + viewer-side
-- watch progress on peer content.
--
-- TWO tables, two distinct concerns:
--
--  * federation_remote_users — origin-side identity tracking. When a peer
--    streams from us, the request body declares `remote_user_id` (their
--    user id on their server). We persist that pair (peer_id,
--    remote_user_id) so per-user concurrency caps and audit logs have a
--    stable handle. NEVER joins to local users.id; a peer's user_x is
--    cryptographically distinct from our user_x even if the strings
--    happen to match.
--
--  * peer_item_progress — viewer-side watch state. When OUR user plays
--    content from a peer, we track position locally so the user's own
--    Continue Watching rail remembers. Keyed on (user_id, peer_id,
--    remote_item_id) — a triple that's globally unique within our DB
--    without colliding with the local user_data table (which keys on
--    local item.id and would not understand a peer's id).
--
-- The asymmetry — origin tracks remote-user, viewer tracks own-user — is
-- deliberate: each side needs the data it owns. There's no
-- viewer→origin progress sync in v1; the origin doesn't need to know
-- that peer A's user X is paused at minute 30 of peer B's "Inception".

-- ─── federation_remote_users ────────────────────────────────────────
-- One row per (peer, peer's-user) pair the origin has ever seen.
-- Created lazily on first stream-session-start; never deleted (audit
-- trail). display_name is whatever the peer's request body said —
-- treated as data, never as auth ("Alex" is a label, not a claim).

CREATE TABLE federation_remote_users (
    id              TEXT PRIMARY KEY,                     -- our local UUID for this remote user
    peer_id         TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_user_id  TEXT NOT NULL,                        -- as declared by the peer
    display_name    TEXT NOT NULL DEFAULT '',
    first_seen_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(peer_id, remote_user_id)
);
CREATE INDEX idx_fed_remote_users_peer ON federation_remote_users(peer_id);

-- ─── peer_item_progress ─────────────────────────────────────────────
-- Viewer-side: my user X has progress on peer Y's item Z.
-- Compound primary key is the natural identity; no surrogate.
-- Updated by the player every ~5s during playback (same cadence as
-- local progress). Read by the Continue Watching rail when the user
-- opens /peers, surfaced alongside local items.

CREATE TABLE peer_item_progress (
    user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    peer_id          TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_item_id   TEXT NOT NULL,
    position_seconds REAL NOT NULL DEFAULT 0,
    duration_seconds REAL,
    played           BOOLEAN NOT NULL DEFAULT 0,
    is_favorite      BOOLEAN NOT NULL DEFAULT 0,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, peer_id, remote_item_id)
);

-- Hot index: ContinueWatching rail filters this user's recent
-- not-completed peer items. Compound matches the WHERE+ORDER BY of
-- the rail query.
CREATE INDEX idx_peer_progress_user_recent
    ON peer_item_progress(user_id, played, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_peer_progress_user_recent;
DROP TABLE IF EXISTS peer_item_progress;
DROP INDEX IF EXISTS idx_fed_remote_users_peer;
DROP TABLE IF EXISTS federation_remote_users;
