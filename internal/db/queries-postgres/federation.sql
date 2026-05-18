-- Federation: server identity, peers, invites, library shares, audit
-- log, item-cache. Schema: migrations/sqlite/020-023, 027.
--
-- Adapter wrapping these queries lives in
-- internal/db/federation_repository.go and is the public surface the
-- federation package consumes.

-- ============================================================
-- server identity (one row, enforced by CHECK(id=1))
-- ============================================================

-- name: GetServerIdentity :one
SELECT server_uuid, name, private_key, public_key, created_at, rotated_at,
       avatar_color, avatar_image_path
FROM server_identity
WHERE id = 1;

-- name: InsertServerIdentity :exec
INSERT INTO server_identity
    (id, server_uuid, name, private_key, public_key, created_at)
VALUES (1, $1, $2, $3, $4, $5);

-- name: UpdateServerIdentityProfile :exec
-- Personalizacion editable del servidor: nombre visible para peers
-- y color hex de fallback para el avatar. La foto se actualiza por
-- separado para no reenviar los otros campos en cada upload.
UPDATE server_identity
SET name = $1, avatar_color = $2
WHERE id = 1;

-- name: SetServerAvatarPath :exec
UPDATE server_identity
SET avatar_image_path = $1
WHERE id = 1;

-- ============================================================
-- invites
-- ============================================================

