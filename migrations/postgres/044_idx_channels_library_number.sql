-- +goose Up
--
-- 044_idx_channels_library_number.sql — Postgres twin of
-- migrations/sqlite/044. See the sqlite file for the design
-- rationale (UUU-mig of the 2026-05-14 audit).

CREATE INDEX IF NOT EXISTS idx_channels_library_number
    ON channels(library_id, number);
