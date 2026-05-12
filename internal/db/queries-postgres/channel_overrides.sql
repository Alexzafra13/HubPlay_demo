-- Manual channel edits keyed by stream URL so they survive an M3U
-- refresh (channel UUIDs are regenerated on every import).
--
-- Table schema: migrations/sqlite/009_channel_overrides.sql.
-- Composite PK: (library_id, stream_url).

-- name: UpsertChannelOverride :exec
INSERT INTO channel_overrides (library_id, stream_url, tvg_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(library_id, stream_url) DO UPDATE SET
   tvg_id     = excluded.tvg_id,
   updated_at = excluded.updated_at;

-- name: DeleteChannelOverride :exec
DELETE FROM channel_overrides WHERE library_id = $1 AND stream_url = $2;

-- name: GetChannelOverride :one
SELECT library_id, stream_url, tvg_id, created_at, updated_at
FROM channel_overrides
WHERE library_id = $1 AND stream_url = $2;

-- name: ListChannelOverridesByLibrary :many
SELECT stream_url, tvg_id
FROM channel_overrides
WHERE library_id = $1;

-- ApplyChannelOverride is the per-row hook the post-import pass uses.
-- Returns rows affected so the caller can count "actually applied"
-- vs orphan (stream_url no longer in playlist).

-- name: ApplyChannelOverride :execrows
UPDATE channels SET tvg_id = $1
WHERE library_id = $2 AND stream_url = $3;
