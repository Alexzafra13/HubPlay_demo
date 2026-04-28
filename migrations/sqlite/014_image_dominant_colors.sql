-- +goose Up
-- Adds two dominant-color columns to the images table so the frontend can
-- render the SeriesHero gradient on first paint without round-tripping
-- through node-vibrant. Colors are stored as `rgb(r, g, b)` strings for
-- direct injection into CSS variables — keeps the API symmetric with how
-- the frontend already consumes the values.
--
-- Both columns are nullable: existing rows stay valid (they degrade to
-- the runtime-extraction fallback the frontend already supports), and a
-- failed extraction at scan time leaves them empty rather than failing
-- the whole image ingest.
ALTER TABLE images ADD COLUMN dominant_color TEXT NOT NULL DEFAULT '';
ALTER TABLE images ADD COLUMN dominant_color_muted TEXT NOT NULL DEFAULT '';
