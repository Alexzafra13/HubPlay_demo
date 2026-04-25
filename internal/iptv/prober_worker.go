package iptv

// ProberWorker periodically walks every livetv library and probes
// every channel. Lives in its own goroutine so the rest of the
// service stays responsive; ticks at a fixed cadence (default 6 h)
// because per-library probe scheduling adds operational complexity
// without buying much — the probe is cheap and a global cadence is
// what an operator actually understands.
//
// Lifecycle mirrors iptv.Scheduler: Start launches the loop, Stop
// drains the in-flight run and returns. Both honour the caller's
// context so a shutdown deadline is respected end-to-end.
//
// Triggers
//
//   - Periodic: every `interval` (default 6 h). The first run fires
//     `initialDelay` after Start to avoid hammering the disk
//     immediately at boot when SQLite WAL is still warming up and
//     metrics are being scraped.
//   - On-demand: ProbeNow(libraryID) is exposed for the M3U-refresh
//     code path (run a fresh probe right after channels change) and
//     for the future admin "probar canales" button.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hubplay/internal/db"
)

const (
	// proberDefaultInterval — how often the worker re-probes every
	// livetv library. Chosen so that a channel which goes dark gets
	// flagged within a single hour at most:
	//   30 min × UnhealthyThreshold(3) = 1.5 h to "dead"
	//   30 min × 1 failure              = ~30 min to "degraded"
	// Earlier we ran every 6 h, which meant 18 h to surface a dead
	// channel — useless for an admin trying to keep the grid tidy.
	// 269 channels × 6 s timeout / 8 concurrent ≈ 3.4 min per cycle,
	// so 30 min leaves plenty of headroom even for a 1k-channel list.
	proberDefaultInterval     = 30 * time.Minute
	proberDefaultInitialDelay = 90 * time.Second
	// proberRunTimeout caps a single full-library probe walk. Even
	// large libraries finish in a few minutes with concurrency=8;
	// 30 m is the upper bound for "something is hung".
	proberRunTimeout = 30 * time.Minute
)

// ProberWorker is the long-running periodic prober. Build with
// NewProberWorker, Start once, Stop on shutdown.
type ProberWorker struct {
	prober    *Prober
	libraries libraryLister
	channels  channelLister
	logger    *slog.Logger

	interval     time.Duration
	initialDelay time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	doneCh  chan struct{}
}

// libraryLister and channelLister are local minimal interfaces so
// the worker depends on capability, not on the concrete repository
// types. Mirrors the sink-pattern used elsewhere (signingKeys,
// MetricsSink).
type libraryLister interface {
	List(ctx context.Context) ([]*db.Library, error)
}

type channelLister interface {
	ListByLibrary(ctx context.Context, libraryID string, activeOnly bool) ([]*db.Channel, error)
}

// NewProberWorker wires a worker around the building blocks. Logger
// is required (a silent worker is a debugging nightmare); panics on
// nil.
func NewProberWorker(prober *Prober, libraries libraryLister, channels channelLister, logger *slog.Logger) *ProberWorker {
	if prober == nil || libraries == nil || channels == nil || logger == nil {
		panic("iptv.NewProberWorker: nil dependency")
	}
	return &ProberWorker{
		prober:       prober,
		libraries:    libraries,
		channels:     channels,
		logger:       logger.With("module", "iptv-prober"),
		interval:     proberDefaultInterval,
		initialDelay: proberDefaultInitialDelay,
	}
}

// SetInterval overrides the periodic-tick interval. Tests use a tiny
// value (50 ms) to drive the loop manually; production sticks to the
// default.
func (w *ProberWorker) SetInterval(d time.Duration) {
	if d > 0 {
		w.interval = d
	}
}

// SetInitialDelay overrides the post-Start delay before the first
// run. Tests typically pass a few ms.
func (w *ProberWorker) SetInitialDelay(d time.Duration) {
	if d >= 0 {
		w.initialDelay = d
	}
}

// Start launches the worker goroutine. Idempotent: a second call
// while already running is a no-op (logged at warn).
func (w *ProberWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		w.logger.Warn("prober worker already running")
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.doneCh = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	go w.loop(runCtx)
}

// Stop signals the worker to exit and waits for the in-flight run
// to drain — bounded by the caller's ctx. If ctx expires before the
// drain finishes, Stop forces a cancel and returns ctx.Err().
func (w *ProberWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	cancel := w.cancel
	doneCh := w.doneCh
	w.running = false
	w.mu.Unlock()

	cancel()
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ProbeNow runs a single probe pass against one library, bypassing
// the periodic schedule. Used by the M3U-refresh code path and the
// admin "probar ahora" button. Returns the run summary and any
// orchestration error (DB read, ctx cancel) — individual probe
// errors are folded into the summary, not surfaced.
func (w *ProberWorker) ProbeNow(ctx context.Context, libraryID string) (ProbeSummary, error) {
	channels, err := w.channels.ListByLibrary(ctx, libraryID, true)
	if err != nil {
		return ProbeSummary{}, fmt.Errorf("list channels: %w", err)
	}
	return w.prober.ProbeChannels(ctx, channels), nil
}

func (w *ProberWorker) loop(ctx context.Context) {
	defer func() {
		w.mu.Lock()
		close(w.doneCh)
		w.mu.Unlock()
	}()

	// Initial delay. A select on ctx.Done so a fast Stop wins.
	select {
	case <-ctx.Done():
		return
	case <-time.After(w.initialDelay):
	}

	// Tick forever (until ctx done). We DON'T align on the wall
	// clock — interval-relative is fine because the prober's job
	// is "eventually consistent freshness", not "fire at 06:00".
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// runOnce wraps one full pass with panic recovery and a per-run
// timeout. A panic from any prober codepath is converted to an
// error log and the worker keeps running — same lesson as the
// IPTV Scheduler review fix.
func (w *ProberWorker) runOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, proberRunTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("prober panic recovered", "panic", fmt.Sprintf("%v", r))
		}
	}()

	libraries, err := w.libraries.List(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		w.logger.Warn("list libraries", "error", err)
		return
	}

	for _, lib := range libraries {
		if ctx.Err() != nil {
			return
		}
		if lib.ContentType != "livetv" {
			continue
		}
		summary, err := w.ProbeNow(ctx, lib.ID)
		if err != nil {
			w.logger.Warn("probe library", "library", lib.ID, "error", err)
			continue
		}
		w.logger.Info("probe complete",
			"library", lib.ID,
			"total", summary.Total,
			"ok", summary.OK,
			"failed", summary.Failed,
			"skipped", summary.Skipped,
		)
	}
}
