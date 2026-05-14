-- +goose Up
-- JWT signing keys for rotation with overlap.
--
-- See migrations/sqlite/004_jwt_signing_keys.sql for the design notes
-- (kept identical between dialects — Postgres translation only
-- changes DATETIME → TIMESTAMPTZ).
CREATE TABLE jwt_signing_keys (
    id         TEXT PRIMARY KEY,
    secret     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retired_at TIMESTAMPTZ
);

-- Partial index: hot-path lookups by kid (every token validation).
CREATE INDEX idx_jwt_signing_keys_active ON jwt_signing_keys(retired_at)
    WHERE retired_at IS NULL;
