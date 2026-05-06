-- +goose Up
-- Genre filtering on /movies and /series used to happen client-side
-- over the loaded page, which made it useless beyond the first 40
-- items in any library. Server-side filtering needs a normalized
-- representation: `metadata.genres_json` is a serialized JSON array
-- and SQLite can't index into it without scanning every row.
--
-- The schema has carried `item_values` (id, type, value, clean_value)
-- + `item_value_map` (item_id, value_id) since the initial schema as
-- a generic tag store, but no code populated it. Genres are the first
-- caller; tags and other facets can reuse the same surface later
-- without another migration.
--
-- This migration:
--   1. Indexes (type, clean_value) for fast filter lookups.
--   2. Indexes (value_id) on the map for the JOIN in the filter query
--      (the existing idx_value_map_value covers this — confirm and skip
--      if present).
--   3. Backfills genres from metadata.genres_json so existing libraries
--      get filtering immediately without a re-scan. The scanner update
--      keeps the two surfaces in sync going forward.
--
-- The backfill runs as a single statement using SQLite's json_each:
-- safe even if genres_json is NULL/empty/malformed (json_each errors
-- get isolated by the WHERE json_valid guard).

CREATE INDEX IF NOT EXISTS idx_item_values_type_clean
    ON item_values (type, clean_value);

-- Backfill: explode genres_json arrays into item_values + item_value_map.
-- Use the metadata's item_id, lower-trim the genre name as clean_value,
-- and synthesize a deterministic id ("genre:" + clean_value) so the
-- INSERT-OR-IGNORE idempotently merges duplicates across items.
INSERT OR IGNORE INTO item_values (id, type, value, clean_value)
SELECT
    'genre:' || LOWER(TRIM(je.value)) AS id,
    'genre' AS type,
    TRIM(je.value) AS value,
    LOWER(TRIM(je.value)) AS clean_value
FROM metadata m, json_each(m.genres_json) je
WHERE m.genres_json IS NOT NULL
  AND m.genres_json != ''
  AND json_valid(m.genres_json)
  AND TRIM(je.value) != '';

INSERT OR IGNORE INTO item_value_map (item_id, value_id)
SELECT
    m.item_id,
    'genre:' || LOWER(TRIM(je.value))
FROM metadata m, json_each(m.genres_json) je
WHERE m.genres_json IS NOT NULL
  AND m.genres_json != ''
  AND json_valid(m.genres_json)
  AND TRIM(je.value) != '';
