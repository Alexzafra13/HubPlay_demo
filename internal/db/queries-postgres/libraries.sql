-- Library management (media libraries, paths, access control).
--
-- Tables: libraries, library_paths, library_access.

-- name: InsertLibrary :exec
INSERT INTO libraries (id, name, content_type, scan_mode, scan_interval,
    m3u_url, epg_url, refresh_interval, language_filter, tls_insecure, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: InsertLibraryPath :exec
INSERT INTO library_paths (library_id, path) VALUES ($1, $2);

-- name: GetLibraryByID :one
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter, tls_insecure,
       created_at, updated_at
FROM libraries
WHERE id = $1;

-- name: ListLibraries :many
SELECT id, name, content_type, scan_mode, COALESCE(scan_interval, '6h') AS scan_interval,
       COALESCE(m3u_url, '') AS m3u_url, COALESCE(epg_url, '') AS epg_url,
       COALESCE(refresh_interval, '24h') AS refresh_interval,
       language_filter, tls_insecure,
       created_at, updated_at
FROM libraries
ORDER BY name;

-- name: UpdateLibrary :execrows
UPDATE libraries SET name = $1, content_type = $2, scan_mode = $3, scan_interval = $4,
       m3u_url = $5, epg_url = $6, refresh_interval = $7, language_filter = $8,
       tls_insecure = $9, updated_at = $10
WHERE id = $11;

-- name: DeleteLibrary :execrows
DELETE FROM libraries WHERE id = $1;

-- name: ListPathsByLibrary :many
SELECT path FROM library_paths WHERE library_id = $1 ORDER BY path;

-- name: ListAllPaths :many
SELECT library_id, path FROM library_paths ORDER BY library_id, path;

-- name: DeletePathsByLibrary :exec
DELETE FROM library_paths WHERE library_id = $1;

-- name: GrantLibraryAccess :exec
-- Postgres: INSERT OR IGNORE → ON CONFLICT DO NOTHING (sin target =
-- match cualquier constraint, mismo comportamiento que SQLite).
INSERT INTO library_access (user_id, library_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GrantPrimaryAdminLibraryAccess :exec
-- Grants the given library_id to the primary admin (oldest top-level
-- role=admin row, same definition as GetPrimaryAdminID). Idempotent
-- via ON CONFLICT DO NOTHING; no-op when no admin exists yet (cold-
-- start pre setup-wizard). Called from LibraryRepository.Create
-- inside the same tx so the invariant "primary admin sees every
-- library" holds for libraries created after migration 041.
INSERT INTO library_access (user_id, library_id)
SELECT u.id, $1
FROM users u
WHERE u.role = 'admin' AND u.parent_user_id IS NULL
ORDER BY u.created_at ASC
LIMIT 1
ON CONFLICT DO NOTHING;

-- name: RevokeLibraryAccess :exec
DELETE FROM library_access WHERE user_id = $1 AND library_id = $2;

-- name: ListLibraryAccessByUser :many
-- Returns library_ids the user_id has explicit grants for. Admin-only
-- surface (the user's library list goes through ListLibrariesForUser
-- which applies the profile-inheritance predicate). Callers pass the
-- top-level user id; for profile rows the caller must resolve to the
-- parent first. See ADR-014.
SELECT library_id FROM library_access WHERE user_id = $1 ORDER BY library_id;

-- ListLibrariesForUser: hand-rolled en library_repository.go (raw SQL),
-- no sqlc. El JOIN con COALESCE(parent_user_id, id) trips el parser
-- de sqlc 1.31.1 igual que las queries de federation con ORDER BY +
-- COLLATE. Documentado en docs/memory/architecture-decisions.md.
