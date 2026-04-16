-- User accounts.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE users).

-- name: GetUserByID :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at
FROM users
WHERE id = ?;

-- name: GetUserByUsername :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at
FROM users
WHERE username = ?;

-- name: CreateUser :exec
INSERT INTO users (id, username, display_name, password_hash, role, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = ? WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) AS cnt FROM users;

-- name: ListUsers :many
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at
FROM users
ORDER BY username
LIMIT ? OFFSET ?;

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, role = ?, is_active = ? WHERE id = ?;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
