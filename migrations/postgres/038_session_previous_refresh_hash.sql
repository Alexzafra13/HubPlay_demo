-- +goose Up
-- Refresh-token reuse detection: one-step memory column on sessions.
-- See SQLite sibling for the full Auth0-style design rationale.
-- Translation is a single ALTER + an index.
ALTER TABLE sessions ADD COLUMN previous_refresh_token_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_sessions_previous_refresh_hash
    ON sessions(previous_refresh_token_hash);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_previous_refresh_hash;
ALTER TABLE sessions DROP COLUMN IF EXISTS previous_refresh_token_hash;
