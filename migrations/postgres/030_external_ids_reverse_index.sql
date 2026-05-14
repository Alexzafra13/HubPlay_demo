-- +goose Up
-- Reverse-lookup index on external_ids (provider, external_id).
-- See SQLite sibling for the recommendations endpoint use case.
CREATE INDEX IF NOT EXISTS idx_external_ids_provider_id
    ON external_ids (provider, external_id);
