-- Extended metadata for items (overview, tagline, genres, etc.).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE metadata).
-- PK: item_id.
-- NOTE: Batch queries (GetOverviewBatch, GetMetadataBatch) use dynamic IN()
-- and remain as raw SQL in the repository adapter.

-- name: UpsertMetadata :exec
INSERT INTO metadata (item_id, overview, tagline, studio, genres_json, tags_json, trailer_key, trailer_site)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(item_id) DO UPDATE SET
    overview = excluded.overview,
    tagline = excluded.tagline,
    studio = excluded.studio,
    genres_json = excluded.genres_json,
    tags_json = excluded.tags_json,
    trailer_key = excluded.trailer_key,
    trailer_site = excluded.trailer_site;

-- name: GetMetadataByItemID :one
SELECT item_id, COALESCE(overview, '') AS overview, COALESCE(tagline, '') AS tagline,
       COALESCE(studio, '') AS studio, COALESCE(genres_json, '') AS genres_json,
       COALESCE(tags_json, '') AS tags_json,
       COALESCE(trailer_key, '') AS trailer_key,
       COALESCE(trailer_site, '') AS trailer_site
FROM metadata
WHERE item_id = ?;

-- name: DeleteMetadata :exec
DELETE FROM metadata WHERE item_id = ?;
