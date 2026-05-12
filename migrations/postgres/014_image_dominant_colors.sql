-- +goose Up
-- Dominant-color columns on images for first-paint Aurora gradient.
-- See SQLite sibling for the full rationale.
ALTER TABLE images ADD COLUMN dominant_color TEXT NOT NULL DEFAULT '';
ALTER TABLE images ADD COLUMN dominant_color_muted TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE images DROP COLUMN IF EXISTS dominant_color_muted;
ALTER TABLE images DROP COLUMN IF EXISTS dominant_color;
