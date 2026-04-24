-- +goose Up
-- Per-library EPG source list.
--
-- Before this table each livetv library had exactly one `libraries.epg_url`
-- column. That model breaks down fast in practice: community EPGs like
-- davidmuma only cover a subset of channels, so the admin ends up with half
-- their channels showing "sin guía". The fix is to let a library point at
-- several providers in priority order — davidmuma covers the popular
-- Spanish cadenas, epg.pw fills the rest, iptv-org covers niche channels —
-- and have the refresher merge them into one coherent EPG.
--
-- Columns
--   catalog_id    nullable reference to `internal/iptv/epg_catalog.go`
--                 PublicEPGSources().ID. NULL means "operator pasted a
--                 custom URL the binary doesn't know about". Kept separate
--                 from `url` so we can upgrade catalog URLs on new releases
--                 without breaking existing rows whose operator intent was
--                 "I always want davidmuma-guiatv whatever its URL becomes".
--   priority      lower-first. The refresher processes sources in priority
--                 order; a channel covered by priority 0 is NOT touched by
--                 priority 1 (so davidmuma wins over epg.pw when both list
--                 La 1). Channels priority 0 missed get filled by priority 1.
--   last_status   'ok' | 'error' | NULL (never refreshed). Persisted so the
--                 admin UI can flag broken sources without waiting for the
--                 next refresh.
--
-- UNIQUE(library_id, url): the same source can't be added twice to a
-- library, regardless of whether it came from the catalog or a custom
-- paste. The catalog_id column doesn't participate in the uniqueness
-- check because two different catalog IDs could theoretically point at
-- the same URL (and that's still a duplicate).
CREATE TABLE library_epg_sources (
    id                 TEXT PRIMARY KEY,
    library_id         TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    catalog_id         TEXT,
    url                TEXT NOT NULL,
    priority           INTEGER NOT NULL DEFAULT 0,
    last_refreshed_at  DATETIME,
    last_status        TEXT,
    last_error         TEXT,
    last_program_count INTEGER NOT NULL DEFAULT 0,
    last_channel_count INTEGER NOT NULL DEFAULT 0,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(library_id, url)
);

CREATE INDEX idx_library_epg_sources_lib
    ON library_epg_sources(library_id, priority);

-- Migrate the old single-URL model into the new table: every library
-- that had an `epg_url` set gets a priority-0 custom source pointing
-- at the same URL. After migration the legacy column is no longer
-- consulted by the refresher — it's kept only so older clients don't
-- explode if they still read it.
INSERT INTO library_epg_sources
    (id, library_id, catalog_id, url, priority, created_at)
SELECT lower(hex(randomblob(16))), id, NULL, epg_url, 0, COALESCE(created_at, CURRENT_TIMESTAMP)
FROM libraries
WHERE epg_url IS NOT NULL AND epg_url != '';
