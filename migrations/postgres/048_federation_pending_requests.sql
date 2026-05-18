-- +goose Up
-- Inbox de peticiones de emparejamiento "Steam-style". Ver hermano
-- SQLite para el rationale completo.

CREATE TABLE federation_pending_requests (
    id                    TEXT PRIMARY KEY,
    direction             TEXT NOT NULL CHECK (direction IN ('incoming', 'outgoing')),
    peer_server_uuid      TEXT NOT NULL,
    peer_name             TEXT NOT NULL,
    peer_base_url         TEXT NOT NULL,
    peer_public_key       BYTEA NOT NULL,
    peer_avatar_color     TEXT NOT NULL DEFAULT '',
    peer_avatar_image_url TEXT NOT NULL DEFAULT '',
    request_token         TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL,
    expires_at            TIMESTAMPTZ NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled', 'expired')),
    responded_at          TIMESTAMPTZ,
    responded_by_user_id  TEXT
);

CREATE INDEX idx_pending_requests_status
    ON federation_pending_requests(status, expires_at);

CREATE UNIQUE INDEX idx_pending_requests_active_uniq
    ON federation_pending_requests(direction, peer_server_uuid)
    WHERE status = 'pending';
