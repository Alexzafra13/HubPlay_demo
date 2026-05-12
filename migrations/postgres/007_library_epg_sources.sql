-- +goose Up
-- Per-library EPG source list. See SQLite sibling for the full
-- design rationale.
--
-- Translation note for the backfill: the SQLite version generates an
-- id via `lower(hex(randomblob(16)))` to produce a 32-char hex string.
-- Postgres equivalent uses pgcrypto's `gen_random_bytes(16)` encoded
-- to hex. Enabled on demand below so this migration is self-contained
-- (the extension may already exist from a previous migration; the
-- IF NOT EXISTS guard makes the call idempotent).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE library_epg_sources (
    id                 TEXT PRIMARY KEY,
    library_id         TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    catalog_id         TEXT,
    url                TEXT NOT NULL,
    priority           INTEGER NOT NULL DEFAULT 0,
    last_refreshed_at  TIMESTAMPTZ,
    last_status        TEXT,
    last_error         TEXT,
    last_program_count INTEGER NOT NULL DEFAULT 0,
    last_channel_count INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(library_id, url)
);

CREATE INDEX idx_library_epg_sources_lib
    ON library_epg_sources(library_id, priority);

-- Backfill from the legacy single-URL model. Every library that had
-- an `epg_url` set gets a priority-0 row pointing at the same URL.
INSERT INTO library_epg_sources
    (id, library_id, catalog_id, url, priority, created_at)
SELECT encode(gen_random_bytes(16), 'hex'),
       id,
       NULL,
       epg_url,
       0,
       COALESCE(created_at, CURRENT_TIMESTAMP)
FROM libraries
WHERE epg_url IS NOT NULL AND epg_url != '';

-- +goose Down
DROP INDEX IF EXISTS idx_library_epg_sources_lib;
DROP TABLE IF EXISTS library_epg_sources;
-- pgcrypto stays — other migrations may depend on it.
