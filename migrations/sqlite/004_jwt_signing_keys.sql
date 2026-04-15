-- +goose Up
-- JWT signing keys for rotation with overlap.
--
-- Each key has an opaque id (used as the JWT "kid" header) and a secret used
-- to HMAC-sign access tokens. At most one key is "primary" (the one signing
-- new tokens): the most recently created row with retired_at IS NULL.
--
-- Rotation inserts a new row; the previous primary keeps retired_at NULL for
-- a short overlap so in-flight tokens keep validating, then the admin path
-- (or the background pruner, future work) sets retired_at to retire it.
CREATE TABLE jwt_signing_keys (
    id         TEXT PRIMARY KEY,
    secret     TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retired_at DATETIME
);

-- Partial index: lookups by kid dominate hot paths (every token validation).
CREATE INDEX idx_jwt_signing_keys_active ON jwt_signing_keys(retired_at)
    WHERE retired_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_jwt_signing_keys_active;
DROP TABLE IF EXISTS jwt_signing_keys;
