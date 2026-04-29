-- +goose Up
-- Federation Phase 4: catalog cache for remote items.
--
-- When a user opens a peer's library, we cache the items locally so:
--
--   1. The browse-feels-instant. After the first fetch the user gets
--      cached results in <10ms instead of waiting on a remote round-
--      trip every page change.
--   2. The catalog stays browsable even when the peer is offline.
--      Cached rows + a small "offline" badge in the UI is much better
--      UX than an empty page or an error.
--
-- Cache freshness: rows have cached_at; admin can configure staleness
-- threshold (default 6h). On a stale read we kick a background
-- refresh; on a hit older than 24h (hard ceiling) we serve stale +
-- queue refresh — same approach as Plex's catalog_cache.
--
-- Why a single table per item (not a tree of items + media_streams +
-- chapters): we deliberately cache only the metadata the BROWSE UI
-- needs. Item detail (cast, episodes, streams) is fetched on-demand
-- when the user opens an item — it's a much smaller blast radius if
-- we get the per-detail wire format wrong.

CREATE TABLE federation_item_cache (
    peer_id      TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id   TEXT NOT NULL,                  -- remote's library_id (string, opaque to us)
    remote_id    TEXT NOT NULL,                  -- remote's items.id
    type         TEXT NOT NULL,
    title        TEXT NOT NULL,
    year         INTEGER,
    overview     TEXT,
    cached_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (peer_id, remote_id)
);

-- Hot path: list items in (peer, library), paginated. ORDER BY title
-- in the query path so the index doesn't need to cover sort.
CREATE INDEX idx_fed_cache_peer_lib ON federation_item_cache(peer_id, library_id);

-- Cold-but-cheap: prune by cached_at < cutoff during background refresh.
CREATE INDEX idx_fed_cache_age ON federation_item_cache(cached_at);


-- +goose Down
DROP INDEX IF EXISTS idx_fed_cache_age;
DROP INDEX IF EXISTS idx_fed_cache_peer_lib;
DROP TABLE IF EXISTS federation_item_cache;
