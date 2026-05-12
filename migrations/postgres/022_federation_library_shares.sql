-- +goose Up
-- Federation Phase 3: per-library opt-in shares. See SQLite sibling
-- for the full rationale. Postgres translation:
--   • BOOLEAN DEFAULT 1/0 → DEFAULT TRUE/FALSE
--   • DATETIME → TIMESTAMPTZ
CREATE TABLE federation_library_shares (
    id            TEXT PRIMARY KEY,
    peer_id       TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id    TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    can_browse    BOOLEAN NOT NULL DEFAULT TRUE,
    can_play      BOOLEAN NOT NULL DEFAULT TRUE,
    can_download  BOOLEAN NOT NULL DEFAULT FALSE,
    can_livetv    BOOLEAN NOT NULL DEFAULT FALSE,
    extra_scopes  TEXT,
    created_by    TEXT NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(peer_id, library_id)
);

CREATE INDEX idx_fed_share_peer ON federation_library_shares(peer_id);
CREATE INDEX idx_fed_share_library ON federation_library_shares(library_id);

-- +goose Down
DROP INDEX IF EXISTS idx_fed_share_library;
DROP INDEX IF EXISTS idx_fed_share_peer;
DROP TABLE IF EXISTS federation_library_shares;
