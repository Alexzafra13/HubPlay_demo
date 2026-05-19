-- +goose Up
--
-- 053_user_upload_permission_quota.sql — per-user upload gate.
--
-- Foundation para la feature "subir películas/series desde la app sin
-- entrar al servidor". Tres columnas en `users`, ningún cambio de
-- tabla nueva — los uploads en curso y el audit log llegan en una
-- migración posterior, cuando el módulo `internal/upload` aterrice.
--
--   can_upload          BOOL  Permiso explícito. Default 0 = nadie
--                              puede subir hasta que un admin lo
--                              flipee. Pensado como "off by default"
--                              porque el riesgo (rellenar disco) es
--                              alto y la audiencia objetivo (familia,
--                              amigos) puede no necesitar el permiso.
--   upload_quota_bytes  INT   Tope absoluto que el usuario puede
--                              ocupar en disco con subidas suyas.
--                              0 = nada permitido (semántica
--                              defensiva: aunque can_upload=true, sin
--                              quota positiva el reserve falla).
--   upload_used_bytes   INT   Espacio actualmente ocupado por sus
--                              subidas. Mantenido por el upload
--                              service con UPDATE atómico que ENFORCA
--                              (upload_used_bytes + delta) <=
--                              upload_quota_bytes en el WHERE — la
--                              propia query devuelve "0 rows affected"
--                              cuando la reserva excede la cuota, y
--                              el service traduce eso a
--                              ErrUploadQuotaExceeded sin necesidad de
--                              transacción explícita.
--
-- BOOLEAN se mapea a INTEGER 0/1 en SQLite igual que `is_active`. El
-- gemelo Postgres usa BOOLEAN nativo.
--
-- No hay índice: estos campos sólo se leen por user_id (que ya es PK)
-- o se actualizan por user_id. Añadir índices sería ruido puro.

ALTER TABLE users ADD COLUMN can_upload BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN upload_quota_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN upload_used_bytes INTEGER NOT NULL DEFAULT 0;
