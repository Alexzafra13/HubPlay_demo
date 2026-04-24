package iptv

// Scheduler: background worker that drives periodic M3U and EPG
// refreshes. Reads enabled/due rows from iptv_scheduled_jobs and
// dispatches to iptv.Service. Models the same Start/Stop contract as
// library.Scheduler so main.go can sequence shutdown symmetrically.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"hubplay/internal/db"
)

// jobRunner is the subset of iptv.Service the scheduler needs. Kept as
// an interface so tests can substitute a fake that counts calls and
// injects errors without spinning a full Service.
type jobRunner interface {
	RefreshM3U(ctx context.Context, libraryID string) (int, error)
	RefreshEPG(ctx context.Context, libraryID string) (int, error)
}

// Scheduler polls for due jobs at tickInterval and runs them through
// the injected runner. A single goroutine owns the whole loop — runs
// happen sequentially. Two concurrent jobs on the same library would
// serialise anyway inside iptv.Service (per-library refresh lock), and
// running refreshes in parallel across libraries would burst the CDN
// that provides the playlists; sequential is the right default.
type Scheduler struct {
	repo       *db.IPTVScheduleRepository
	runner     jobRunner
	logger     *slog.Logger

	// tickInterval: how often the scheduler checks for due jobs.
	// 1 minute strikes a balance between responsiveness (an admin
	// who just toggled "enable" sees the run within 60 s) and load
	// (one SELECT-per-minute on a tiny indexed table is free).
	tickInterval time.Duration
	// runTimeout: cap per individual refresh call. EPG downloads can
	// legitimately take 2+ minutes for a 200 MB davidmuma dump, but
	// 10 min is a hard upper bound past which something is wrong and
	// we'd rather log an error than hang the worker.
	runTimeout time.Duration

	stopCh chan struct{}
	doneCh chan struct{}

	// rootCancel is the CancelFunc paired with the context the loop
	// derived from Start's ctx. Stop uses it as a last-resort lever
	// when the caller's shutdown deadline fires before the in-flight
	// run finishes naturally. Nil until Start runs.
	rootCancel context.CancelFunc

	// now abstracts time.Now for tests. Always UTC via
	// time.Now().UTC() in production.
	now func() time.Time
}

