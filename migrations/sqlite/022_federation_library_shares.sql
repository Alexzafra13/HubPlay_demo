-- +goose Up
-- Federation Phase 3: per-library opt-in shares.
--
-- Phase 1 + 2 gave us paired peers and authenticated calls between
-- them. Phase 3 makes those calls actually do something visible: a
-- peer can list and browse the libraries we've explicitly shared
-- with them, with admin-controlled scopes.
--
-- Design choices:
--
--   * Per-library opt-in. NO blanket "share everything" — each
--     (peer_id, library_id) pair is its own row. Admin checks a
--     library on a peer's panel; row created. Unchecks it; row
--     deleted. UNIQUE constraint enforces that a peer can have at
--     most one share row per library (no scope conflicts to
--     reconcile).
--
--   * Flat boolean scope columns for the v1 set (browse, play,
--     download, livetv) PLUS a JSON `extra_scopes` escape hatch.
--     Future scopes (e.g. "share watch history") add a JSON key
--     without a schema migration — the existing 4 stay flat for
--     fast JOIN filtering.
--
--   * Defaults matter: can_browse=1 + can_play=1 (the obvious
--     "share my library" intent), can_download=0 + can_livetv=0
--     (more sensitive — download burns peer's disk, livetv burns
--     OUR upstream + IPTV provider quota).
--
--   * created_by tracks WHICH admin made the share. With a single-
--     admin self-hosted deployment this is mostly cosmetic, but for
--     multi-admin setups (or a future audit-trail policy that
--     wants "who can revoke whose shares") it's load-bearing.

CREATE TABLE federation_library_shares (
    id            TEXT PRIMARY KEY,
    peer_id       TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id    TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    can_browse    BOOLEAN NOT NULL DEFAULT 1,
    can_play      BOOLEAN NOT NULL DEFAULT 1,
    can_download  BOOLEAN NOT NULL DEFAULT 0,
    can_livetv    BOOLEAN NOT NULL DEFAULT 0,
    extra_scopes  TEXT,                                            -- JSON object, future-proof
    created_by    TEXT NOT NULL REFERENCES users(id),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(peer_id, library_id)
);

-- Hot path: "what can peer X see?" — every authenticated peer call
-- that lists libraries / items hits this index.
CREATE INDEX idx_fed_share_peer ON federation_library_shares(peer_id);

-- Hot path: "who's seeing library Y?" — admin reviewing exposure of
-- a library; cascading delete on library_id; sorting in admin UI.
CREATE INDEX idx_fed_share_library ON federation_library_shares(library_id);


-- +goose Down
DROP INDEX IF EXISTS idx_fed_share_library;
DROP INDEX IF EXISTS idx_fed_share_peer;
DROP TABLE IF EXISTS federation_library_shares;
