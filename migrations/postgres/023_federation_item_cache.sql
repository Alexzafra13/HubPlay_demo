-- +goose Up
-- Federation Phase 4: catalog cache for remote items. See SQLite
-- sibling for design notes. Direct translation.
CREATE TABLE federation_item_cache (
    peer_id      TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id   TEXT NOT NULL,
    remote_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    title        TEXT NOT NULL,
    year         INTEGER,
    overview     TEXT,
    cached_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (peer_id, remote_id)
);

CREATE INDEX idx_fed_cache_peer_lib ON federation_item_cache(peer_id, library_id);
CREATE INDEX idx_fed_cache_age ON federation_item_cache(cached_at);

-- +goose Down
DROP INDEX IF EXISTS idx_fed_cache_age;
DROP INDEX IF EXISTS idx_fed_cache_peer_lib;
DROP TABLE IF EXISTS federation_item_cache;
