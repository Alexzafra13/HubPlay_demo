-- +goose Up
-- IPTV language filter on libraries. See SQLite sibling for the
-- full rationale (comma-separated ISO 639-1 codes, applied at
-- import time).
ALTER TABLE libraries ADD COLUMN language_filter TEXT NOT NULL DEFAULT '';
