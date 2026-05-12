-- Movie collections (sagas).
--
-- Schema: migrations/sqlite/033_collections.sql.
-- Keyed by tmdb_id (UNIQUE) so the same X-Men / MCU / Toy Story
-- saga collapses across every member movie regardless of name drift.

-- name: GetCollectionByID :one
SELECT id, tmdb_id, name, overview, poster_url, backdrop_url, created_at
FROM collections
WHERE id = $1;

-- name: GetCollectionByTMDBID :one
SELECT id, tmdb_id, name, overview, poster_url, backdrop_url, created_at
FROM collections
WHERE tmdb_id = $1;

-- UpsertCollection is intentionally implemented as raw SQL in the
-- repository (collection_repository.go::EnsureCollection) — the
-- ON CONFLICT clause uses CASE expressions to preserve non-empty
-- artwork on re-scan, and sqlc v1.31.1 truncates the trailing `END`
-- of the final query in a file. Same workaround pattern as
-- item_value_repository.go::ListGenres.

-- ListCollections + ListItemsForCollection are intentionally
-- implemented as raw SQL in the repository (collection_repository.go)
-- — sqlc v1.31.1 truncates the trailing identifier of the final
-- query in a file (here `ASC` becomes `A`, producing invalid SQL
-- that fails at runtime). Same workaround as
-- item_value_repository.go::ListGenres.
