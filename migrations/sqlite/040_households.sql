-- +goose Up
--
-- 040_household_access.sql — el "hogar" se materializa reusando el
-- profile system existente (parent_user_id, migración 034): el top-
-- level user es el dueño del hogar, sus profiles son los miembros.
-- El acceso se otorga SIEMPRE al top-level user; los profiles
-- heredan vía COALESCE(parent_user_id, id) en el predicado.
--
-- Esta migración normaliza los datos existentes para que la regla
-- arriba sea consistente:
--
--   1. Cualquier grant en library_access cuyo user_id sea un profile
--      (parent_user_id != NULL) se reemite hacia el parent. Si ya
--      había un grant para el parent, el INSERT OR IGNORE lo deja
--      pasar sin duplicar.
--
--   2. Tras la promoción, los grants per-profile se borran. Quedan
--      sólo grants contra top-level users.
--
--   3. Bibliotecas que NO tenían ningún grant (modelo opt-in viejo:
--      "público por defecto") reciben grant explícito hacia cada
--      top-level user no-admin existente. El predicado nuevo es
--      strict (sin fallback público) y sin esto los usuarios
--      existentes perderían visibilidad al desplegar la migración.
--      Pre-v1.0 squash limpiaría esto al consolidar el schema.

-- Paso 1: promover grants de profile al parent.
INSERT OR IGNORE INTO library_access (user_id, library_id)
SELECT u.parent_user_id, la.library_id
FROM library_access la
JOIN users u ON u.id = la.user_id
WHERE u.parent_user_id IS NOT NULL;

-- Paso 2: limpiar grants huérfanos de profiles.
DELETE FROM library_access
WHERE user_id IN (
    SELECT id FROM users WHERE parent_user_id IS NOT NULL
);

-- Paso 3: backfill de visibilidad para bibliotecas que eran
-- "públicas" (sin ningún grant). Otorga acceso a cada top-level
-- user no-admin para que el modelo strict no rompa data existente.
INSERT OR IGNORE INTO library_access (user_id, library_id)
SELECT u.id, l.id
FROM users u
CROSS JOIN libraries l
WHERE u.parent_user_id IS NULL
  AND u.role != 'admin'
  AND NOT EXISTS (
      SELECT 1 FROM library_access la WHERE la.library_id = l.id
  );
