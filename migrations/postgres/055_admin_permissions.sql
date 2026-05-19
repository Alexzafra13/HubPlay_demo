-- +goose Up
-- Permisos granulares para admins secundarios (PR3). Ver SQLite
-- sibling para el rationale completo del modelo y el backfill.
--
-- Diferencias vs SQLite:
--   - BOOLEAN nativo (no INTEGER 0/1)
--   - is_active es BOOLEAN ya en migración 001 del gemelo postgres
--   - el ORDER BY del backfill funciona idéntico

ALTER TABLE users ADD COLUMN is_owner             BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_manage_admins    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_manage_users     BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_manage_libraries BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_manage_iptv      BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_edit_metadata    BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_change_artwork   BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN can_view_audit       BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE users
SET can_manage_admins    = TRUE,
    can_manage_users     = TRUE,
    can_manage_libraries = TRUE,
    can_manage_iptv      = TRUE,
    can_edit_metadata    = TRUE,
    can_change_artwork   = TRUE,
    can_view_audit       = TRUE
WHERE role = 'admin' AND parent_user_id IS NULL;

UPDATE users SET is_owner = TRUE
WHERE id = (
    SELECT id FROM users
    WHERE role = 'admin' AND parent_user_id IS NULL
    ORDER BY created_at ASC
    LIMIT 1
);

CREATE UNIQUE INDEX idx_users_one_owner ON users(is_owner) WHERE is_owner = TRUE;
