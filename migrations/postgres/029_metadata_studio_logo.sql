-- +goose Up
-- Studio / network logo URL on metadata. See SQLite sibling for
-- the design rationale.
ALTER TABLE metadata ADD COLUMN studio_logo_url TEXT NOT NULL DEFAULT '';
