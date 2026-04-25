-- Scheduled IPTV jobs (per-library M3U + EPG refresh automation).
--
-- Table schema: migrations/sqlite/011_iptv_scheduled_jobs.sql.
-- Composite PK: (library_id, kind in {'m3u_refresh','epg_refresh'}).
--
-- Time handling: callers MUST normalise time.Time values to UTC
-- before passing them in (see iptv_schedule_repository.go). The
-- modernc.org/sqlite driver serialises time.Time with named-zone
-- locations using a format the default Scan cannot parse, so we
-- treat UTC-on-write as a hard contract. Reads land into stdlib
-- time.Time / sql.NullTime without coerceSQLiteTime needing to
-- intervene.

-- name: ListIPTVScheduledJobsByLibrary :many
SELECT library_id, kind, interval_hours, enabled,
       last_run_at, last_status, last_error,
       last_duration_ms, created_at, updated_at
FROM iptv_scheduled_jobs
WHERE library_id = ?
ORDER BY kind ASC;

-- name: GetIPTVScheduledJob :one
SELECT library_id, kind, interval_hours, enabled,
       last_run_at, last_status, last_error,
       last_duration_ms, created_at, updated_at
FROM iptv_scheduled_jobs
WHERE library_id = ? AND kind = ?;

-- Workers hot query. Filtered by enabled in SQL; due-ness is
-- computed in Go (see ListDue) because date arithmetic on the
-- multi-format-tolerant column would invite the same Scan problem
-- we work around elsewhere.

-- name: ListEnabledIPTVScheduledJobs :many
SELECT library_id, kind, interval_hours, enabled,
       last_run_at, last_status, last_error,
       last_duration_ms, created_at, updated_at
FROM iptv_scheduled_jobs
WHERE enabled = 1
ORDER BY last_run_at ASC NULLS FIRST;

-- Preserves last_* fields by design: only the configuration
-- (interval_hours / enabled) and updated_at change. The history
-- (last_run_at, last_status, ...) survives reconfiguration.

-- name: UpsertIPTVScheduledJob :exec
INSERT INTO iptv_scheduled_jobs (library_id, kind, interval_hours, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(library_id, kind) DO UPDATE SET
   interval_hours = excluded.interval_hours,
   enabled        = excluded.enabled,
   updated_at     = excluded.updated_at;

-- name: RecordIPTVScheduledJobRun :exec
UPDATE iptv_scheduled_jobs SET
    last_run_at      = ?,
    last_status      = ?,
    last_error       = ?,
    last_duration_ms = ?,
    updated_at       = ?
WHERE library_id = ? AND kind = ?;

-- name: DeleteIPTVScheduledJob :exec
DELETE FROM iptv_scheduled_jobs WHERE library_id = ? AND kind = ?;
