-- +goose Up
-- Server-side genre filtering: index + backfill into item_values +
-- item_value_map from metadata.genres_json. See SQLite sibling for
-- the full background.
--
-- Postgres translation challenges:
--
--   1. `json_each(text)` (SQLite) → `jsonb_array_elements_text(jsonb)`
--      (Postgres). The text-to-jsonb cast happens inline; we guard
--      it with the `IS JSON ARRAY` predicate (Postgres 16+) so a
--      malformed row doesn't blow up the whole migration.
--
--   2. `json_valid(text)` (SQLite) has no direct equivalent. The
--      Postgres `IS JSON [ARRAY|OBJECT|VALUE]` predicate (16+) is
--      the canonical replacement and doesn't throw on bad input.
--
--   3. `INSERT OR IGNORE` (SQLite) → `INSERT … ON CONFLICT DO
--      NOTHING` (Postgres). Without a conflict-target, the latter
--      ignores conflicts on ANY constraint, matching SQLite's
--      behaviour. For item_values that means either the (type,
--      clean_value) UNIQUE or the id PK; for item_value_map the
--      composite PK.
--
--   4. `LOWER()` and `TRIM()` are identical in both.

CREATE INDEX IF NOT EXISTS idx_item_values_type_clean
    ON item_values (type, clean_value);

-- Backfill: explode genres_json into item_values rows. Skip rows
-- where the JSON is empty, null, or not a valid JSON array.
INSERT INTO item_values (id, type, value, clean_value)
SELECT
    'genre:' || LOWER(TRIM(je.value)) AS id,
    'genre' AS type,
    TRIM(je.value) AS value,
    LOWER(TRIM(je.value)) AS clean_value
FROM metadata m,
     LATERAL jsonb_array_elements_text(m.genres_json::jsonb) je(value)
WHERE m.genres_json IS NOT NULL
  AND m.genres_json != ''
  AND m.genres_json IS JSON ARRAY
  AND TRIM(je.value) != ''
ON CONFLICT DO NOTHING;

-- Map every item to its genre rows. Same JSON gating; deterministic
-- id reproduction so the join lines up with the row inserted above.
INSERT INTO item_value_map (item_id, value_id)
SELECT
    m.item_id,
    'genre:' || LOWER(TRIM(je.value))
FROM metadata m,
     LATERAL jsonb_array_elements_text(m.genres_json::jsonb) je(value)
WHERE m.genres_json IS NOT NULL
  AND m.genres_json != ''
  AND m.genres_json IS JSON ARRAY
  AND TRIM(je.value) != ''
ON CONFLICT DO NOTHING;
