// Package retention runs the daily background sweep that prunes
// append-only diagnostic and programming tables. Without it the EPG
// programmes table and the federation audit log grow monotonically,
// turning a long-lived single-tenant install into a multi-GB SQLite
// over weeks.
//
// The Runner is intentionally tiny: it owns a ticker, calls the two
// existing repository-level cleaners, and logs counts. No retry, no
// backoff — a missed tick is harmless because the next tick re-runs
// the whole sweep. It does not depend on the IPTV or federation
// packages directly so the wiring layer in cmd/hubplay can compose
// it without pulling in import cycles.
package retention

import (
	"context"
	"log/slog"
	"time"

	"hubplay/internal/config"
)

// EPGCleaner is the subset of the IPTV service the runner needs.
type EPGCleaner interface {
	CleanupOldPrograms(ctx context.Context, window time.Duration) (int64, error)
}

// AuditPruner is the subset of the federation repository the runner needs.
type AuditPruner interface {
	PruneAuditBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Runner sweeps EPG and federation audit rows on a fixed cadence.
// All dependencies are nil-safe so an operator who runs without IPTV
// or federation can still wire the runner without panics.
type Runner struct {
	cfg    config.RetentionConfig
	epg    EPGCleaner
	audit  AuditPruner
	logger *slog.Logger
	stopCh chan struct{}
}

// New builds a runner. epg and audit may be nil — the runner skips the
// corresponding sweep when its dependency is missing.
func New(cfg config.RetentionConfig, epg EPGCleaner, audit AuditPruner, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		cfg:    cfg,
		epg:    epg,
		audit:  audit,
		logger: logger.With("module", "retention"),
		stopCh: make(chan struct{}),
	}
}

// Start kicks off the background loop. It blocks the caller for the
// initial sweep so a fresh restart immediately reflects the
// configured retention window in observability dashboards. Subsequent
// sweeps run on the configured interval.
//
// Use Stop or cancel ctx to terminate.
func (r *Runner) Start(ctx context.Context) {
	interval := r.cfg.SweepInterval
	if interval <= 0 {
		// Defensive: refuse to spin a 0-duration ticker. An operator
		// who explicitly disables retention should not also lose the
		// ticker goroutine to a runtime panic.
		r.logger.Info("retention disabled: sweep_interval <= 0")
		return
	}

	go func() {
		// Run once at startup so the deletion happens immediately on
		// boot rather than waiting a full interval. Fits the same
		// pattern as auth.StartSessionCleaner.
		r.sweep(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.sweep(ctx)
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts the background loop. Safe to call once.
func (r *Runner) Stop() { close(r.stopCh) }

// sweep runs a single retention pass. Each cleaner is independent —
// if EPG fails we still try federation audit and vice versa.
func (r *Runner) sweep(ctx context.Context) {
	if r.epg != nil && r.cfg.EPGPrograms > 0 {
		n, err := r.epg.CleanupOldPrograms(ctx, r.cfg.EPGPrograms)
		switch {
		case err != nil:
			r.logger.Warn("epg cleanup failed", "error", err)
		case n > 0:
			r.logger.Info("epg cleanup", "rows_deleted", n, "window", r.cfg.EPGPrograms)
		}
	}

	if r.audit != nil && r.cfg.FederationAuditLog > 0 {
		cutoff := time.Now().Add(-r.cfg.FederationAuditLog)
		n, err := r.audit.PruneAuditBefore(ctx, cutoff)
		switch {
		case err != nil:
			r.logger.Warn("federation audit prune failed", "error", err)
		case n > 0:
			r.logger.Info("federation audit prune", "rows_deleted", n, "window", r.cfg.FederationAuditLog)
		}
	}
}
