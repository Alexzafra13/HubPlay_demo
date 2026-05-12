-- +goose Up

-- ─────────────────────────────────────────────────────────────────
-- Postgres replacement for SQLite's FTS5 search.
--
-- SQLite uses a virtual `items_fts` table with triggers that mirror
-- title/original_title into a contentless FTS index. Postgres has no
-- direct equivalent — the canonical pattern is:
--
--   1. Add a tsvector column on `items` itself.
--   2. GIN index over that column for fast `@@` matches.
--   3. Trigger that rebuilds the tsvector on INSERT / UPDATE of
--      title or original_title.
--
-- The search vector uses the 'simple' dictionary so accent-insensitive
-- matching falls back to user-side normalisation (the SQLite version
-- used `remove_diacritics 2` for the same reason). When we
-- eventually want stemming / language-aware ranking, switch to
-- 'spanish' / 'english' here and load the matching dictionaries.
--
-- Backfill at the bottom populates existing rows from the items
-- already in the database. Idempotent — runs once per upgrade.
-- ─────────────────────────────────────────────────────────────────

ALTER TABLE items ADD COLUMN search_vector tsvector;
CREATE INDEX idx_items_fts ON items USING GIN(search_vector);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION items_search_vector_refresh() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        to_tsvector('simple',
            COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.original_title, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER items_search_vector_insert
    BEFORE INSERT ON items
    FOR EACH ROW
    EXECUTE FUNCTION items_search_vector_refresh();

CREATE TRIGGER items_search_vector_update
    BEFORE UPDATE OF title, original_title ON items
    FOR EACH ROW
    EXECUTE FUNCTION items_search_vector_refresh();

-- Backfill existing rows (the trigger only fires on future writes).
UPDATE items
SET search_vector = to_tsvector('simple',
    COALESCE(title, '') || ' ' || COALESCE(original_title, ''));

-- +goose Down
DROP TRIGGER IF EXISTS items_search_vector_update ON items;
DROP TRIGGER IF EXISTS items_search_vector_insert ON items;
DROP FUNCTION IF EXISTS items_search_vector_refresh();
DROP INDEX IF EXISTS idx_items_fts;
ALTER TABLE items DROP COLUMN IF EXISTS search_vector;
