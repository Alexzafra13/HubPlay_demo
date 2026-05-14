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


