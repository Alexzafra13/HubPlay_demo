-- +goose Up
-- Per-user key/value preference store. See SQLite sibling for the
-- full rationale (kept identical between dialects).
CREATE TABLE user_preferences (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, key)
);
