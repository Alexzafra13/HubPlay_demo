-- +goose Up
-- has_poster flag on federation cache. See SQLite sibling for
-- background.
--
-- Translation note: Postgres supports ALTER TABLE DROP COLUMN
-- natively (SQLite < 3.35 required a table-rebuild; the SQLite
-- migration includes that legacy down block). Our Postgres down is
-- a clean DROP COLUMN.
ALTER TABLE federation_item_cache
    ADD COLUMN has_poster BOOLEAN NOT NULL DEFAULT FALSE;
