-- +goose Up
-- "Locked" flag on images: once an admin manually picks or uploads
-- an artwork, they almost never want a future refresh (manual or
-- scheduled) to silently overwrite it. Plex calls this a "lock";
-- Jellyfin calls it the same. Without it, every time the metadata
-- refresher runs the user's curated poster gets clobbered.
--
-- Default 0 — existing rows are unlocked, matching pre-migration
-- behaviour. The refresher's "skip kinds where any locked image
-- exists" check sees no locked rows and behaves identically.
--
-- Indexed (item_id, type) → lookups by "does this (item, kind) have
-- any locked image?" are the hot path in the refresher's per-item
-- loop. Without the index that's a full scan per item per refresh.
ALTER TABLE images ADD COLUMN is_locked BOOLEAN NOT NULL DEFAULT 0;
CREATE INDEX idx_images_item_type_locked ON images(item_id, type, is_locked);
