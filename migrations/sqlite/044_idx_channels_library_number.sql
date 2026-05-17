-- +goose Up
--
-- 044_idx_channels_library_number.sql — closes UUU-mig of the
-- 2026-05-14 audit. The hot channel listings
-- (ListChannelsByLibrary, ListActiveChannelsByLibrary in
-- internal/db/queries/channels.sql) run
--
--   SELECT ... FROM channels WHERE library_id = ? ORDER BY number, name
--
-- The pre-existing idx_channels_library only covers `library_id`,
-- so the ORDER BY forced an in-memory sort over the filtered set.
-- Fine on small libraries; visibly costly on iptv libraries with
-- 5 000+ channels per playlist where the listing is the LiveTV
-- home rail itself. The composite index lets the planner walk the
-- B-tree in (library_id, number) order and avoid the sort.
--
-- We leave the pre-existing idx_channels_library in place — it's
-- still useful for queries that don't ORDER BY number (e.g.
-- counts) and dropping it would force a planner re-evaluation we
-- haven't measured.

CREATE INDEX IF NOT EXISTS idx_channels_library_number
    ON channels(library_id, number);
