-- +goose Up
--
-- 056_cors_origins.sql — Orígenes CORS añadidos en runtime via el
-- panel admin (PR4 feature CORS-dynamic).
--
-- Contexto: la configuración CORS hasta aquí venía sólo del YAML
-- (Server.BaseURL + localhost dev). Para deploys donde el operador
-- añade frontends nuevos sin reiniciar el binario, esta tabla
-- guarda orígenes EXTRA que el middleware une con los del YAML.
--
-- Política:
--   - Los orígenes del YAML son INMUTABLES en runtime (no aparecen
--     en esta tabla). Auditables en git.
--   - Los orígenes de esta tabla los gestiona SÓLO el owner. Añadir
--     un origen es abrir superficie de CSRF cross-origin — owner-only
--     enforza que la decisión la toma quien custodia la instalación.
--   - El middleware CORS combina ambas listas en cada preflight via
--     un atomic.Pointer; añadir/quitar surte efecto inmediato sin
--     reiniciar el server.
--
-- created_by referencia users(id) sin FK con CASCADE — el log de
-- quién añadió el origen sobrevive al usuario que ya no exista
-- (misma política que upload_audit). Si el user se borra, el origen
-- queda con created_by apuntando a un id huérfano; la UI lo pinta
-- como "unknown" sin romper.

CREATE TABLE cors_origins (
    -- El origin canónico: scheme://host[:port]. Validado en el
    -- handler antes de insertar (sin path, sin trailing slash, sólo
    -- http/https, no wildcards).
    origin       TEXT PRIMARY KEY,

    -- Quién y cuándo. created_by es nullable porque la migración
    -- desde una instalación que ya tenía orígenes (futuro hipotético)
    -- no tiene a quién atribuir.
    created_by   TEXT,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Texto libre opcional. El operador apunta "frontend staging
    -- desplegado en cloudflare pages el 2026-05-20" para que tres
    -- meses después no se pregunte qué es ese origen.
    note         TEXT NOT NULL DEFAULT ''
);

-- Listado ordenado por orden de creación (LIFO en la UI: el más
-- reciente arriba). Sin LIMIT porque la cardinalidad esperada es
-- pequeña — operadores reales tienen 1-10 orígenes extra, no miles.
CREATE INDEX idx_cors_origins_created_at ON cors_origins(created_at DESC);
