-- +goose Up
-- Netflix-style profile model on top of `users` (parent_user_id +
-- pin_hash + max_content_rating + password_change_required).
-- See SQLite sibling for the full design rationale.
--
-- Translation:
--   • BOOLEAN NOT NULL DEFAULT 0 → DEFAULT FALSE
--   • REFERENCES users(id) ON DELETE CASCADE in ADD COLUMN works
--     natively in Postgres.
ALTER TABLE users ADD COLUMN parent_user_id TEXT
    REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE users ADD COLUMN pin_hash TEXT;

ALTER TABLE users ADD COLUMN max_content_rating TEXT;

ALTER TABLE users ADD COLUMN password_change_required BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_parent_user_id
    ON users(parent_user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_users_parent_user_id;
ALTER TABLE users DROP COLUMN IF EXISTS password_change_required;
ALTER TABLE users DROP COLUMN IF EXISTS max_content_rating;
ALTER TABLE users DROP COLUMN IF EXISTS pin_hash;
ALTER TABLE users DROP COLUMN IF EXISTS parent_user_id;
