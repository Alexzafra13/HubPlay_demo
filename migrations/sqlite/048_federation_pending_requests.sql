-- +goose Up
--
-- 048_federation_pending_requests.sql - inbox de peticiones de
-- emparejamiento "Steam-style" sin codigo de invitacion.
--
-- El flujo legacy (federation_invites + /peer/handshake) sigue
-- existiendo en paralelo: admin A genera un codigo, lo pasa por
-- canal seguro, admin B lo pega. Ese flujo funciona pero exige
-- un copy-paste fuera de banda incomodo.
--
-- El flujo nuevo: admin A introduce la URL de B en su panel y
-- pulsa "Enviar peticion". Servidor A POST a B/federation/pairing-
-- requests; B persiste la peticion en este inbox; admin B ve un
-- badge en su header + entrada en su lista de peticiones; tras
-- comparar la huella OOB la acepta con un click; ambos lados
-- quedan emparejados. Cero copy-paste de codigos.
--
-- Tabla unica para AMBAS direcciones (incoming y outgoing) porque
-- el shape es identico (mismo branding del peer, mismo flujo de
-- estados); la columna direction discrimina. Asi un admin ve sus
-- propias peticiones enviadas + las que le han enviado en el
-- mismo listado, ordenado por created_at desc.
--
-- Columnas:
--   id                     - UUID del request
--   direction              - 'incoming' (entrante) o 'outgoing' (saliente)
--   peer_server_uuid       - UUID del remoto
--   peer_name              - nombre visible del remoto (snapshot al recibir)
--   peer_base_url          - URL del remoto (la que usamos para callbacks)
--   peer_public_key        - pubkey ed25519 del remoto, pinneada al crear
--   peer_avatar_color      - branding visual del remoto (snapshot)
--   peer_avatar_image_url  - URL absoluta de su foto (snapshot)
--   request_token          - secreto compartido para callbacks de
--                            accept/decline (peer A lo genera al enviar,
--                            B lo devuelve al confirmar - prueba que la
--                            callback viene del B esperado)
--   created_at             - cuando se creo la peticion
--   expires_at             - TTL 7 dias; despues se marca expired y se
--                            puede limpiar con un purge job (no en MVP).
--   status                 - 'pending' / 'accepted' / 'declined' /
--                            'cancelled' / 'expired'
--   responded_at           - cuando se acepto/declino/cancelo
--   responded_by_user_id   - quien lo respondio (admin del lado local)
--
-- Indices:
--   - status+expires_at: para el barrido periodico de expirados.
--   - direction+server_uuid PARCIAL WHERE status='pending': impide
--     que un mismo (direccion, peer) tenga dos peticiones pending
--     simultaneas. Si el remoto re-envia, el endpoint detecta el
--     conflicto y devuelve el id existente en vez de duplicar.

CREATE TABLE federation_pending_requests (
    id                    TEXT PRIMARY KEY,
    direction             TEXT NOT NULL CHECK (direction IN ('incoming', 'outgoing')),
    peer_server_uuid      TEXT NOT NULL,
    peer_name             TEXT NOT NULL,
    peer_base_url         TEXT NOT NULL,
    peer_public_key       BLOB NOT NULL,
    peer_avatar_color     TEXT NOT NULL DEFAULT '',
    peer_avatar_image_url TEXT NOT NULL DEFAULT '',
    request_token         TEXT NOT NULL,
    created_at            TIMESTAMP NOT NULL,
    expires_at            TIMESTAMP NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled', 'expired')),
    responded_at          TIMESTAMP,
    responded_by_user_id  TEXT
);

CREATE INDEX idx_pending_requests_status
    ON federation_pending_requests(status, expires_at);

CREATE UNIQUE INDEX idx_pending_requests_active_uniq
    ON federation_pending_requests(direction, peer_server_uuid)
    WHERE status = 'pending';
