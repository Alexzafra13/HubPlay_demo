-- External ID mappings (tmdb, imdb, tvdb, ...) for items.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE external_ids).
-- PK: (item_id, provider).

-- name: UpsertExternalID :exec
INSERT INTO external_ids (item_id, provider, external_id)
VALUES (?, ?, ?)
ON CONFLICT(item_id, provider) DO UPDATE SET external_id = excluded.external_id;

-- name: ListExternalIDsByItem :many
SELECT item_id, provider, external_id
FROM external_ids
WHERE item_id = ?;

-- name: GetExternalIDByProvider :one
SELECT item_id, provider, external_id
FROM external_ids
WHERE item_id = ? AND provider = ?;

-- name: CountExternalIDsByItem :one
SELECT COUNT(*) AS cnt
FROM external_ids
WHERE item_id = ?;
