-- +goose Up
-- Temporary-access window for users (access_expires_at). See SQLite
-- sibling for the design rationale. DATETIME → TIMESTAMPTZ.
ALTER TABLE users ADD COLUMN access_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_access_expires_at
    ON users(access_expires_at)
    WHERE access_expires_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_users_access_expires_at;
ALTER TABLE users DROP COLUMN IF EXISTS access_expires_at;
