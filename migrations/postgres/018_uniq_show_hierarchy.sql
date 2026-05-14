-- +goose Up
-- Stop series + season rows from duplicating across re-scans. See
-- SQLite sibling for the full rationale (canonical id = MIN(id) per
-- group, two-step re-parent + delete, partial unique indexes as
-- structural guard).
--
-- Translation notes:
--   • The subselect / GROUP BY / MIN(id) syntax used here is
--     standard SQL and works identically in both engines.
--   • The implicit cross-join `FROM items canon, items dup` is
--     valid in Postgres; semantics unchanged.
--   • Partial UNIQUE indexes with WHERE clauses work identically.
--   • One subtle difference: Postgres is stricter about ambiguous
--     column references in correlated subqueries. The original
--     SQLite version was OK; the Postgres translation here adds
--     no qualifiers because all column references are already
--     unambiguous via the table aliases.

-- ── Step 1a: re-parent children of non-canonical SERIES dups to
-- the canonical one.
UPDATE items
SET parent_id = (
    SELECT MIN(canon.id)
    FROM items canon, items dup
    WHERE dup.id = items.parent_id
      AND dup.type = 'series'
      AND canon.type = 'series'
      AND canon.library_id = dup.library_id
      AND canon.title = dup.title
)
WHERE parent_id IN (
    SELECT id FROM items
    WHERE type = 'series'
      AND id NOT IN (
          SELECT MIN(id) FROM items
          WHERE type = 'series'
          GROUP BY library_id, title
      )
);

-- ── Step 1b: drop the non-canonical SERIES dups.
DELETE FROM items
WHERE type = 'series'
  AND id NOT IN (
      SELECT MIN(id) FROM items
      WHERE type = 'series'
      GROUP BY library_id, title
  );

-- ── Step 1c: re-parent episodes of non-canonical SEASON dups to
-- the canonical season.
UPDATE items
SET parent_id = (
    SELECT MIN(canon.id)
    FROM items canon, items dup
    WHERE dup.id = items.parent_id
      AND dup.type = 'season'
      AND dup.season_number IS NOT NULL
      AND canon.type = 'season'
      AND canon.parent_id = dup.parent_id
      AND canon.season_number = dup.season_number
)
WHERE parent_id IN (
    SELECT id FROM items
    WHERE type = 'season'
      AND season_number IS NOT NULL
      AND id NOT IN (
          SELECT MIN(id) FROM items
          WHERE type = 'season' AND season_number IS NOT NULL
          GROUP BY parent_id, season_number
      )
);

-- ── Step 1d: drop the non-canonical SEASON dups.
DELETE FROM items
WHERE type = 'season'
  AND season_number IS NOT NULL
  AND id NOT IN (
      SELECT MIN(id) FROM items
      WHERE type = 'season' AND season_number IS NOT NULL
      GROUP BY parent_id, season_number
  );

-- ── Step 2: structural guards. Partial UNIQUE indexes so only
-- series + season rows participate.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_series_per_library
    ON items(library_id, title)
    WHERE type = 'series';

CREATE UNIQUE INDEX IF NOT EXISTS uniq_season_per_series
    ON items(parent_id, season_number)
    WHERE type = 'season' AND season_number IS NOT NULL;
