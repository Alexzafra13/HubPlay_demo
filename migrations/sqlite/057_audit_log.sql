-- +goose Up
--
-- 057_audit_log.sql — Audit log unificado de toda la app (PR5).
--
-- Contexto: hasta aquí teníamos audit fragmentado:
--   - upload_audit (migración 054) — sólo subidas.
--   - logs en texto (LogBuffer) — efímeros, sin estructura.
-- El operador con varios admins NO podía responder "quién hizo X
-- cuándo" sin SSH a la DB o leer logs por grep. Estándar en
-- productos serios (Plex, Jellyfin, etc.) y barato de implementar
-- con una tabla genérica.
--
-- Diseño:
--   - event_type es una cadena jerárquica tipo "auth.login.failed",
--     "permission.changed", "library.deleted". El servicio puede
--     filtrar por prefijo ("auth.*") sin enums rígidos.
--   - target_type + target_id apuntan al objeto afectado. Vacíos
--     para eventos sin target (system.restart).
--   - payload es JSON opcional con detalles específicos del evento.
--     Schemaless a propósito — cada productor define su shape.
--   - actor_user_id sin FK: el log sobrevive al usuario borrado
--     (misma política que upload_audit).
--   - ip/user_agent capturados del request HTTP cuando aplica;
--     vacíos en eventos generados por jobs background.
--
-- Retención: la tabla la barre el job de retención (config.Retention)
-- con AuditLog timeout — default 90d. Sin esto, en años acumularía
-- decenas de millones de filas. 90d cubre cualquier investigación
-- razonable + auditoría compliance ligera.

CREATE TABLE audit_log (
    id              TEXT PRIMARY KEY,
    actor_user_id   TEXT,              -- vacío si la acción no estaba autenticada (login fallido)
    event_type      TEXT NOT NULL,     -- "auth.login.ok", "permission.changed", "upload.accepted"...
    target_type     TEXT NOT NULL DEFAULT '',  -- "user", "library", "item", "channel", ""
    target_id       TEXT NOT NULL DEFAULT '',
    payload         TEXT NOT NULL DEFAULT '',  -- JSON, schemaless por evento
    ip_address      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index principal: el panel ordena DESC por created_at y filtra por
-- ventana temporal. Es el hot path.
CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);

-- Filtrar por actor (panel "qué hizo Bob"). Cardinalidad media —
-- decenas de usuarios típicamente, miles de eventos por usuario en
-- 90d, así que el índice paga claramente.
CREATE INDEX idx_audit_log_actor_created ON audit_log(actor_user_id, created_at DESC);

-- Filtrar por event_type (panel "todos los logins"). Prefix matching
-- con LIKE 'auth.%' usa este índice para los primeros 1-2 segmentos.
CREATE INDEX idx_audit_log_type_created ON audit_log(event_type, created_at DESC);
