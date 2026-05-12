-- +goose Up
-- Per-profile avatar color override. See SQLite sibling for the
-- design rationale.
ALTER TABLE users ADD COLUMN avatar_color TEXT;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS avatar_color;
