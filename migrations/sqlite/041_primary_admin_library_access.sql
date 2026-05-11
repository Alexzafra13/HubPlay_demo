-- +goose Up
--
-- 041_primary_admin_library_access.sql — el "admin principal" (oldest
-- top-level role=admin row, mismo concepto que GetPrimaryAdminID en
-- users.sql) debe ver TODAS las bibliotecas en library_access. La
-- migración 040 lo excluyó del backfill (`AND u.role != 'admin'`)
-- asumiendo que el bypass de código (handlers ven todo si
-- claims.Role=='admin') bastaba, pero la matrix UI nueva (Phase B)
-- consulta library_access directo: un admin sin grants explícitos
-- veía la matriz vacía. Persistir los grants hace que el contrato
-- sea multi-dispositivo y consistente entre LIST y la matriz.
--
-- Sólo el admin principal recibe grants automáticos. Admins
-- secundarios siguen apoyándose en el bypass de LIST; la matriz
-- vacía refleja honestamente que no tienen grants explícitos. El
-- runtime hook en LibraryRepository.Create mantiene la invariante
-- para libraries nuevas sin necesidad de re-correr esta migración.
--
-- Idempotente (INSERT OR IGNORE) y no-op cuando no existe ningún
-- admin todavía (instalación pre-setup-wizard).

INSERT OR IGNORE INTO library_access (user_id, library_id)
SELECT primary_admin.id, l.id
FROM libraries l
CROSS JOIN (
    SELECT id FROM users
    WHERE role = 'admin' AND parent_user_id IS NULL
    ORDER BY created_at ASC
    LIMIT 1
) AS primary_admin;
