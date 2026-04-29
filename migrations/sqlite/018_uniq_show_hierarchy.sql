-- +goose Up
-- Stop series + season rows from duplicating across re-scans.
--
-- Why this exists: the `items` schema has only `id` (uuid) as PK, with
-- nothing structurally preventing two rows where (library_id, type='series',
-- title) match. The scanner has a per-scan in-memory cache to avoid that
-- (internal/scanner/show_hierarchy.go), but it's keyed on the EXACT
-- title string — case / whitespace / accent diffs between an existing
-- DB row and the freshly-parsed path slip past it as cache misses,
-- and a buggy older code path could create dups directly. Result:
-- "Breaking Bad" appearing twice in the series rail, with episodes
-- split across the two parent rows depending on which one each scan
-- happened to hit.
--
-- This migration does two things:
--
--   1. Cleans up any pre-existing duplicates (defensive — a fresh
--      install has nothing to do, an upgraded install gets dedup'd).
--      Children of non-canonical dups are re-parented to the canonical
--      one before the dup row is deleted, so no episode / season is
--      lost.
--
--   2. Adds partial UNIQUE indexes that make recurrence structurally
--      impossible:
--        - one series per (library_id, title)
--        - one season per (parent_series_id, season_number)
--      The scanner's idempotency now leans on these instead of trusting
--      its own cache; the repo's Create call surfaces a typed
--      ErrItemConflict when an insert would violate either constraint,
--      and ensureSeriesRow / ensureSeasonRow fall back to looking up
--      the existing id.
--
-- Choice of canonical row inside each dup group: lowest id (uuid lex
-- order). Arbitrary but deterministic and stable across re-runs of the
-- migration; child re-parenting is the part that matters for keeping
-- episodes attached, not which specific id wins.

-- ── Step 1a: re-parent children of non-canonical SERIES dups to the
-- canonical one. The subselect resolves "the other (library_id, title)
-- twin with the smallest id".
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

-- ── Step 1b: drop the non-canonical SERIES dups now that nothing
-- references them.
DELETE FROM items
WHERE type = 'series'
  AND id NOT IN (
      SELECT MIN(id) FROM items
      WHERE type = 'series'
      GROUP BY library_id, title
  );

-- ── Step 1c: re-parent episodes of non-canonical SEASON dups to the
-- canonical season. season_number IS NULL is a degenerate row that
-- shouldn't participate in the dedupe — handled by GROUP BY ignoring
-- NULL keys (SQLite drops the group, the row keeps its id).
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

-- ── Step 2: structural guards so the scanner can never recreate the
-- dup state. Partial indexes (filtered WHERE) so only series + season
-- rows participate; movies / episodes / audio are unaffected (multiple
-- episode files with the same number are legal and represent quality
-- variants).
CREATE UNIQUE INDEX IF NOT EXISTS uniq_series_per_library
    ON items(library_id, title)
    WHERE type = 'series';

CREATE UNIQUE INDEX IF NOT EXISTS uniq_season_per_series
    ON items(parent_id, season_number)
    WHERE type = 'season' AND season_number IS NOT NULL;
