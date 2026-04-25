-- Per-library EPG source list (multi-provider XMLTV configuration).
--
-- Table schema: migrations/sqlite/007_library_epg_sources.sql.
-- PK: (id). UNIQUE on (library_id, url) so a duplicate add raises a
-- detectable constraint failure that the repo maps to
-- ErrEPGSourceAlreadyAttached.
--
-- COALESCE columns: catalog_id, last_refreshed_at, last_status and
-- last_error are all nullable in storage. Reading them back with
-- COALESCE keeps the consumer code free of NullString / NullTime
-- branching except for last_refreshed_at, which still needs zero-
-- value detection (we cannot tell "never refreshed" from an empty
-- string). Falling back to sql.NullTime there.

-- name: ListLibraryEPGSourcesByLibrary :many
SELECT id, library_id, COALESCE(catalog_id,'') AS catalog_id,
       url, priority, last_refreshed_at,
       COALESCE(last_status,'') AS last_status,
       COALESCE(last_error,'') AS last_error,
       last_program_count, last_channel_count, created_at
FROM library_epg_sources
WHERE library_id = ?
ORDER BY priority ASC, created_at ASC;

-- name: GetLibraryEPGSourceByID :one
SELECT id, library_id, COALESCE(catalog_id,'') AS catalog_id,
       url, priority, last_refreshed_at,
       COALESCE(last_status,'') AS last_status,
       COALESCE(last_error,'') AS last_error,
       last_program_count, last_channel_count, created_at
FROM library_epg_sources
WHERE id = ?;

-- name: NextLibraryEPGSourcePriority :one
-- The default-priority slot for a freshly-added source: one past the
-- current max, so it runs last and can be reordered from the UI.
-- COALESCE returns -1 for "no rows yet" so the repo can lift it to 0
-- without splitting NullInt branches.
SELECT COALESCE(MAX(priority), -1) AS max_priority
FROM library_epg_sources
WHERE library_id = ?;

-- name: CreateLibraryEPGSource :exec
INSERT INTO library_epg_sources
    (id, library_id, catalog_id, url, priority, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteLibraryEPGSource :exec
DELETE FROM library_epg_sources WHERE id = ?;

-- name: UpdateLibraryEPGSourcePriority :exec
UPDATE library_epg_sources SET priority = ?
WHERE id = ? AND library_id = ?;

-- name: RecordLibraryEPGSourceRefresh :exec
UPDATE library_epg_sources SET
    last_refreshed_at  = ?,
    last_status        = ?,
    last_error         = ?,
    last_program_count = ?,
    last_channel_count = ?
WHERE id = ?;
