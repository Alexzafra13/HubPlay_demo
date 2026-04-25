package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
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
	LibraryID      string
	Kind           string
	IntervalHours  int
	Enabled        bool
	LastRunAt      time.Time
	LastStatus     string // "ok" | "error" | "" (never run)
	LastError      string
	LastDurationMS int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IPTVScheduleRepository persists automation schedules for IPTV
// libraries. Sqlc-generated queries do the heavy lifting; this thin
// adapter handles the (a) sql.NullTime → time.Time projection and
// (b) Go-side filtering of due-ness in ListDue (the date arithmetic
// stays out of SQL — see queries/iptv_scheduled_jobs.sql).
type IPTVScheduleRepository struct {
	q *sqlc.Queries
}

func NewIPTVScheduleRepository(database *sql.DB) *IPTVScheduleRepository {
	return &IPTVScheduleRepository{q: sqlc.New(database)}
}

// ListByLibrary returns every scheduled job for a library. Empty
// slice means no rows; the handler layer synthesises "disabled, 24 h
// default" placeholders for the UI so the admin always sees both
// kinds.
func (r *IPTVScheduleRepository) ListByLibrary(ctx context.Context, libraryID string) ([]*IPTVScheduledJob, error) {
	rows, err := r.q.ListIPTVScheduledJobsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list iptv scheduled jobs: %w", err)
	}
	out := make([]*IPTVScheduledJob, 0, len(rows))
	for _, row := range rows {
		j := iptvJobFromSQLC(row)
		out = append(out, &j)
	}
	return out, nil
}

// Get returns a single (library_id, kind) row. Returns
// ErrIPTVScheduledJobNotFound when missing so the handler can respond
// 404 without wrapping the sql.ErrNoRows.
func (r *IPTVScheduleRepository) Get(ctx context.Context, libraryID, kind string) (*IPTVScheduledJob, error) {
	row, err := r.q.GetIPTVScheduledJob(ctx, sqlc.GetIPTVScheduledJobParams{
		LibraryID: libraryID, Kind: kind,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIPTVScheduledJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get iptv scheduled job: %w", err)
	}
	j := iptvJobFromSQLC(row)
	return &j, nil
}

// ListDue returns every enabled job whose next run time has passed.
// "Never run" rows (last_run_at NULL) are always due — the worker
// runs them on the first tick so enabling a job gives immediate
// feedback.
//
// Computation is done in Go rather than SQL because modernc.org/sqlite
// date arithmetic has rough edges with the multiple time serialisation
// formats the rest of the codebase has to tolerate.
func (r *IPTVScheduleRepository) ListDue(ctx context.Context, now time.Time) ([]*IPTVScheduledJob, error) {
	rows, err := r.q.ListEnabledIPTVScheduledJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list due jobs: %w", err)
	}
	out := make([]*IPTVScheduledJob, 0, len(rows))
	for _, row := range rows {
		j := iptvJobFromSQLC(row)
		if j.IntervalHours <= 0 {
			// Guard against corrupted rows the CHECK constraint
			// wouldn't catch on older SQLite binaries. Skip rather
			// than panic the worker.
			continue
		}
		if j.LastRunAt.IsZero() {
			out = append(out, &j)
			continue
		}
		next := j.LastRunAt.Add(time.Duration(j.IntervalHours) * time.Hour)
		if !now.Before(next) {
			out = append(out, &j)
		}
	}
	return out, nil
}

// Upsert inserts or updates a (library_id, kind) row. Caller sets
// IntervalHours + Enabled; runtime fields (last_*) are not overwritten
// so a reconfiguration doesn't reset the history.
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

	if err := r.q.UpsertIPTVScheduledJob(ctx, sqlc.UpsertIPTVScheduledJobParams{
		LibraryID:     job.LibraryID,
		Kind:          job.Kind,
		IntervalHours: int64(job.IntervalHours),
		Enabled:       job.Enabled,
		CreatedAt:     job.CreatedAt,
		UpdatedAt:     job.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("upsert iptv scheduled job: %w", err)
	}
	return nil
}

// RecordRun persists the outcome of a worker run. Error message is
// stored trimmed to avoid runaway payloads from verbose provider
// failures.
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
	utcRanAt := ranAt.UTC()
	if err := r.q.RecordIPTVScheduledJobRun(ctx, sqlc.RecordIPTVScheduledJobRunParams{
		LastRunAt:      sql.NullTime{Time: utcRanAt, Valid: true},
		LastStatus:     status,
		LastError:      errMsg,
		LastDurationMs: duration.Milliseconds(),
		UpdatedAt:      utcRanAt,
		LibraryID:      libraryID,
		Kind:           kind,
	}); err != nil {
		return fmt.Errorf("record iptv scheduled job run: %w", err)
	}
	return nil
}

// Delete removes a schedule row. Idempotent.
func (r *IPTVScheduleRepository) Delete(ctx context.Context, libraryID, kind string) error {
	if err := r.q.DeleteIPTVScheduledJob(ctx, sqlc.DeleteIPTVScheduledJobParams{
		LibraryID: libraryID, Kind: kind,
	}); err != nil {
		return fmt.Errorf("delete iptv scheduled job: %w", err)
	}
	return nil
}

// iptvJobFromSQLC projects the generated row into the domain struct.
// Kept private to the repo — callers see only the public type.
func iptvJobFromSQLC(row sqlc.IptvScheduledJob) IPTVScheduledJob {
	out := IPTVScheduledJob{
		LibraryID:      row.LibraryID,
		Kind:           row.Kind,
		IntervalHours:  int(row.IntervalHours),
		Enabled:        row.Enabled,
		LastStatus:     row.LastStatus,
		LastError:      row.LastError,
		LastDurationMS: int(row.LastDurationMs),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	if row.LastRunAt.Valid {
		out.LastRunAt = row.LastRunAt.Time
	}
	return out
}
