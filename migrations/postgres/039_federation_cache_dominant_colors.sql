-- +goose Up
-- Dominant colour swatches on federation cache (poster_color +
-- poster_color_muted). See SQLite sibling for the rationale.
ALTER TABLE federation_item_cache ADD COLUMN poster_color TEXT NOT NULL DEFAULT '';
ALTER TABLE federation_item_cache ADD COLUMN poster_color_muted TEXT NOT NULL DEFAULT '';
