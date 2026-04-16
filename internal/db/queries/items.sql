-- Items (movies, series, seasons, episodes, albums, etc.).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE items).
-- NOTE: List and LatestItems use dynamic WHERE/FTS/cursor and stay raw SQL.

-- name: CreateItem :exec
INSERT INTO items (id, library_id, parent_id, type, title, sort_title, original_title,
    year, path, size, duration_ticks, container, fingerprint, season_number, episode_number,
    community_rating, content_rating, premiere_date, added_at, updated_at, is_available)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetItemByID :one
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, fingerprint, season_number,
       episode_number, community_rating, content_rating, premiere_date,
       added_at, updated_at, is_available
FROM items
WHERE id = ?;

-- name: GetItemByPath :one
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, fingerprint, season_number,
       episode_number, community_rating, content_rating, premiere_date,
       added_at, updated_at, is_available
FROM items
WHERE path = ?;

-- name: UpdateItem :execrows
UPDATE items SET title = ?, sort_title = ?, original_title = ?,
       year = ?, size = ?, duration_ticks = ?, container = ?,
       fingerprint = ?, season_number = ?, episode_number = ?,
       community_rating = ?, content_rating = ?,
       premiere_date = ?, updated_at = ?, is_available = ?
WHERE id = ?;

-- name: DeleteItem :execrows
DELETE FROM items WHERE id = ?;

-- name: DeleteItemsByLibrary :exec
DELETE FROM items WHERE library_id = ?;

-- name: CountItemsByLibrary :one
SELECT COUNT(*) AS cnt FROM items WHERE library_id = ?;

-- name: GetItemChildren :many
SELECT id, library_id, parent_id, type, title, sort_title, original_title,
       year, path, size, duration_ticks, container, season_number, episode_number,
       community_rating, added_at, updated_at, is_available
FROM items
WHERE parent_id = ?
ORDER BY COALESCE(season_number, 0), COALESCE(episode_number, 0), sort_title;
