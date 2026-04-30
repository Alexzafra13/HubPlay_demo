-- +goose Up
-- Device authorization grant codes (RFC 8628). The flow:
--
--  1. A device (TV app, CLI tool, headless service) calls
--     POST /api/v1/auth/device/start. The server inserts a row here
--     with a fresh user_code + device_code, user_id NULL.
--
--  2. The device displays the user_code to its operator and starts
--     polling POST /api/v1/auth/device/poll with the device_code.
--     While user_id IS NULL the poll returns 'authorization_pending'.
--
--  3. The operator opens the verification URL on a phone/laptop
--     (an authenticated browser), pastes the user_code, and approves.
--     The approve handler updates user_id + approved_at on this row.
--
--  4. Next poll sees approved_at populated and consumed_at NULL: the
--     handler issues access + refresh tokens (via the existing session
--     machinery) and sets consumed_at to mark the code single-use.
--
-- Hard 10-minute TTL via expires_at — a leaked user_code becomes
-- worthless after that window even if nobody noticed.

CREATE TABLE device_codes (
    device_code     TEXT PRIMARY KEY,                      -- opaque 32-char hex (128-bit randomness)
    user_code       TEXT NOT NULL UNIQUE,                  -- short alphanumeric, displayed to user (e.g. "ABCD-EFGH")
    device_name     TEXT NOT NULL,                         -- friendly label supplied by the device (e.g. "Living-room TV")
    user_id         TEXT REFERENCES users(id) ON DELETE SET NULL, -- approver — NULL until approval
    expires_at      DATETIME NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_at     DATETIME,                              -- timestamp of the operator's approval click
    consumed_at     DATETIME,                              -- timestamp of token issuance — code becomes single-use
    last_polled_at  DATETIME                               -- updated on every poll; used for slow-down detection
);

-- The user_code is the lookup key when the operator pastes it on /link.
CREATE INDEX idx_device_codes_user_code ON device_codes(user_code);

-- Cleanup index — the background sweep deletes expired/consumed rows.
CREATE INDEX idx_device_codes_expires ON device_codes(expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_device_codes_expires;
DROP INDEX IF EXISTS idx_device_codes_user_code;
DROP TABLE IF EXISTS device_codes;
