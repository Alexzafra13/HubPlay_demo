-- +goose Up
-- Device authorization grant codes (RFC 8628). See SQLite sibling
-- for the full flow. Direct translation — DATETIME → TIMESTAMPTZ.
CREATE TABLE device_codes (
    device_code     TEXT PRIMARY KEY,
    user_code       TEXT NOT NULL UNIQUE,
    device_name     TEXT NOT NULL,
    user_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_at     TIMESTAMPTZ,
    consumed_at     TIMESTAMPTZ,
    last_polled_at  TIMESTAMPTZ
);

CREATE INDEX idx_device_codes_user_code ON device_codes(user_code);
CREATE INDEX idx_device_codes_expires ON device_codes(expires_at);
