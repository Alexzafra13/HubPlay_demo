-- Provider configurations (TMDb, Fanart, OpenSubtitles, ...).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE providers).
-- Runtime registry + api-key sourcing lives in internal/provider/manager.go.

-- name: UpsertProvider :exec
INSERT INTO providers (
    name, type, version, status, priority,
    config_json, api_key, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT(name) DO UPDATE SET
    type = excluded.type,
    version = excluded.version,
    status = excluded.status,
    priority = excluded.priority,
    config_json = excluded.config_json,
    api_key = excluded.api_key,
    updated_at = excluded.updated_at;

-- name: GetProvider :one
SELECT name, type, version, status, priority,
       config_json, api_key, created_at, updated_at
FROM providers
WHERE name = $1;

-- name: ListActiveProviders :many
SELECT name, type, version, status, priority,
       config_json, api_key, created_at, updated_at
FROM providers
WHERE status = 'active'
ORDER BY priority, name;

-- name: ListProviders :many
SELECT name, type, version, status, priority,
       config_json, api_key, created_at, updated_at
FROM providers
ORDER BY priority, name;

-- name: ListProvidersByType :many
SELECT name, type, version, status, priority,
       config_json, api_key, created_at, updated_at
FROM providers
WHERE type = $1 AND status = 'active'
ORDER BY priority, name;

-- name: SetProviderStatus :execrows
UPDATE providers
SET status = $1, updated_at = $2
WHERE name = $3;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE name = $1;
