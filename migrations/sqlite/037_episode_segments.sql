-- +goose Up
--
-- 037_episode_segments.sql — intro / outro / recap markers.
--
-- Skip-intro / skip-credits affordance, à la Netflix and Plex. We
-- store one segment per (item, kind, source) so the player can
-- surface a "Saltar intro" button when currentTime falls inside the
-- recorded range.
--
-- Why a separate table (and not columns on items):
--
--   - One item can carry several kinds at once (recap + intro +
--     outro on the same episode) without forcing the items row to
--     grow six nullable timestamp columns that almost every row
--     leaves NULL.
--   - Multiple detection sources can coexist. A chapter-derived
--     intro at confidence 0.95 and a fingerprint-derived one at
--     confidence 0.99 may both be valid; the player picks the
--     higher-confidence row at read time. Re-running a detector
--     replaces only its own (item, kind, source) row, so the other
--     source's row survives untouched.
--   - Cascade-on-item-delete is a single FK. Cleanup of stale
--     segments after a re-scan is one DELETE WHERE item_id = ?
--     statement instead of touching the items table.
--
-- Why ticks (not ms or seconds): the rest of the schema (chapters,
-- duration_ticks on items, probe Duration helpers) speaks 10M-ticks-
-- per-second. Matching that keeps DurationTicks() / TicksToDuration()
-- usable everywhere and removes one mental conversion across layers.
--
-- Source enum:
--
--   - 'chapter'     — derived from named chapter markers in the file.
--                     Cheap, runs in the scheduler's post-scan hook.
--   - 'fingerprint' — derived from cross-episode audio fingerprint
--                     matching (Phase 2; not implemented yet but the
--                     enum reserves it so a later detector doesn't
--                     need a schema change).
--   - 'manual'      — admin override via the UI (also future).
--
-- Confidence is 0..1 and used by the frontend to decide whether to
-- show the button automatically (>=0.7) or hide it behind a "more"
-- menu. Chapter-title-based detection uses a fixed 0.95 since a
-- chapter literally titled "Intro" is essentially ground truth.

CREATE TABLE episode_segments (
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('intro', 'outro', 'recap')),
    source       TEXT NOT NULL CHECK (source IN ('chapter', 'fingerprint', 'manual')),
    start_ticks  INTEGER NOT NULL CHECK (start_ticks >= 0),
    end_ticks    INTEGER NOT NULL CHECK (end_ticks > start_ticks),
    confidence   REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    detected_at  INTEGER NOT NULL,
    PRIMARY KEY (item_id, kind, source)
);

-- Read path: "give me every segment for this item" hits this index.
CREATE INDEX idx_episode_segments_item ON episode_segments(item_id);