// NewScheduler wires a scheduler with sane production defaults.
func NewScheduler(repo *db.IPTVScheduleRepository, runner jobRunner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		repo:         repo,
		runner:       runner,
		logger:       logger.With("module", "iptv.scheduler"),
		tickInterval: 1 * time.Minute,
		runTimeout:   10 * time.Minute,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// Start launches the polling goroutine. Does not block. Call Stop to
// drain before the process exits. The context is wrapped with a cancel
// function so Stop can force-unblock a stalled run when the caller's
// shutdown deadline fires.
func (s *Scheduler) Start(ctx context.Context) {
	s.logger.Info("iptv scheduler started", "tick_interval", s.tickInterval)
	rootCtx, cancel := context.WithCancel(ctx)
	s.rootCancel = cancel
	go s.loop(rootCtx)
}

// Stop signals the loop to drain and waits until the current run (if
// any) finishes. The context bounds the wait: passing a deadline
// shorter than runTimeout cancels the in-flight runCtx so the goroutine
// returns promptly even when upstreams are stalled. Callers during
// graceful shutdown should pass the shutdownCtx so the supervisor
// deadline is honoured end-to-end.
func (s *Scheduler) Stop(ctx context.Context) {
	close(s.stopCh)
	select {
	case <-s.doneCh:
	case <-ctx.Done():
		// Propagate to any in-flight runCtx by cancelling the loop's
		// root context so the runner path unblocks. Then give the
		// goroutine a short grace to record outcome + exit; we still
		// return even if it can't.
		s.logger.Warn("iptv scheduler stop deadline reached, forcing cancel")
		s.rootCancel()
		select {
		case <-s.doneCh:
		case <-time.After(2 * time.Second):
			s.logger.Error("iptv scheduler did not drain within grace period")
		}
	}
	s.logger.Info("iptv scheduler stopped")
}

func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick runs one pass: list due jobs, run them, record outcomes. Errors
// are logged and swallowed — the scheduler must never panic the whole
// process because an upstream returned 500.
func (s *Scheduler) tick(ctx context.Context) {
	jobs, err := s.repo.ListDue(ctx, s.now())
	if err != nil {
		s.logger.Error("list due iptv jobs", "error", err)
		return
	}
	for _, job := range jobs {
		// Shutdown signalled mid-tick: stop starting new runs but let
		// the one in flight (if any) complete inside runOne.
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		// Tick-driven runs log their outcome inside runOne; the
		// error return exists for the synchronous RunNow path.
		_ = s.runOne(ctx, job)
	}
}

// RunNow fires a single job synchronously, bypassing the schedule.
// Used by the "Run now" admin button. Returns the refresh outcome and
// records it against the job row so the UI shows the new timestamp.
// If the job row doesn't exist the run still happens — an admin who
// hits "Run now" without having configured a schedule is a supported
// flow.
func (s *Scheduler) RunNow(ctx context.Context, libraryID, kind string) error {
	job, err := s.repo.Get(ctx, libraryID, kind)
	if err != nil && err != db.ErrIPTVScheduledJobNotFound {
		return fmt.Errorf("get iptv job: %w", err)
	}
	if job == nil {
		job = &db.IPTVScheduledJob{LibraryID: libraryID, Kind: kind}
	}
	return s.runOne(ctx, job)
}

// runOne invokes the right runner method for the job's kind and
// persists the outcome. Returns the run error (nil on success) so the
// RunNow caller can surface it to the HTTP client; ticker-driven runs
// ignore the return because they already logged.
//
// Panic safety: the runner executes third-party-ish code (XMLTV parser
// on user-supplied feeds, HTTP clients, etc.). A panic in that path
// must not kill the scheduler goroutine — it would silently stop every
// future refresh. defer/recover converts it into a recorded error so
// the admin sees "error: panic: …" instead of discovering days later
// that nothing ran. The surrounding duration + RecordRun still happen
// via the named return + cleanup pattern below.
func (s *Scheduler) runOne(ctx context.Context, job *db.IPTVScheduledJob) (runErr error) {
	runCtx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()

	startedAt := s.now()
	defer func() {
		if r := recover(); r != nil {
			// Turn the panic into a recorded error and keep the
			// goroutine alive. Log loudly so the admin's next
			// look at the logs surfaces the cause.
			runErr = fmt.Errorf("panic: %v", r)
			s.logger.Error("iptv scheduled job panicked",
				"library", job.LibraryID, "kind", job.Kind, "panic", r)
		}
		duration := s.now().Sub(startedAt)
		s.recordOutcome(ctx, job, runErr, duration, startedAt)
	}()

	switch job.Kind {
	case db.IPTVJobKindM3URefresh:
		_, runErr = s.runner.RefreshM3U(runCtx, job.LibraryID)
	case db.IPTVJobKindEPGRefresh:
		_, runErr = s.runner.RefreshEPG(runCtx, job.LibraryID)
	default:
		runErr = fmt.Errorf("unknown iptv job kind %q", job.Kind)
	}
	return runErr
}

// recordOutcome logs and persists a single run's result. Split out of
// runOne so the defer that handles panic + normal return can share one
// code path. ErrRefreshInProgress is benign (the per-library lock
// fired because an admin clicked "Run now" at the same moment the
// ticker dispatched, or vice versa) — log at info and still record a
// successful last_run_at so the UI doesn't show a spurious error badge,
// but skip updating last_status so the previous real outcome wins.
func (s *Scheduler) recordOutcome(
	ctx context.Context,
	job *db.IPTVScheduledJob,
	runErr error,
	duration time.Duration,
	startedAt time.Time,
) {
	if errors.Is(runErr, ErrRefreshInProgress) {
		s.logger.Info("iptv scheduled job skipped (concurrent refresh)",
			"library", job.LibraryID, "kind", job.Kind)
		return
	}

	status := "ok"
	errMsg := ""
	if runErr != nil {
		status = "error"
		errMsg = runErr.Error()
		s.logger.Warn("iptv scheduled job failed",
			"library", job.LibraryID, "kind", job.Kind,
			"duration_ms", duration.Milliseconds(), "error", runErr)
	} else {
		s.logger.Info("iptv scheduled job ran",
			"library", job.LibraryID, "kind", job.Kind,
			"duration_ms", duration.Milliseconds())
	}

	// Record even when the job row didn't exist at Get time: the
	// UPDATE is a no-op against zero rows, which is the correct
	// behaviour for a row-less RunNow.
	if recErr := s.repo.RecordRun(ctx, job.LibraryID, job.Kind, status, errMsg, duration, startedAt); recErr != nil {
		s.logger.Error("record iptv job run",
			"library", job.LibraryID, "kind", job.Kind, "error", recErr)
	}
}

// Compile-time guarantee that *iptv.Service satisfies jobRunner.
var _ jobRunner = (*Service)(nil)
