-- User accounts.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE users)
-- + migrations/sqlite/034_user_profiles.sql (parent_user_id, pin_hash,
-- max_content_rating, password_change_required).

-- name: GetUserByID :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at, avatar_color,
       can_upload, upload_quota_bytes, upload_used_bytes,
       is_owner, can_manage_admins, can_manage_users, can_manage_libraries,
       can_manage_iptv, can_edit_metadata, can_change_artwork, can_view_audit
FROM users
WHERE id = $1;

-- name: GetUserByUsername :one
SELECT id, username, display_name, password_hash, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, max_sessions, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at, avatar_color,
       can_upload, upload_quota_bytes, upload_used_bytes,
       is_owner, can_manage_admins, can_manage_users, can_manage_libraries,
       can_manage_iptv, can_edit_metadata, can_change_artwork, can_view_audit
FROM users
WHERE username = $1;

-- name: CreateUser :exec
INSERT INTO users (id, username, display_name, password_hash, role, created_at,
                   parent_user_id, password_change_required)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = $1 WHERE id = $2;

-- name: CountUsers :one
SELECT COUNT(*) AS cnt FROM users;

-- name: ListUsers :many
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at, avatar_color,
       can_upload, upload_quota_bytes, upload_used_bytes,
       is_owner, can_manage_admins, can_manage_users, can_manage_libraries,
       can_manage_iptv, can_edit_metadata, can_change_artwork, can_view_audit
FROM users
ORDER BY username
LIMIT $1 OFFSET $2;

-- name: UpdateUser :exec
UPDATE users SET display_name = $1, role = $2, is_active = $3 WHERE id = $4;

-- name: UpdateUserDisplayName :exec
-- Per-field update so callers (renaming a profile, an admin
-- relabelling a user) don't need to round-trip the rest of the
-- mutable surface.
UPDATE users SET display_name = $1 WHERE id = $2;

-- name: UpdateUserRole :exec
-- Promote / demote between user and admin. Caller-side gate keeps
-- the primary admin (oldest role=admin row) immutable.
UPDATE users SET role = $1 WHERE id = $2;

-- name: UpdateUserActive :exec
-- Soft-disable a user without deleting. Login rejects on
-- is_active=false; the row + every per-user table stays intact so
-- re-enabling is a one-flag flip.
UPDATE users SET is_active = $1 WHERE id = $2;

-- name: GetPrimaryAdminID :one
-- Returns the user_id of the oldest admin row. The primary admin
-- is identified positionally, not by an explicit flag, so a fresh
-- DB starts with the setup-wizard user as primary automatically.
-- Empty result when no admin exists yet (cold-start install).
SELECT id FROM users
WHERE role = 'admin' AND parent_user_id IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: ListAdminIDs :many
-- Fan-out destination for cross-admin notifications.
SELECT id FROM users
WHERE role = 'admin' AND parent_user_id IS NULL AND is_active = TRUE
ORDER BY created_at ASC;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- name: UpdateUserPassword :exec
-- Used by both the admin reset-password endpoint and the user's own
-- change-password flow. Resetting must clear the must-change flag so
-- the user isn't bounced back to the change screen forever; setting
-- a fresh password from the admin path SETs it so the new password
-- triggers a forced change on first login.
UPDATE users SET password_hash = $1, password_change_required = $2 WHERE id = $3;

-- name: UpdateUserPIN :exec
-- pin_hash NULL clears the PIN; non-null sets it. The handler always
-- bcrypt-hashes the value before reaching the repo.
UPDATE users SET pin_hash = $1 WHERE id = $2;

-- name: UpdateUserMaxContentRating :exec
-- max_content_rating NULL means "no restriction". Empty string is
-- normalised to NULL by the handler so callers don't have to choose.
UPDATE users SET max_content_rating = $1 WHERE id = $2;

-- name: UpdateUserAccessExpiresAt :exec
-- NULL = no expiry (permanent). Non-null = JWT middleware + login
-- reject after this timestamp. Lazy: no background job needed.
UPDATE users SET access_expires_at = $1 WHERE id = $2;

-- name: UpdateUserAvatarColor :exec
-- avatar_color NULL = use the deterministic FNV-1a -> palette
-- fallback the frontend already has. Non-null = explicit hex
-- override. Service-layer enforces the value is in the known
-- palette (or empty) before reaching the repo.
UPDATE users SET avatar_color = $1 WHERE id = $2;

-- name: UpdateUserAvatarPath :exec
-- Sube/actualiza la ruta en disco del avatar subido por el usuario.
-- El path es relativo al directorio de avatares (config/avatars/<file>),
-- no absoluto, para que la migración del volumen no rompa nada.
UPDATE users SET avatar_path = $1 WHERE id = $2;

-- name: ClearUserAvatarPath :exec
-- Quita el avatar subido. El fichero en disco lo borra el service
-- antes de llamar; aquí sólo desreferenciamos.
UPDATE users SET avatar_path = NULL WHERE id = $1;

-- ListProfilesForOwner + upload mutations: hand-rolled in
-- user_repository.go, not sqlc. See SQLite sibling for the rationale
-- (sqlc 1.31.1 truncates the mutations the same way it did with
-- ListProfilesForOwner). SELECTs still flow through sqlc.
