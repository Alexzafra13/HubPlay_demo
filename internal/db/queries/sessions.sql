-- Auth sessions: one row per active login (refresh token lives here hashed).
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE sessions).
-- Rotation/max-sessions policy lives in internal/auth/service.go.

-- name: CreateSession :exec
INSERT INTO sessions (
    id, user_id, device_name, device_id, ip_address,
    refresh_token_hash, created_at, last_active_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionByRefreshTokenHash :one
SELECT id, user_id, device_name, device_id, ip_address,
       refresh_token_hash, created_at, last_active_at, expires_at
FROM sessions
WHERE refresh_token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteSessionByRefreshTokenHash :exec
DELETE FROM sessions WHERE refresh_token_hash = ?;

-- name: ListSessionsByUser :many
SELECT id, user_id, device_name, device_id, ip_address,
       refresh_token_hash, created_at, last_active_at, expires_at
FROM sessions
WHERE user_id = ?
ORDER BY last_active_at DESC;

-- name: CountSessionsByUser :one
SELECT COUNT(*) FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP;

-- name: DeleteOldestSessionByUser :exec
-- Subquery aliased to 's' because sqlc needs unambiguous column resolution
-- when the same table appears twice in scope.
DELETE FROM sessions WHERE id = (
    SELECT s.id FROM sessions s
    WHERE s.user_id = ?
    ORDER BY s.last_active_at ASC, s.created_at ASC
    LIMIT 1
);

-- name: DeleteAllSessionsByUser :execrows
DELETE FROM sessions WHERE user_id = ?;

-- name: UpdateSessionLastActive :exec
UPDATE sessions SET last_active_at = ? WHERE id = ?;
