-- IPTV channels per library.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE channels).

-- name: CreateChannel :exec
INSERT INTO channels (id, library_id, name, number, group_name, logo_url,
    stream_url, tvg_id, language, country, is_active, added_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetChannelByID :one
SELECT id, library_id, name, number, COALESCE(group_name, '') AS group_name,
       COALESCE(logo_url, '') AS logo_url, stream_url, COALESCE(tvg_id, '') AS tvg_id,
       COALESCE(language, '') AS language, COALESCE(country, '') AS country,
       is_active, added_at
FROM channels
WHERE id = ?;

-- name: ListChannelsByLibrary :many
SELECT id, library_id, name, number, COALESCE(group_name, '') AS group_name,
       COALESCE(logo_url, '') AS logo_url, stream_url, COALESCE(tvg_id, '') AS tvg_id,
       COALESCE(language, '') AS language, COALESCE(country, '') AS country,
       is_active, added_at
FROM channels
WHERE library_id = ?
ORDER BY number, name;

-- name: ListActiveChannelsByLibrary :many
SELECT id, library_id, name, number, COALESCE(group_name, '') AS group_name,
       COALESCE(logo_url, '') AS logo_url, stream_url, COALESCE(tvg_id, '') AS tvg_id,
       COALESCE(language, '') AS language, COALESCE(country, '') AS country,
       is_active, added_at
FROM channels
WHERE library_id = ? AND is_active = 1
ORDER BY number, name;

-- name: DeleteChannelsByLibrary :exec
DELETE FROM channels WHERE library_id = ?;

-- name: SetChannelActive :execrows
UPDATE channels SET is_active = ? WHERE id = ?;

-- name: ListChannelGroups :many
SELECT DISTINCT group_name
FROM channels
WHERE library_id = ? AND group_name != ''
ORDER BY group_name;
