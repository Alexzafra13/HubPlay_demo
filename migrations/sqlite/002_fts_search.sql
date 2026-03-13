-- +goose Up
-- FTS5 disabled: ncruces/go-sqlite3 (wasm) has stability issues with FTS5.
-- Search uses LIKE queries on items.title instead.
-- This migration is intentionally empty — kept as a placeholder for future native FTS5.

-- +goose Down
