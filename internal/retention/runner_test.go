package retention_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/retention"
)

type fakeEPG struct {
	mu     sync.Mutex
	calls  int32
	last   time.Duration
	failOn int32 // call number that should error (1-indexed; 0 = never)
}

func (f *fakeEPG) CleanupOldPrograms(_ context.Context, window time.Duration) (int64, error) {
	n := atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.last = window
	f.mu.Unlock()
	if f.failOn != 0 && n == f.failOn {
		return 0, errors.New("simulated EPG failure")
	}
	return 7, nil
}

type fakeAudit struct {
	calls  int32
	failOn int32
}

func (f *fakeAudit) PruneAuditBefore(_ context.Context, _ time.Time) (int64, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if f.failOn != 0 && n == f.failOn {
		return 0, errors.New("simulated audit failure")
	}
	return 3, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// TestRunner_RunsBothCleanersOnStartup pins the contract that the
// initial sweep happens immediately on Start so a fresh restart
// reflects the configured retention window without waiting a full
// interval.
func TestRunner_RunsBothCleanersOnStartup(t *testing.T) {
	epg := &fakeEPG{}
	audit := &fakeAudit{}
	cfg := config.RetentionConfig{
		EPGPrograms:        12 * time.Hour,
		FederationAuditLog: 7 * 24 * time.Hour,
		SweepInterval:      24 * time.Hour, // very long; only the startup tick runs in the test
	}
	r := retention.New(cfg, epg, audit, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	t.Cleanup(r.Stop)

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&epg.calls) == 0 || atomic.LoadInt32(&audit.calls) == 0 {
		select {
		case <-deadline:
			t.Fatalf("startup sweep did not run within 2s (epg=%d audit=%d)",
				epg.calls, audit.calls)
		case <-time.After(10 * time.Millisecond):
		}
	}

	epg.mu.Lock()
	if epg.last != cfg.EPGPrograms {
		t.Errorf("EPG window passed through wrong: want %v, got %v", cfg.EPGPrograms, epg.last)
	}
	epg.mu.Unlock()
}

// TestRunner_DisabledOnZeroInterval guards the operator-disable knob:
// setting sweep_interval <= 0 must NOT spin a 0-duration ticker (Go
// panics) and must NOT invoke either cleaner.
func TestRunner_DisabledOnZeroInterval(t *testing.T) {
	epg := &fakeEPG{}
	audit := &fakeAudit{}
	cfg := config.RetentionConfig{
		EPGPrograms:        24 * time.Hour,
		FederationAuditLog: 30 * 24 * time.Hour,
		SweepInterval:      0,
	}
	r := retention.New(cfg, epg, audit, quietLogger())
	r.Start(context.Background())
	t.Cleanup(r.Stop)

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&epg.calls); got != 0 {
		t.Errorf("EPG cleanup must not run when interval=0, got %d calls", got)
	}
	if got := atomic.LoadInt32(&audit.calls); got != 0 {
		t.Errorf("audit prune must not run when interval=0, got %d calls", got)
	}
}

// TestRunner_NilDepsAreSafe — operators who run without IPTV or
// federation must still be able to wire the runner without nil panics.
func TestRunner_NilDepsAreSafe(t *testing.T) {
	cfg := config.RetentionConfig{
		EPGPrograms:        24 * time.Hour,
		FederationAuditLog: 30 * 24 * time.Hour,
		SweepInterval:      24 * time.Hour,
	}
	r := retention.New(cfg, nil, nil, quietLogger())
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	// Just sleep long enough for startup tick to fire — it must not panic.
	time.Sleep(50 * time.Millisecond)
}

// TestRunner_OneCleanerFailureDoesNotBlockTheOther — independent paths
// (EPG and federation audit) must not chain failures.
func TestRunner_OneCleanerFailureDoesNotBlockTheOther(t *testing.T) {
	epg := &fakeEPG{failOn: 1}
	audit := &fakeAudit{}
	cfg := config.RetentionConfig{
		EPGPrograms:        24 * time.Hour,
		FederationAuditLog: 30 * 24 * time.Hour,
		SweepInterval:      24 * time.Hour,
	}
	r := retention.New(cfg, epg, audit, quietLogger())
	r.Start(context.Background())
	t.Cleanup(r.Stop)

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&audit.calls) == 0 {
		select {
		case <-deadline:
			t.Fatalf("audit prune was skipped after EPG failure (calls=%d)", audit.calls)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
