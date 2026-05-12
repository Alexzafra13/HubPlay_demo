-- +goose Up
-- Studios (production companies + TV networks) as first-class
-- entities. See SQLite sibling for the full design rationale.
--
-- Postgres translation notes:
--   • DATETIME → TIMESTAMPTZ
--   • INSERT OR IGNORE → INSERT … ON CONFLICT DO NOTHING
--   • The slug-builder REPLACE chain works identically in both
--     dialects.
--   • Partial index `WHERE studio_id IS NOT NULL` is identical.
--   • Correlated subquery in the SELECT list (the "any non-empty
--     logo_url" pick) is standard SQL; Postgres handles it.
--   • The LATERAL keyword is NOT needed here — the subquery is
--     a scalar correlated subquery, not a join.

CREATE TABLE studios (
    id          TEXT PRIMARY KEY,
    tmdb_id     INTEGER UNIQUE,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    logo_url    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_studios_slug ON studios(slug);
CREATE INDEX idx_studios_name ON studios(name);

ALTER TABLE metadata
    ADD COLUMN studio_id TEXT REFERENCES studios(id) ON DELETE SET NULL;

CREATE INDEX idx_metadata_studio_id ON metadata(studio_id)
    WHERE studio_id IS NOT NULL;

-- Backfill studios from existing free-form metadata.studio text.
-- Slug builder mirrors the Go-side slugify (lowercase, replace
-- common separators with '-', collapse). Pure SQL, same chain of
-- REPLACE calls used in the SQLite sibling.
INSERT INTO studios (id, tmdb_id, name, slug, logo_url)
SELECT
    'studio:' || lower(
        replace(replace(replace(replace(replace(replace(replace(replace(
            trim(m.studio),
            ' ', '-'), '.', ''), ',', ''), '&', 'and'),
            '/', '-'), '(', ''), ')', ''), '''', '')
    ) AS id,
    NULL AS tmdb_id,
    m.studio AS name,
    lower(
        replace(replace(replace(replace(replace(replace(replace(replace(
            trim(m.studio),
            ' ', '-'), '.', ''), ',', ''), '&', 'and'),
            '/', '-'), '(', ''), ')', ''), '''', '')
    ) AS slug,
    COALESCE(
        (SELECT studio_logo_url
         FROM metadata m2
         WHERE m2.studio = m.studio AND COALESCE(m2.studio_logo_url, '') <> ''
         LIMIT 1),
        ''
    ) AS logo_url
FROM metadata m
WHERE COALESCE(m.studio, '') <> ''
GROUP BY m.studio
ON CONFLICT DO NOTHING;

-- Wire each metadata row's studio_id to the freshly-inserted studio
-- row by matching on the same slug recipe.
UPDATE metadata
SET studio_id = (
    SELECT s.id FROM studios s
    WHERE s.slug = lower(
        replace(replace(replace(replace(replace(replace(replace(replace(
            trim(metadata.studio),
            ' ', '-'), '.', ''), ',', ''), '&', 'and'),
            '/', '-'), '(', ''), ')', ''), '''', '')
    )
)
WHERE COALESCE(metadata.studio, '') <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_metadata_studio_id;
ALTER TABLE metadata DROP COLUMN IF EXISTS studio_id;
DROP INDEX IF EXISTS idx_studios_name;
DROP INDEX IF EXISTS idx_studios_slug;
DROP TABLE IF EXISTS studios;
