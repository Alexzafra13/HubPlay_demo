-- +goose Up
-- Scheduled IPTV jobs: automates M3U and EPG refreshes so the product
-- stops requiring an admin to click "Refrescar" every morning.
--
-- Shape: composite PK (library_id, kind) — one row per job per
-- library, two possible kinds:
--   * 'm3u_refresh' → iptv.Service.RefreshM3U
--   * 'epg_refresh' → iptv.Service.RefreshEPG
-- The kind/library pair is unique because running two concurrent
-- M3U refreshes on the same library makes no sense (and the service
-- already guards against it with a per-library lock).
--
-- interval_hours is integer for UI simplicity (dropdown of 1/3/6/12/24
-- etc.). The worker interprets it as hours since last_run_at; a value
-- of 0 is invalid and rejected at the handler layer.
--
-- last_run_at NULL means "never run" — the worker runs such rows on
-- the first tick after they're enabled. last_status is '' initially
-- and becomes 'ok' / 'error' after the first run. last_error captures
-- the trimmed message; last_duration_ms lets the UI show "took 3.4 s".
--
-- Index: the worker's hot query is "enabled rows whose next run is
-- due". A partial index on enabled=1 keeps it tight; ORDER BY on
-- last_run_at (NULLs first) gives us the oldest-first scan.
CREATE TABLE iptv_scheduled_jobs (
    library_id        TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    kind              TEXT NOT NULL CHECK (kind IN ('m3u_refresh', 'epg_refresh')),
    interval_hours    INTEGER NOT NULL DEFAULT 24,
    enabled           BOOLEAN NOT NULL DEFAULT 0,
    last_run_at       DATETIME,
    last_status       TEXT NOT NULL DEFAULT '',
    last_error        TEXT NOT NULL DEFAULT '',
    last_duration_ms  INTEGER NOT NULL DEFAULT 0,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, kind)
);

CREATE INDEX idx_iptv_jobs_enabled_due
    ON iptv_scheduled_jobs(last_run_at)
    WHERE enabled = 1;
