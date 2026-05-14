-- +goose Up
-- Persisted runtime settings (admin-editable from the panel). See
-- SQLite sibling for the full design rationale. Postgres translation
-- only changes DATETIME → TIMESTAMPTZ.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
