-- +goose Up
-- Federation: server identity + peer registry + invite codes.
-- See SQLite sibling for the full design notes. Postgres translation:
--   • BLOB (Ed25519 keys, 32 bytes) → BYTEA
--   • DATETIME → TIMESTAMPTZ
--   • CHECK / partial-index syntax is identical between dialects.

CREATE TABLE server_identity (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    server_uuid TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    private_key BYTEA NOT NULL,
    public_key  BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rotated_at  TIMESTAMPTZ
);

CREATE TABLE federation_peers (
    id                    TEXT PRIMARY KEY,
    server_uuid           TEXT NOT NULL UNIQUE,
    name                  TEXT NOT NULL,
    base_url              TEXT NOT NULL,
    public_key            BYTEA NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    paired_at             TIMESTAMPTZ,
    last_seen_at          TIMESTAMPTZ,
    last_seen_status_code INTEGER,
    revoked_at            TIMESTAMPTZ,
    CHECK (status IN ('pending', 'paired', 'revoked'))
);

CREATE INDEX idx_fed_peers_status ON federation_peers(status)
    WHERE status != 'revoked';

CREATE TABLE federation_invites (
    id                  TEXT PRIMARY KEY,
    code                TEXT NOT NULL UNIQUE,
    created_by_user_id  TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          TIMESTAMPTZ NOT NULL,
    accepted_by_peer_id TEXT REFERENCES federation_peers(id) ON DELETE SET NULL,
    accepted_at         TIMESTAMPTZ
);

CREATE INDEX idx_fed_invites_unused ON federation_invites(code)
    WHERE accepted_at IS NULL;
