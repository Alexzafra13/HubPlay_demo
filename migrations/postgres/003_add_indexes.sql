-- +goose Up

CREATE INDEX IF NOT EXISTS idx_items_library_parent_type ON items(library_id, parent_id, type);
CREATE INDEX IF NOT EXISTS idx_channels_library_active ON channels(library_id, is_active);

-- +goose Down
DROP INDEX IF EXISTS idx_channels_library_active;
DROP INDEX IF EXISTS idx_items_library_parent_type;
