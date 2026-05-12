-- Studios (production companies + TV networks).
--
-- Schema: migrations/sqlite/032_studios.sql.
-- The slug is URL-safe; ListItemsForStudio is intentionally raw SQL
-- (lives in studio_repository.go) because the trailing ORDER BY hits
-- the sqlc v1.31.1 parser truncation we already work around for
-- ListGenres + GetItemIDByExternalID.

-- name: GetStudioBySlug :one
SELECT id, tmdb_id, name, slug, logo_url, created_at
FROM studios
WHERE slug = $1;

-- name: GetStudioByTMDBID :one
SELECT id, tmdb_id, name, slug, logo_url, created_at
FROM studios
WHERE tmdb_id = $1;

-- name: UpsertStudio :exec
-- The scanner calls this every time it processes a movie/series with
-- a TMDb production company match. Conflict on tmdb_id keeps a single
-- row per upstream studio even when the slug recipe would diverge
-- (e.g. "Lucasfilm" vs "Lucasfilm Ltd."); conflict on slug catches
-- the rare case of a studio with no tmdb_id (legacy backfill rows).
-- We refresh the logo_url on conflict so a re-scan with a richer
-- TMDb response upgrades the brand mark, but we keep `name` and
-- `slug` stable so the URL stays valid even if TMDb renames.
INSERT INTO studios (id, tmdb_id, name, slug, logo_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(tmdb_id) DO UPDATE SET
    logo_url = excluded.logo_url
WHERE excluded.logo_url <> '';

-- name: UpsertStudioBySlug :exec
-- Fallback when the scanner has no tmdb_id (legacy items, providers
-- other than TMDb). The slug is the dedupe key.
INSERT INTO studios (id, tmdb_id, name, slug, logo_url)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(slug) DO UPDATE SET
    logo_url = excluded.logo_url
WHERE excluded.logo_url <> '';

-- name: ListStudios :many
-- Browse-page listing: every studio that has at least one item linked
-- to it, ordered by item count desc. The COUNT subquery is fine
-- (one studio thousand-items at most) and keeps the index touched
-- to (studio_id) which we already created in 032_studios.sql.
SELECT
    s.id,
    s.name,
    s.slug,
    s.logo_url,
    (SELECT COUNT(*) FROM metadata m WHERE m.studio_id = s.id) AS item_count
FROM studios s
WHERE EXISTS (SELECT 1 FROM metadata m WHERE m.studio_id = s.id)
ORDER BY item_count DESC, s.name ASC;

-- ListItemsForStudio is intentionally implemented as raw SQL in the
-- repository (studio_repository.go::ListItemsForStudio) — sqlc v1.31.1
-- truncates the trailing identifier of the final query in a file.
-- Same workaround pattern as item_value_repository.go::ListGenres.
