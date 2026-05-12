-- External ID mappings (tmdb, imdb, tvdb, ...) for items.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE external_ids).
-- PK: (item_id, provider).

-- name: UpsertExternalID :exec
INSERT INTO external_ids (item_id, provider, external_id)
VALUES ($1, $2, $3)
ON CONFLICT(item_id, provider) DO UPDATE SET external_id = excluded.external_id;

-- name: ListExternalIDsByItem :many
SELECT item_id, provider, external_id
FROM external_ids
WHERE item_id = $1;

-- name: GetExternalIDByProvider :one
SELECT item_id, provider, external_id
FROM external_ids
WHERE item_id = $1 AND provider = $2;

-- name: CountExternalIDsByItem :one
SELECT COUNT(*) AS cnt
FROM external_ids
WHERE item_id = $1;

-- GetItemIDByExternalID is intentionally implemented as raw SQL in the
-- repository (external_id_repository.go::GetItemIDByExternalID) — sqlc
-- v1.31.1 truncates the trailing identifier of the final query in a
-- file (here `LIMIT 1` becomes `LIMIT`, producing invalid SQL that
-- fails at runtime). Same workaround as item_values.sql::ListGenres.
