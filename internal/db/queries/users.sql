-- User accounts.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE users)
-- + migrations/sqlite/034_user_profiles.sql (parent_user_id, pin_hash,
-- max_content_rating, password_change_required).

-- name: GetUserByID :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at
FROM users
WHERE id = ?;

-- name: GetUserByUsername :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at
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
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at
FROM users
ORDER BY username
LIMIT ? OFFSET ?;

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, role = ?, is_active = ? WHERE id = ?;

-- name: UpdateUserRole :exec
-- Promote / demote between user and admin. Caller-side gate keeps
-- the primary admin (oldest role=admin row) immutable.
UPDATE users SET role = ? WHERE id = ?;

-- name: UpdateUserActive :exec
-- Soft-disable a user without deleting. Login rejects on
-- is_active=false; the row + every per-user table stays intact so
-- re-enabling is a one-flag flip.
UPDATE users SET is_active = ? WHERE id = ?;

-- name: GetPrimaryAdminID :one
-- Returns the user_id of the oldest admin row. The primary admin
-- is identified positionally, not by an explicit flag, so a fresh
-- DB starts with the setup-wizard user as primary automatically.
-- Empty result when no admin exists yet (cold-start install).
SELECT id FROM users
WHERE role = 'admin' AND parent_user_id IS NULL
ORDER BY created_at ASC
LIMIT 1;

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

-- name: UpdateUserAccessExpiresAt :exec
-- NULL = no expiry (permanent). Non-null = JWT middleware + login
-- reject after this timestamp. Lazy: no background job needed.
UPDATE users SET access_expires_at = ? WHERE id = ?;

-- name: ListProfilesForOwner :many
-- Returns the parent account row plus every profile that hangs off
-- it, ordered by the parent first, then profiles alphabetically.
-- Drives both the post-login "Who's watching?" payload and the admin
-- profile list under a user. The username column is unique, so
-- profiles synthesise theirs as "<parent.username>:<display_name>"
-- via the handler — we don't expose them for login anyway.
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at
FROM users
WHERE id = ? OR parent_user_id = ?
-- ASC is redundant for collation but works around a sqlc 1.31.x
-- bug that truncates `COLLATE NOCASE;` (with the trailing semicolon
-- directly after) to `COLLATE NOCA` in the generated Go string —
-- which then errors at runtime ("no such collation sequence: NOCA").
-- The other queries in this codebase that use COLLATE happen to
-- carry ASC/DESC and render fine. See git history of this file
-- for the diagnosis.
ORDER BY parent_user_id IS NOT NULL, display_name COLLATE NOCASE ASC;
