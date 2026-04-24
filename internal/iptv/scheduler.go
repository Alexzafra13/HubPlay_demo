package iptv

// Scheduler: background worker that drives periodic M3U and EPG
// refreshes. Reads enabled/due rows from iptv_scheduled_jobs and
// dispatches to iptv.Service. Models the same Start/Stop contract as
// library.Scheduler so main.go can sequence shutdown symmetrically.

import (
	"context"
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
// drain before the process exits.
func (s *Scheduler) Start(ctx context.Context) {
	s.logger.Info("iptv scheduler started", "tick_interval", s.tickInterval)
	go s.loop(ctx)
}

// Stop signals the loop to drain and waits until it finishes the
// currently-running job, if any. Bounded by the runTimeout of the
// active run — shutdown won't hang forever even if an upstream stalls.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
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
func (s *Scheduler) runOne(ctx context.Context, job *db.IPTVScheduledJob) error {
	runCtx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()

	startedAt := s.now()
	var runErr error

	switch job.Kind {
	case db.IPTVJobKindM3URefresh:
		_, runErr = s.runner.RefreshM3U(runCtx, job.LibraryID)
	case db.IPTVJobKindEPGRefresh:
		_, runErr = s.runner.RefreshEPG(runCtx, job.LibraryID)
	default:
		runErr = fmt.Errorf("unknown iptv job kind %q", job.Kind)
	}

	duration := s.now().Sub(startedAt)
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
	return runErr
}

// ── Test hooks ────────────────────────────────────────────────────

// tickOnce runs exactly one polling pass. Used by tests to avoid
// sleeping for tickInterval; production uses the internal loop.
func (s *Scheduler) tickOnce(ctx context.Context) { s.tick(ctx) }

// setTickInterval lets tests speed up the loop. Harmless in
// production (nothing calls it) but not part of the stable API.
func (s *Scheduler) setTickInterval(d time.Duration) { s.tickInterval = d }

// Compile-time guarantee that *iptv.Service satisfies jobRunner.
var _ jobRunner = (*Service)(nil)
