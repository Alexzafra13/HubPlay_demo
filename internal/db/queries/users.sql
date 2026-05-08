-- User accounts.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE users)
-- + migrations/sqlite/034_user_profiles.sql (parent_user_id, pin_hash,
-- max_content_rating, password_change_required).

-- name: GetUserByID :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required
FROM users
WHERE id = ?;

-- name: GetUserByUsername :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required
FROM users
WHERE username = ?;

-- name: CreateUser :exec
INSERT INTO users (id, username, display_name, password_hash, role, created_at,
                   parent_user_id, password_change_required)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = ? WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) AS cnt FROM users;

-- name: ListUsers :many
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required
FROM users
ORDER BY username
LIMIT ? OFFSET ?;

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, role = ?, is_active = ? WHERE id = ?;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;

-- name: UpdateUserPassword :exec
-- Used by both the admin reset-password endpoint and the user's own
-- change-password flow. Resetting must clear the must-change flag so
-- the user isn't bounced back to the change screen forever; setting
-- a fresh password from the admin path SETs it so the new password
-- triggers a forced change on first login.
UPDATE users SET password_hash = ?, password_change_required = ? WHERE id = ?;

-- name: UpdateUserPIN :exec
-- pin_hash NULL clears the PIN; non-null sets it. The handler always
-- bcrypt-hashes the value before reaching the repo.
UPDATE users SET pin_hash = ? WHERE id = ?;

-- name: UpdateUserMaxContentRating :exec
-- max_content_rating NULL means "no restriction". Empty string is
-- normalised to NULL by the handler so callers don't have to choose.
UPDATE users SET max_content_rating = ? WHERE id = ?;

-- name: ListProfilesForOwner :many
-- Returns the parent account row plus every profile that hangs off
-- it, ordered by the parent first, then profiles alphabetically.
-- Drives both the post-login "Who's watching?" payload and the admin
-- profile list under a user. The username column is unique, so
-- profiles synthesise theirs as "<parent.username>:<display_name>"
-- via the handler — we don't expose them for login anyway.
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required
FROM users
WHERE id = ? OR parent_user_id = ?
ORDER BY parent_user_id IS NOT NULL, display_name COLLATE NOCASE;
