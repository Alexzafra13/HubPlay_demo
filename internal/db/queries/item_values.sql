-- Normalized tag store reused for genres (and future tag-like facets).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (item_values, item_value_map).
-- Backfill: migrations/sqlite/031_item_values_genres_backfill.sql.
--
-- The synthetic id format ("<type>:<clean_value>", lowercased) makes
-- INSERT-OR-IGNORE idempotent across items sharing the same value, so
-- the scanner can re-run UpsertItemValue per scan without dedup logic.

-- name: UpsertItemValue :exec
INSERT INTO item_values (id, type, value, clean_value)
VALUES (?, ?, ?, ?)
ON CONFLICT(type, clean_value) DO UPDATE SET value = excluded.value;

-- name: LinkItemValue :exec
INSERT OR IGNORE INTO item_value_map (item_id, value_id)
VALUES (?, ?);

-- name: ClearItemValuesForItem :exec
-- Used before re-populating an item's tags during a metadata refresh,
-- so removed genres don't linger after a TMDb update changes them.
DELETE FROM item_value_map
WHERE item_id = ? AND value_id IN (
    SELECT id FROM item_values WHERE type = ?
);

-- ListItemValuesByType is intentionally implemented as raw SQL in the
-- repository (item_value_repository.go::ListGenres) — sqlc v1.31.1
-- truncates the last identifier of the final query in a file, which
-- breaks the trailing ORDER BY / LIMIT regardless of phrasing.

