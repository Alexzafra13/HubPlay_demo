-- +goose Up
-- Federation Phase 5 Slice 2: catalog cache learns about posters.
--
-- The `/peer/libraries/{id}/items` response now carries a `has_poster`
-- flag per item so the calling peer can decide whether to surface a
-- poster URL in its catalog UI without an extra round trip per item.
-- We mirror the flag in the local catalog cache so cached browsing
-- (peer offline, or under the staleness threshold) renders posters
-- consistently with live browsing.
--
-- Default 0: existing rows pre-date the flag and are conservatively
-- treated as poster-less. The next live refresh will repopulate the
-- column from the peer's authoritative response.

ALTER TABLE federation_item_cache
    ADD COLUMN has_poster BOOLEAN NOT NULL DEFAULT 0;


-- +goose Down
-- SQLite does not support DROP COLUMN before 3.35; the project pins a
-- newer runtime via modernc.org/sqlite, but to keep `goose down` cheap
-- and not fight migration tooling on older debug shells, we recreate
-- the table without the column. Acceptable for a dev-rollback path.
CREATE TABLE federation_item_cache_old (
    peer_id      TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id   TEXT NOT NULL,
    remote_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    title        TEXT NOT NULL,
    year         INTEGER,
    overview     TEXT,
    cached_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (peer_id, remote_id)
);
INSERT INTO federation_item_cache_old (peer_id, library_id, remote_id, type, title, year, overview, cached_at)
    SELECT peer_id, library_id, remote_id, type, title, year, overview, cached_at
      FROM federation_item_cache;
DROP TABLE federation_item_cache;
ALTER TABLE federation_item_cache_old RENAME TO federation_item_cache;
CREATE INDEX idx_fed_cache_peer_lib ON federation_item_cache(peer_id, library_id);
CREATE INDEX idx_fed_cache_age ON federation_item_cache(cached_at);
