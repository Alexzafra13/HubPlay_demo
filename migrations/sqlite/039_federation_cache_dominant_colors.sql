-- +goose Up
-- Carry the peer's pre-extracted dominant swatches through the consumer-side
-- catalog cache so the page-wide aurora paints from cached data even when
-- the peer is unreachable. Without this, going offline would silently drop
-- the colours and force the frontend back into runtime node-vibrant
-- extraction on every re-render.
--
-- Both columns default to empty string for backward compatibility with
-- existing cache rows seeded before this migration — the wire forwarders
-- already treat "" identically to "field absent".
ALTER TABLE federation_item_cache ADD COLUMN poster_color TEXT NOT NULL DEFAULT '';
ALTER TABLE federation_item_cache ADD COLUMN poster_color_muted TEXT NOT NULL DEFAULT '';
