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

-- name: SumItemSizesByLibrary :many
-- Suma el peso total en bytes y cuenta los ficheros reales por
-- biblioteca. Filtra `size > 0` para excluir nodos jerarquicos
-- (series, seasons) que no tienen fichero propio - solo los
-- "leaves" (movies, episodes, channels) tienen size>0. El indice
-- existente idx_items_library hace que el GROUP BY sea barato
-- incluso con millones de items.
SELECT library_id, CAST(COALESCE(SUM(size), 0) AS INTEGER) AS total_bytes, COUNT(*) AS file_count
FROM items
WHERE size > 0
GROUP BY library_id;
