// Package retention: sweep diario de tablas append-only (EPG + audit federation).
// Sin esto, en semanas el SQLite crece a varios GB. Runner mínimo: ticker + 2
// cleaners + log; sin retry/backoff (un tick perdido lo arregla el siguiente).
package retention

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"hubplay/internal/config"
)

type EPGCleaner interface {
	CleanupOldPrograms(ctx context.Context, window time.Duration) (int64, error)
}

type AuditPruner interface {
	PruneAuditBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Runner: sweep periódico de EPG + audit federation. Dependencias nil-safe
// (operador sin IPTV o sin federation puede cablear esto sin pánicos).
type Runner struct {
	cfg    config.RetentionConfig
	epg    EPGCleaner
	audit  AuditPruner
	logger *slog.Logger
	stopCh chan struct{}

	sweepsDone       atomic.Int64
	sweepsDoneNotify chan struct{}
}

// New: epg y audit pueden ser nil — se omite el sweep correspondiente.
func New(cfg config.RetentionConfig, epg EPGCleaner, audit AuditPruner, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		cfg:              cfg,
		epg:              epg,
		audit:            audit,
		logger:           logger.With("module", "retention"),
		stopCh:           make(chan struct{}),
		sweepsDoneNotify: make(chan struct{}, 32),
	}
}

// Start: lanza el loop en background. El primer sweep corre síncrono al boot
// para que el dashboard refleje ya la retention window. Stop o cancel del ctx
// para terminar.
func (r *Runner) Start(ctx context.Context) {
	interval := r.cfg.SweepInterval
	if interval <= 0 {
		// interval=0 desactiva retention; no spawneamos ticker (ticker(0) panic).
		r.logger.Info("retention disabled: sweep_interval <= 0")
		return
	}

	go func() {
		// Sweep inicial síncrono — mismo patrón que auth.StartSessionCleaner.
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

// Stop: idempotente sólo si se llama una vez.
func (r *Runner) Stop() { close(r.stopCh) }

// sweep: cada cleaner es independiente — si EPG falla, audit sigue, y viceversa.
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

	r.sweepsDone.Add(1)
	select {
	case r.sweepsDoneNotify <- struct{}{}:
	default:
	}
}

// WaitForSweep blocks until at least n sweeps have completed (or
// timeout elapses). Test-only.
func (r *Runner) WaitForSweep(n int64, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if r.sweepsDone.Load() >= n {
			return true
		}
		select {
		case <-r.sweepsDoneNotify:
		case <-deadline:
			return r.sweepsDone.Load() >= n
		}
	}
}
