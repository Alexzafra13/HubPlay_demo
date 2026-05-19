-- +goose Up
-- Per-user upload gate. See SQLite sibling for design rationale.
-- INTEGER → BIGINT (quotas above 2 GB are routine for media), BOOLEAN
-- nativo en Postgres en vez del 0/1 de SQLite.

ALTER TABLE users ADD COLUMN can_upload BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN upload_quota_bytes BIGINT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN upload_used_bytes BIGINT NOT NULL DEFAULT 0;