-- name: InsertInvite :exec
INSERT INTO federation_invites
    (id, code, created_by_user_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetInviteByCode :one
SELECT id, code, created_by_user_id, created_at, expires_at,
       accepted_by_peer_id, accepted_at
FROM federation_invites
WHERE code = $1;

-- name: MarkInviteUsed :exec
UPDATE federation_invites
SET accepted_by_peer_id = $1, accepted_at = $2
WHERE id = $3 AND accepted_at IS NULL;

-- name: ListActiveInvites :many
SELECT id, code, created_by_user_id, created_at, expires_at,
       accepted_by_peer_id, accepted_at
FROM federation_invites
WHERE accepted_at IS NULL AND expires_at > $1
ORDER BY created_at DESC;

-- ============================================================
-- peers
-- ============================================================

-- name: InsertPeer :exec
INSERT INTO federation_peers
    (id, server_uuid, name, base_url, public_key, status, created_at, paired_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdatePeerPaired :exec
UPDATE federation_peers
SET status = 'paired', paired_at = $1
WHERE id = $2;

-- name: UpdatePeerRevoked :execrows
UPDATE federation_peers
SET status = 'revoked', revoked_at = $1
WHERE id = $2 AND status != 'revoked';

-- name: UpdatePeerLastSeen :exec
UPDATE federation_peers
SET last_seen_at = $1, last_seen_status_code = $2
WHERE id = $3;

-- name: GetPeerByID :one
SELECT id, server_uuid, name, base_url, public_key, status,
       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
FROM federation_peers
WHERE id = $1;

-- name: GetPeerByServerUUID :one
SELECT id, server_uuid, name, base_url, public_key, status,
       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
FROM federation_peers
WHERE server_uuid = $1;

-- name: ListPeers :many
SELECT id, server_uuid, name, base_url, public_key, status,
       created_at, paired_at, last_seen_at, last_seen_status_code, revoked_at
FROM federation_peers
ORDER BY created_at DESC;

-- ============================================================
-- audit log
-- ============================================================

-- name: InsertFederationAuditEntry :exec
INSERT INTO federation_audit_log
    (peer_id, remote_user_id, method, endpoint, status_code,
     bytes_out, item_id, session_id, error_kind, duration_ms,
     occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ListFederationAuditEntries :many
SELECT peer_id, remote_user_id, method, endpoint, status_code,
       bytes_out, item_id, session_id, error_kind, duration_ms,
       occurred_at
FROM federation_audit_log
WHERE peer_id = $1
ORDER BY occurred_at DESC
LIMIT $2;

-- name: PruneFederationAuditBefore :execrows
DELETE FROM federation_audit_log
WHERE occurred_at < $1;

-- ============================================================
-- library shares
-- ============================================================

-- name: UpsertLibraryShare :exec
INSERT INTO federation_library_shares
    (id, peer_id, library_id, can_browse, can_play, can_download,
     can_livetv, extra_scopes, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT(peer_id, library_id) DO UPDATE SET
    can_browse   = excluded.can_browse,
    can_play     = excluded.can_play,
    can_download = excluded.can_download,
    can_livetv   = excluded.can_livetv,
    extra_scopes = excluded.extra_scopes;

-- name: DeleteLibraryShare :exec
DELETE FROM federation_library_shares
WHERE peer_id = $1 AND id = $2;

-- name: GetLibraryShare :one
SELECT id, peer_id, library_id, can_browse, can_play,
       can_download, can_livetv, extra_scopes, created_by, created_at
FROM federation_library_shares
WHERE peer_id = $1 AND library_id = $2;

-- name: ListSharesByPeer :many
SELECT id, peer_id, library_id, can_browse, can_play,
       can_download, can_livetv, extra_scopes, created_by, created_at
FROM federation_library_shares
WHERE peer_id = $1
ORDER BY created_at DESC;

-- name: ListSharedLibrariesForPeer :many
SELECT l.id, l.name, l.content_type,
       s.can_browse, s.can_play, s.can_download, s.can_livetv
FROM federation_library_shares s
JOIN libraries l ON l.id = s.library_id
WHERE s.peer_id = $1
ORDER BY LOWER(l.name) ASC;

-- name: CountSharedItems :one
SELECT COUNT(*)
FROM items i
JOIN federation_library_shares s ON s.library_id = i.library_id
WHERE i.library_id = $1 AND s.peer_id = $2 AND s.can_browse
  AND i.parent_id IS NULL;

-- name: ListSharedItems :many
-- Overview lives in the metadata sidecar (LEFT JOIN so items without
-- metadata still surface with empty overview). HasPoster is an EXISTS
-- subquery against the images table so the listing path does not
-- need to pull the image id; the actual bytes flow through
-- /peer/items/{id}/poster on demand.
SELECT i.id, i.type, i.title,
       COALESCE(i.year, 0) AS year,
       COALESCE(m.overview, '') AS overview,
       EXISTS (
         SELECT 1 FROM images img
          WHERE img.item_id = i.id
            AND img.type = 'primary'
            AND img.is_primary
       ) AS has_poster
FROM items i
JOIN federation_library_shares s ON s.library_id = i.library_id
LEFT JOIN metadata m ON m.item_id = i.id
WHERE i.library_id = $1 AND s.peer_id = $2 AND s.can_browse
  AND i.parent_id IS NULL
ORDER BY LOWER(i.sort_title) ASC
LIMIT $3 OFFSET $4;

-- NOTE: SearchSharedItems is implemented as raw SQL in
-- federation_repository.go because sqlc does not parse FTS5 virtual
-- tables (items_fts MATCH $5). Same precedent as item_repository.go's
-- List path.

-- name: ListRecentSharedItems :many
-- Most recently added items across every library shared with `peerID`
-- (CanBrowse gate). Powers the consumer-side "Recently added on
-- peers" rail: each paired peer answers with its top-N freshest
-- titles, the consumer fan-out merges them. library_id is selected
-- so the consumer can route a click into
-- /peers/{peerID}/libraries/{libraryID}/items/{id} (same shape as
-- the search hits).
SELECT i.id, i.type, i.title,
       COALESCE(i.year, 0) AS year,
       COALESCE(m.overview, '') AS overview,
       EXISTS (
         SELECT 1 FROM images img
          WHERE img.item_id = i.id
            AND img.type = 'primary'
            AND img.is_primary
       ) AS has_poster,
       i.library_id
FROM items i
JOIN federation_library_shares s ON s.library_id = i.library_id
LEFT JOIN metadata m ON m.item_id = i.id
WHERE s.peer_id = $1 AND s.can_browse
  AND i.parent_id IS NULL
ORDER BY i.added_at DESC
LIMIT $2;

-- ============================================================
-- catalog cache (Phase 4 + 027)
-- ============================================================

-- name: DeleteCachedItemsForLibrary :exec
DELETE FROM federation_item_cache
WHERE peer_id = $1 AND library_id = $2;

-- name: InsertCachedItem :exec
INSERT INTO federation_item_cache
    (peer_id, library_id, remote_id, type, title, year, overview, has_poster, cached_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CountAndNewestCachedItems :one
SELECT COUNT(*) AS total, MAX(cached_at) AS newest_cached_at
FROM federation_item_cache
WHERE peer_id = $1 AND library_id = $2;

-- name: ListCachedItems :many
SELECT remote_id, type, title,
       COALESCE(year, 0) AS year,
       COALESCE(overview, '') AS overview,
       has_poster
FROM federation_item_cache
WHERE peer_id = $1 AND library_id = $2
ORDER BY LOWER(title) ASC
LIMIT $3 OFFSET $4;

-- ============================================================
-- federation_progress (028) -- cross-peer Continue Watching
-- ============================================================

-- name: UpsertFederationProgress :exec
INSERT INTO federation_progress
    (user_id, peer_id, remote_item_id, position_ticks, duration_ticks,
     completed, last_played_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT(user_id, peer_id, remote_item_id) DO UPDATE SET
    position_ticks = excluded.position_ticks,
    -- Only overwrite duration when the caller actually knows it.
    -- The player learns duration from the manifest after a few
    -- segments; the first save typically arrives before that, so
    -- the first row may be inserted with duration_ticks = 0 and
    -- later upserts replace it with the real value.
    duration_ticks = CASE WHEN excluded.duration_ticks > 0
                          THEN excluded.duration_ticks
                          ELSE federation_progress.duration_ticks END,
    completed = excluded.completed,
    last_played_at = excluded.last_played_at,
    updated_at = excluded.updated_at;

-- name: GetFederationProgress :one
SELECT user_id, peer_id, remote_item_id, position_ticks, duration_ticks,
       completed, last_played_at, updated_at
FROM federation_progress
WHERE user_id = $1 AND peer_id = $2 AND remote_item_id = $3;

-- name: DeleteFederationProgress :exec
DELETE FROM federation_progress
WHERE user_id = $1 AND peer_id = $2 AND remote_item_id = $3;

-- name: ListFederationContinueWatching :many
-- Cross-peer Continue Watching rail. Only rows that look genuinely
-- "in progress" are returned: not completed, position > 0, and (when
-- duration is known) less than 90 percent played -- mirrors the local
-- ContinueWatching filter so peer rows behave the same way the user
-- already expects from local rows. Joins the catalog cache for
-- title / poster availability so the rail can render without a
-- per-row hop. Rows whose cache entry has been evicted (peer
-- catalog refreshed against newer state) are dropped from the rail
-- rather than rendered title-less.
SELECT fp.peer_id, fp.remote_item_id, fp.position_ticks,
       fp.duration_ticks, fp.last_played_at,
       c.library_id, c.type, c.title,
       COALESCE(c.year, 0) AS year,
       COALESCE(c.overview, '') AS overview,
       c.has_poster,
       p.name AS peer_name
FROM federation_progress fp
JOIN federation_item_cache c
  ON c.peer_id = fp.peer_id AND c.remote_id = fp.remote_item_id
JOIN federation_peers p
  ON p.id = fp.peer_id
WHERE fp.user_id = $1
  AND NOT fp.completed
  AND fp.position_ticks > 0
  AND p.status = 'paired'
  AND NOT (
    fp.duration_ticks > 0
    AND fp.position_ticks * 100 >= fp.duration_ticks * 90
  )
ORDER BY fp.last_played_at DESC
LIMIT $2;
