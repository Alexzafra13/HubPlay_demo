-- +goose Up
--
-- 055_admin_permissions.sql — Permisos granulares para admins
-- secundarios (PR3 de la feature multi-admin).
--
-- Contexto: hasta aquí teníamos `role TEXT` con valor 'admin' o 'user'
-- y `RequireAdmin` middleware que lo gateaba todo en bloque. El
-- producto quiere ahora admins SECUNDARIOS con capacidades
-- restringidas (p.ej. "este admin sólo cambia carátulas; aquel sólo
-- da de alta usuarios"), sin tocar al admin RAÍZ que custodia la
-- instalación.
--
-- Modelo elegido — flags granulares en `users` (RBAC plano):
--
--   is_owner               admin raíz inmutable. Trigger garantiza
--                          que sólo existe UNO en toda la DB. Tiene
--                          TODO implícito; los demás flags se
--                          ignoran cuando is_owner=1.
--   can_manage_admins      otorgar/revocar flags a otros admins,
--                          promover/degradar role='admin'. SÓLO el
--                          owner lo tiene por defecto. Quien lo
--                          tenga no puede auto-otorgárselo (el
--                          chequeo lo hace el handler).
--   can_manage_users       alta/baja usuarios no-admin, reset pwd,
--                          library_access, perfiles.
--   can_manage_libraries   CRUD librerías, paths, trigger scans.
--   can_manage_iptv        M3U, EPG, canales, schedules IPTV.
--   can_edit_metadata      título, descripción, año, rating,
--                          identify TMDb, segmentos skip-intro.
--   can_change_artwork     poster, fondo, logo, image overrides.
--   can_view_audit         /admin/uploads/audit y similares
--                          (auditoría read-only).
--
-- can_upload ya existía (migración 053). Se mantiene tal cual — los
-- admins NO heredan can_upload automáticamente; subir media es una
-- acción del usuario, no del operador. Si quieres un admin que
-- también suba, le activas can_upload + cuota como a cualquier
-- usuario.
--
-- Backfill:
--   1. Todos los flags = 0 por default (cubre usuarios normales).
--   2. Cada `role='admin' AND parent_user_id IS NULL` recibe TODOS
--      los flags = 1 — preservamos exactamente el comportamiento
--      previo (admin = todopoderoso) post-migración.
--   3. El admin más antiguo (oldest created_at entre los admins
--      cuenta-titular) recibe is_owner = 1. Si no había admins
--      aún (instalación fresh), ningún owner; el setup wizard lo
--      marcará al crear el primer usuario.
--
-- Unicidad del owner: índice parcial UNIQUE WHERE is_owner=1
-- enforce que no haya >1. La transferencia se hace en una sola
-- transacción (clear old → set new) — SQLite valida la unicidad
-- entre statements y la app no expone un endpoint que pueda
-- dejar dos a la vez.

ALTER TABLE users ADD COLUMN is_owner             BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_manage_admins    BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_manage_users     BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_manage_libraries BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_manage_iptv      BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_edit_metadata    BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_change_artwork   BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN can_view_audit       BOOLEAN NOT NULL DEFAULT 0;

-- Backfill admins existentes → todos los flags = 1 (excepto is_owner
-- que se asigna en el paso siguiente). Garantiza que los admins
-- previos no pierden capacidad cuando se aplica esta migración.
UPDATE users
SET can_manage_admins    = 1,
    can_manage_users     = 1,
    can_manage_libraries = 1,
    can_manage_iptv      = 1,
    can_edit_metadata    = 1,
    can_change_artwork   = 1,
    can_view_audit       = 1
WHERE role = 'admin' AND parent_user_id IS NULL;

-- Owner = admin más antiguo. Sin admins, sin owner — el setup
-- wizard lo asignará al crear el primer cuenta-titular.
UPDATE users SET is_owner = 1
WHERE id = (
    SELECT id FROM users
    WHERE role = 'admin' AND parent_user_id IS NULL
    ORDER BY created_at ASC
    LIMIT 1
);

-- Unicidad del owner via índice parcial. Es la única defensa a nivel
-- DB; el resto (no auto-revocar el flag, no borrar al owner) lo
-- enforce el handler.
CREATE UNIQUE INDEX idx_users_one_owner ON users(is_owner) WHERE is_owner = 1;
