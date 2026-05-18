-- +goose Up
--
-- 049_notifications.sql - inbox de notificaciones generico por usuario.
--
-- Modelo deliberadamente abierto para que cualquier feature pueda emitir
-- entradas sin tocar el schema: la columna `kind` discrimina (string libre
-- tipo "federation.pairing_request_received") y `payload` lleva un JSON
-- arbitrario con datos especificos de ese kind (peer_uuid, fingerprint,
-- etc.). El frontend usa `kind` para decidir el icono + link de la
-- entrada y se trae los datos via las queries normales si hace falta
-- mas detalle.
--
-- Pensado como base extensible: hoy solo lo usa federation para avisar
-- de pairing requests entrantes; mañana puede tener "scan completado",
-- "tu peer X esta offline", "nueva temporada disponible", etc.
--
-- Columnas:
--   id          - UUID
--   user_id     - destinatario; FK con CASCADE para que borrar el user
--                 limpie su inbox.
--   kind        - identificador del tipo de notificacion. El frontend
--                 mapea esto a icono + traduccion + link.
--   title       - texto plano corto que se renderiza tal cual (el
--                 backend ya lo localiza/formatea antes de persistir).
--   body        - texto opcional con mas contexto (vacio = solo title).
--   link        - URL relativa del SPA a la que llevar al hacer click
--                 (e.g. "/admin/peers"). Vacio = no es clickable.
--   payload     - JSON con datos crudos por si el frontend quiere mas
--                 (peer_uuid, fingerprint, etc.). Vacio si no aplica.
--   created_at  - cuando se emitio
--   read_at     - NULL = sin leer. Cuando el user pulsa "marcar como
--                 leida" se setea timestamp. La query del badge cuenta
--                 las que tienen read_at IS NULL.
--
-- Indices:
--   - user + created_at desc: listing principal del dropdown.
--   - PARCIAL sobre user + created_at WHERE read_at IS NULL: el badge
--     del header consulta COUNT con este filtro a CADA SSE tick, asi
--     que un indice parcial es la diferencia entre O(1) y O(N).

CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    payload    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    read_at    TIMESTAMP
);

CREATE INDEX idx_notifications_user_created
    ON notifications(user_id, created_at DESC);

CREATE INDEX idx_notifications_unread
    ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;
