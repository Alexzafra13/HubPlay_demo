-- +goose Up
-- TMDb trailer key + site on metadata. See SQLite sibling for the
-- full rationale. Postgres translation is a direct ALTER TABLE.
ALTER TABLE metadata ADD COLUMN trailer_key TEXT NOT NULL DEFAULT '';
ALTER TABLE metadata ADD COLUMN trailer_site TEXT NOT NULL DEFAULT '';
