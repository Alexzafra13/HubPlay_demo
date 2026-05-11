-- Library management (media libraries, paths, access control).
--
-- Tables: libraries, library_paths, library_access.

-- name: InsertLibrary :exec
INSERT INTO libraries (id, name, content_type, scan_mode, scan_interval,
    m3u_url, epg_url, refresh_interval, language_filter, tls_insecure, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertLibraryPath :exec
INSERT INTO library_paths (library_id, path) VALUES (?, ?);

-- name: GetLibraryByID :one
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter, tls_insecure,
       created_at, updated_at
FROM libraries
WHERE id = ?;

-- name: ListLibraries :many
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter, tls_insecure,
       created_at, updated_at
FROM libraries
ORDER BY name;

-- name: UpdateLibrary :execrows
UPDATE libraries SET name = ?, content_type = ?, scan_mode = ?, scan_interval = ?,
       m3u_url = ?, epg_url = ?, refresh_interval = ?, language_filter = ?,
       tls_insecure = ?, updated_at = ?
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

-- name: GrantPrimaryAdminLibraryAccess :exec
-- Grants the given library_id to the primary admin (oldest top-level
-- role=admin row, same definition as GetPrimaryAdminID). Idempotent
-- via INSERT OR IGNORE; no-op when no admin exists yet (cold-start
-- pre setup-wizard). Called from LibraryRepository.Create inside the
-- same tx so the invariant "primary admin sees every library" holds
-- for libraries created after migration 041.
INSERT OR IGNORE INTO library_access (user_id, library_id)
SELECT u.id, ?
FROM users u
WHERE u.role = 'admin' AND u.parent_user_id IS NULL
ORDER BY u.created_at ASC
LIMIT 1;

-- name: RevokeLibraryAccess :exec
DELETE FROM library_access WHERE user_id = ? AND library_id = ?;

-- name: ListLibraryAccessByUser :many
-- Returns library_ids the user_id has explicit grants for. Admin-only
-- surface (the user's library list goes through ListLibrariesForUser
-- which applies the profile-inheritance predicate). Callers pass the
-- top-level user id; for profile rows the caller must resolve to the
-- parent first. See ADR-014.
SELECT library_id FROM library_access WHERE user_id = ? ORDER BY library_id;

-- ListLibrariesForUser: hand-rolled en library_repository.go (raw SQL),
-- no sqlc. El JOIN con COALESCE(parent_user_id, id) trips el parser
-- de sqlc 1.31.1 igual que las queries de federation con ORDER BY +
-- COLLATE. Documentado en docs/memory/architecture-decisions.md.
