-- Items (movies, series, seasons, episodes, albums, etc.).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE items).
-- NOTE: List and LatestItems use dynamic WHERE/FTS/cursor and stay raw SQL.

-- name: CreateItem :exec
INSERT INTO items (id, library_id, parent_id, type, title, sort_title, original_title,
    year, path, size, duration_ticks, container, fingerprint, season_number, episode_number,
    community_rating, content_rating, premiere_date, added_at, updated_at, is_available)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21);

-- name: GetItemByID :one
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, fingerprint, season_number,
       episode_number, community_rating, content_rating, premiere_date,
       added_at, updated_at, is_available
FROM items
WHERE id = $1;

-- name: GetItemByPath :one
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, fingerprint, season_number,
       episode_number, community_rating, content_rating, premiere_date,
       added_at, updated_at, is_available
FROM items
WHERE path = $1;

-- name: UpdateItem :execrows
UPDATE items SET title = $1, sort_title = $2, original_title = $3,
       year = $4, size = $5, duration_ticks = $6, container = $7,
       fingerprint = $8, season_number = $9, episode_number = $10,
       community_rating = $11, content_rating = $12,
       premiere_date = $13, updated_at = $14, is_available = $15
WHERE id = $16;

-- name: DeleteItem :execrows
DELETE FROM items WHERE id = $1;

-- name: DeleteItemsByLibrary :exec
DELETE FROM items WHERE library_id = $1;

-- name: CountItemsByLibrary :one
SELECT COUNT(*) AS cnt FROM items WHERE library_id = $1;

-- name: GetItemChildren :many
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, season_number, episode_number,
       community_rating, added_at, updated_at, is_available
FROM items
WHERE parent_id = $1
ORDER BY COALESCE(season_number, 0), COALESCE(episode_number, 0), sort_title;

-- name: SumItemSizesByLibrary :many
-- Suma el peso total en bytes y cuenta los ficheros reales por
-- biblioteca. Ver hermano SQLite para rationale.
SELECT library_id, COALESCE(SUM(size), 0)::BIGINT AS total_bytes, COUNT(*)::BIGINT AS file_count
FROM items
WHERE size > 0
GROUP BY library_id;
