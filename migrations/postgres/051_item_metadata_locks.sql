-- +goose Up
-- Per-item metadata lock. See SQLite sibling for full design rationale
-- (separate table to avoid touching items.Update and the sqlc-generated
-- pipeline behind it).
CREATE TABLE item_metadata_locks (
    item_id    TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    locked_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
