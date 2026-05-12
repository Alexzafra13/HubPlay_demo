-- +goose Up
-- Mirror of migrations/sqlite/005_drop_api_keys.sql — drops the unused
-- api_keys table introduced in 001 but never wired up. Idempotent.
DROP TABLE IF EXISTS api_keys;

-- +goose Down
-- No-op: recreating an unused table on rollback adds noise without
-- value. Same approach as the SQLite side.
