-- +goose Up
-- Scheduled IPTV jobs (M3U + EPG refresh). See SQLite sibling for the
-- full rationale. Postgres translation:
--   • DATETIME → TIMESTAMPTZ
--   • BOOLEAN DEFAULT 0 → BOOLEAN DEFAULT FALSE
--   • Partial index `WHERE enabled = 1` → `WHERE enabled = TRUE`
--     (Postgres is strict — `enabled = 1` would error on a BOOLEAN
--     column).
CREATE TABLE iptv_scheduled_jobs (
    library_id        TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    kind              TEXT NOT NULL CHECK (kind IN ('m3u_refresh', 'epg_refresh')),
    interval_hours    INTEGER NOT NULL DEFAULT 24,
    enabled           BOOLEAN NOT NULL DEFAULT FALSE,
    last_run_at       TIMESTAMPTZ,
    last_status       TEXT NOT NULL DEFAULT '',
    last_error        TEXT NOT NULL DEFAULT '',
    last_duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, kind)
);

-- Partial index on enabled rows. Postgres requires the boolean
-- literal TRUE here; `enabled = 1` is a type error.
CREATE INDEX idx_iptv_jobs_enabled_due
    ON iptv_scheduled_jobs(last_run_at)
    WHERE enabled = TRUE;

-- +goose Down
DROP INDEX IF EXISTS idx_iptv_jobs_enabled_due;
DROP TABLE IF EXISTS iptv_scheduled_jobs;
