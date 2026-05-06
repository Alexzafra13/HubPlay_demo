-- +goose Up
--
-- Studios (production companies + TV networks) as first-class entities.
--
-- Until now `metadata.studio` was a free-form TEXT column — fine for
-- rendering "Lucasfilm" inline, useless for "show me everything from
-- Marvel Studios" because the same logical studio drifts across rows
-- as "Lucasfilm" / "Lucasfilm Ltd." / "Lucasfilm Limited" depending on
-- which TMDb record we matched. This migration introduces a normalised
-- table so the studio mark on the detail page can deep-link to a
-- collection page (`/studios/<slug>`) the way Plex / Jellyfin surface
-- network attribution.
--
-- Movies use TMDb `production_companies[0]`, series fall through to
-- `networks[0]` (HBO, Disney+, …); both are merged into this table —
-- they are the same brand mark from the user's perspective.
--
-- The `slug` is the URL-safe form of the name (lowercase, dashed,
-- collapsed). UNIQUE so /studios/marvel-studios stays stable even when
-- two TMDb records share the same name (alias drift).
--
-- The `tmdb_id` UNIQUE constraint dedupes by external id when the
-- scanner has it — different TMDb records of "Lucasfilm" with the
-- same id 1 collapse into one studio row instead of fanning out.
CREATE TABLE studios (
    id          TEXT PRIMARY KEY,
    tmdb_id     INTEGER UNIQUE,         -- NULL when scanner had no provider match
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    logo_url    TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_studios_slug ON studios(slug);
CREATE INDEX idx_studios_name ON studios(name);

-- The link between an item's metadata row and the studio. Nullable
-- because legacy items scanned before this migration only have the
-- text name; the migration backfills the obvious matches but anything
-- the backfill can't resolve stays NULL until the user runs a
-- metadata refresh (which re-runs the scanner with the new logic).
ALTER TABLE metadata ADD COLUMN studio_id TEXT REFERENCES studios(id) ON DELETE SET NULL;

CREATE INDEX idx_metadata_studio_id ON metadata(studio_id) WHERE studio_id IS NOT NULL;

-- Backfill studios from the existing free-form `metadata.studio` text.
-- This is best-effort: it captures every distinct non-empty studio
-- name that exists today, generates a deterministic id + slug, and
-- copies the existing `studio_logo_url` so the new collection page
-- can render its hero without waiting for a re-scan.
--
-- The slug builder mirrors the Go-side `slugify` we'll use in the
-- scanner: lowercase, replace non-alphanumeric runs with '-', trim.
-- Pure SQL doesn't have regex, so we do it with a small chain of
-- REPLACE calls covering the common separators in studio names
-- (space, period, comma, ampersand, slash, parentheses). Anything
-- exotic falls through with the original char and the UNIQUE
-- constraint just rejects it; the scanner will get another shot
-- with the proper Go slugify on next pass.
INSERT OR IGNORE INTO studios (id, tmdb_id, name, slug, logo_url)
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
    -- Pick any non-empty logo_url for this studio name (different
    -- items might have or not have one; we want the first non-empty).
    COALESCE(
        (SELECT studio_logo_url
         FROM metadata m2
         WHERE m2.studio = m.studio AND COALESCE(m2.studio_logo_url, '') <> ''
         LIMIT 1),
        ''
    ) AS logo_url
FROM metadata m
WHERE COALESCE(m.studio, '') <> ''
GROUP BY m.studio;

-- Now wire each metadata row's studio_id to the freshly-inserted
-- studio row by matching on the same slug recipe. NULL stays NULL
-- when no studio name was set.
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
