-- Library management (media libraries, paths, access control).
--
-- Tables: libraries, library_paths, library_access.

-- name: InsertLibrary :exec
INSERT INTO libraries (id, name, content_type, scan_mode, scan_interval,
    m3u_url, epg_url, refresh_interval, language_filter, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertLibraryPath :exec
INSERT INTO library_paths (library_id, path) VALUES (?, ?);

-- name: GetLibraryByID :one
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter,
       created_at, updated_at
FROM libraries
WHERE id = ?;

-- name: ListLibraries :many
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter,
       created_at, updated_at
FROM libraries
ORDER BY name;

-- name: UpdateLibrary :execrows
UPDATE libraries SET name = ?, content_type = ?, scan_mode = ?, scan_interval = ?,
       m3u_url = ?, epg_url = ?, refresh_interval = ?, language_filter = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteLibrary :execrows
DELETE FROM libraries WHERE id = ?;

-- name: ListPathsByLibrary :many
SELECT path FROM library_paths WHERE library_id = ? ORDER BY path;

-- name: ListAllPaths :many
SELECT library_id, path FROM library_paths ORDER BY library_id, path;

-- name: DeletePathsByLibrary :exec
DELETE FROM library_paths WHERE library_id = ?;

-- name: GrantLibraryAccess :exec
INSERT OR IGNORE INTO library_access (user_id, library_id) VALUES (?, ?);

-- name: RevokeLibraryAccess :exec
DELETE FROM library_access WHERE user_id = ? AND library_id = ?;

-- name: ListLibrariesForUser :many
SELECT l.id, l.name, l.content_type, l.scan_mode,
       COALESCE(l.scan_interval, '6h') AS scan_interval,
       COALESCE(l.m3u_url, '') AS m3u_url,
       COALESCE(l.epg_url, '') AS epg_url,
       COALESCE(l.refresh_interval, '24h') AS refresh_interval,
       l.language_filter,
       l.created_at, l.updated_at
FROM libraries l
LEFT JOIN library_access la ON la.library_id = l.id AND la.user_id = ?
WHERE la.user_id IS NOT NULL
   OR NOT EXISTS (SELECT 1 FROM library_access WHERE library_id = l.id)
ORDER BY l.name;
