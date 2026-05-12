-- +goose Up
-- "Locked" flag on images. See SQLite sibling for the full
-- rationale. Postgres translation:
--   • BOOLEAN NOT NULL DEFAULT 0 → BOOLEAN NOT NULL DEFAULT FALSE
ALTER TABLE images ADD COLUMN is_locked BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_images_item_type_locked ON images(item_id, type, is_locked);

-- +goose Down
DROP INDEX IF EXISTS idx_images_item_type_locked;
ALTER TABLE images DROP COLUMN IF EXISTS is_locked;
