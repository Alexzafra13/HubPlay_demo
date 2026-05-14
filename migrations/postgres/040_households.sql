-- +goose Up
-- Household access model: promote per-profile grants in library_access
-- to the parent (top-level) user; backfill visibility for pre-existing
-- non-admin users. See SQLite sibling for the full design rationale.
--
-- Postgres translation:
--   • INSERT OR IGNORE → INSERT … ON CONFLICT DO NOTHING
--   • CROSS JOIN, NOT EXISTS, subselects: identical in both dialects.
--   • The DELETE FROM ... WHERE ... IN (SELECT ...) form is standard
--     SQL.

-- Paso 1: promover grants de profile al parent.
INSERT INTO library_access (user_id, library_id)
SELECT u.parent_user_id, la.library_id
FROM library_access la
JOIN users u ON u.id = la.user_id
WHERE u.parent_user_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Paso 2: limpiar grants huérfanos de profiles.
DELETE FROM library_access
WHERE user_id IN (
    SELECT id FROM users WHERE parent_user_id IS NOT NULL
);

-- Paso 3: backfill de visibilidad para bibliotecas sin ningún grant.
-- Otorga acceso a cada top-level user no-admin para que el modelo
-- strict no rompa data existente.
INSERT INTO library_access (user_id, library_id)
SELECT u.id, l.id
FROM users u
CROSS JOIN libraries l
WHERE u.parent_user_id IS NULL
  AND u.role != 'admin'
  AND NOT EXISTS (
      SELECT 1 FROM library_access la WHERE la.library_id = l.id
  )
ON CONFLICT DO NOTHING;
