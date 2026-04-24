package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Kinds recognised by the IPTV scheduler. Kept as typed constants so
// handler + worker + test code share the same vocabulary and a typo
// fails at compile time.
const (
	IPTVJobKindM3URefresh = "m3u_refresh"
	IPTVJobKindEPGRefresh = "epg_refresh"
)

// ErrIPTVScheduledJobNotFound signals a missing (library_id, kind) row.
var ErrIPTVScheduledJobNotFound = errors.New("iptv scheduled job not found")

// IPTVScheduledJob is one (library, kind) schedule entry. Absent rows
// are equivalent to "not scheduled"; enabled=false + a row is "saved
// but paused" so the admin doesn't lose the interval they configured.
type IPTVScheduledJob struct {
	LibraryID       string
	Kind            string
	IntervalHours   int
	Enabled         bool
	LastRunAt       time.Time
	LastStatus      string // "ok" | "error" | "" (never run)
	LastError       string
	LastDurationMS  int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IPTVScheduleRepository persists automation schedules for IPTV
// libraries. Raw SQL on purpose — the sqlc adapter isn't regenerated
// as part of this change (see library_epg_sources_repository.go for
// the same rationale).
type IPTVScheduleRepository struct {
	db *sql.DB
}

func NewIPTVScheduleRepository(database *sql.DB) *IPTVScheduleRepository {
	return &IPTVScheduleRepository{db: database}
}

// ListByLibrary returns every scheduled job for a library. Empty slice
// means no rows; the handler layer synthesises "disabled, 24 h default"
// placeholders for the UI so the admin always sees both kinds.
func (r *IPTVScheduleRepository) ListByLibrary(ctx context.Context, libraryID string) ([]*IPTVScheduledJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT library_id, kind, interval_hours, enabled,
		        COALESCE(last_run_at, ''), last_status, last_error,
		        last_duration_ms, created_at, updated_at
		 FROM iptv_scheduled_jobs
		 WHERE library_id = ?
		 ORDER BY kind ASC`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list iptv scheduled jobs: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*IPTVScheduledJob
	for rows.Next() {
		j, scanErr := scanIPTVScheduledJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Get returns a single (library_id, kind) row. Returns
// ErrIPTVScheduledJobNotFound when missing so the handler can respond
// 404 without wrapping the sql.ErrNoRows.
func (r *IPTVScheduleRepository) Get(ctx context.Context, libraryID, kind string) (*IPTVScheduledJob, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT library_id, kind, interval_hours, enabled,
		        COALESCE(last_run_at, ''), last_status, last_error,
		        last_duration_ms, created_at, updated_at
		 FROM iptv_scheduled_jobs
		 WHERE library_id = ? AND kind = ?`, libraryID, kind)
	j, err := scanIPTVScheduledJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIPTVScheduledJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// ListDue returns every enabled job whose next run time has passed.
// "Never run" rows (last_run_at NULL / zero) are always due — the
// worker runs them on the first tick so enabling a job gives
// immediate feedback.
//
// Computation is done in Go rather than SQL because modernc.org/sqlite
// date arithmetic has rough edges with the multiple time serialisation
// formats the rest of the codebase has to tolerate. The SELECT just
// narrows to enabled=1; filtering by due-ness happens on the caller.
func (r *IPTVScheduleRepository) ListDue(ctx context.Context, now time.Time) ([]*IPTVScheduledJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT library_id, kind, interval_hours, enabled,
		        COALESCE(last_run_at, ''), last_status, last_error,
		        last_duration_ms, created_at, updated_at
		 FROM iptv_scheduled_jobs
		 WHERE enabled = 1
		 ORDER BY last_run_at ASC NULLS FIRST`)
	if err != nil {
		return nil, fmt.Errorf("list due jobs: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*IPTVScheduledJob
	for rows.Next() {
		j, scanErr := scanIPTVScheduledJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if j.IntervalHours <= 0 {
			// Guard against corrupted rows the CHECK constraint
			// wouldn't catch on older SQLite binaries. Skip rather
			// than panic the worker.
			continue
		}
		if j.LastRunAt.IsZero() {
			out = append(out, j)
			continue
		}
		next := j.LastRunAt.Add(time.Duration(j.IntervalHours) * time.Hour)
		if !now.Before(next) {
			out = append(out, j)
		}
	}
	return out, rows.Err()
}

// Upsert inserts or updates a (library_id, kind) row. Caller sets
// IntervalHours + Enabled; runtime fields (last_*) are not overwritten
// so a reconfiguration doesn't reset the history — enabling a job you
// just disabled keeps the "last ran 3 h ago" signal intact.
func (r *IPTVScheduleRepository) Upsert(ctx context.Context, job *IPTVScheduledJob) error {
	if job.IntervalHours <= 0 {
		return fmt.Errorf("interval_hours must be > 0")
	}
	if job.Kind != IPTVJobKindM3URefresh && job.Kind != IPTVJobKindEPGRefresh {
		return fmt.Errorf("invalid job kind %q", job.Kind)
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO iptv_scheduled_jobs
		   (library_id, kind, interval_hours, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(library_id, kind) DO UPDATE SET
		   interval_hours = excluded.interval_hours,
		   enabled        = excluded.enabled,
		   updated_at     = excluded.updated_at`,
		job.LibraryID, job.Kind, job.IntervalHours, job.Enabled,
		job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert iptv scheduled job: %w", err)
	}
	return nil
}

// RecordRun persists the outcome of a worker run. Error message is
// stored trimmed to avoid runaway payloads from verbose provider
// failures (stack traces, HTML error pages echoed in the message).
func (r *IPTVScheduleRepository) RecordRun(
	ctx context.Context,
	libraryID, kind, status, errMsg string,
	duration time.Duration,
	ranAt time.Time,
) error {
	const maxErrLen = 512
	if len(errMsg) > maxErrLen {
		errMsg = errMsg[:maxErrLen]
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE iptv_scheduled_jobs SET
		    last_run_at      = ?,
		    last_status      = ?,
		    last_error       = ?,
		    last_duration_ms = ?,
		    updated_at       = ?
		 WHERE library_id = ? AND kind = ?`,
		ranAt.UTC(), status, errMsg, duration.Milliseconds(), ranAt.UTC(),
		libraryID, kind)
	if err != nil {
		return fmt.Errorf("record iptv scheduled job run: %w", err)
	}
	return nil
}

// Delete removes a schedule row. CASCADE on libraries(id) already
// handles the library-deletion case; this is for the admin "stop
// scheduling" button.
func (r *IPTVScheduleRepository) Delete(ctx context.Context, libraryID, kind string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM iptv_scheduled_jobs WHERE library_id = ? AND kind = ?`,
		libraryID, kind); err != nil {
		return fmt.Errorf("delete iptv scheduled job: %w", err)
	}
	return nil
}

// rowScanner abstracts *sql.Row and *sql.Rows so scanIPTVScheduledJob
// can serve both Get and List paths without duplicating the column
// layout.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanIPTVScheduledJob(r rowScanner) (*IPTVScheduledJob, error) {
	j := &IPTVScheduledJob{}
	var lastRunRaw any
	if err := r.Scan(
		&j.LibraryID, &j.Kind, &j.IntervalHours, &j.Enabled,
		&lastRunRaw, &j.LastStatus, &j.LastError,
		&j.LastDurationMS, &j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	t, err := coerceSQLiteTime(lastRunRaw)
	if err != nil {
		return nil, fmt.Errorf("parse last_run_at: %w", err)
	}
	j.LastRunAt = t
	return j, nil
}
