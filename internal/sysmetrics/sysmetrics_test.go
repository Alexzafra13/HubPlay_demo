package sysmetrics

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"
)

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestSnapshot_BeforeStart_ReturnsRuntimeFallback pins that a caller
// reading Snapshot() on a freshly-constructed sampler (i.e. before
// Start has run the probes) doesn't get a zero-value struct — at
// minimum the logical core count is populated so the panel can render
// the "X cores" pill while the slower probes are still running.
func TestSnapshot_BeforeStart_ReturnsRuntimeFallback(t *testing.T) {
	s := New(0, newQuietLogger())
	snap := s.Snapshot()
	if snap.CPUCoresLogical <= 0 {
		t.Errorf("CPUCoresLogical before Start should fall back to runtime.NumCPU(); got %d", snap.CPUCoresLogical)
	}
	if snap.CPUCoresLogical != runtime.NumCPU() {
		t.Errorf("CPUCoresLogical pre-Start should equal runtime.NumCPU(); got %d want %d",
			snap.CPUCoresLogical, runtime.NumCPU())
	}
}

// TestSampler_Start_PopulatesStaticFields is an integration test
// against the real gopsutil — it must run inside a container or a
// real host. Assertions are intentionally loose ("non-empty model
// string", "RAM total > 0") because the values vary per host. The
// point is to catch a regression where the probes silently return
// blank values on a platform we don't notice.
func TestSampler_Start_PopulatesStaticFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping gopsutil integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	s := New(time.Hour, newQuietLogger()) // long interval — we only care about the boot probe
	s.Start(ctx)
	snap := s.Snapshot()

	if snap.CPUModel == "" {
		t.Errorf("CPUModel empty after Start — gopsutil cpu.Info() probably failed")
	}
	if snap.CPUCoresLogical <= 0 {
		t.Errorf("CPUCoresLogical not populated: %d", snap.CPUCoresLogical)
	}
	if snap.RAMTotalBytes == 0 {
		t.Errorf("RAMTotalBytes is zero — mem.VirtualMemory failed")
	}
	// CPU% must be a valid percentage. Don't assert >0: a perfectly
	// idle CI runner could return very small values that round to 0,
	// or the very first sample can come back as 0 even with the
	// 250 ms interval Start uses.
	if snap.CPUPercent < 0 || snap.CPUPercent > 100 {
		t.Errorf("CPUPercent out of range [0, 100]: %f", snap.CPUPercent)
	}
}

// TestSampler_Snapshot_AtomicReadWrite exercises the atomic.Value
// snapshot from two goroutines — sampler ticking while a reader
// hammers Snapshot(). Race detector catches torn reads if the
// atomic guarantee ever regresses.
func TestSampler_Snapshot_AtomicReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	s := New(50*time.Millisecond, newQuietLogger())
	s.Start(ctx)

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Snapshot()
			}
		}
	}()
	<-ctx.Done()
	close(stop)
}

// TestAtoiUint covers the local nvidia-smi parser helper. Cheap unit
// tests for arithmetic — would be wasteful as integration.
func TestAtoiUint(t *testing.T) {
	good := map[string]uint64{
		"0":      0,
		"1":      1,
		"12345":  12345,
		"999999": 999999,
	}
	for in, want := range good {
		got, err := atoiUint(in)
		if err != nil {
			t.Errorf("atoiUint(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("atoiUint(%q) = %d want %d", in, got, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.5", "-1", "12a", " 5"} {
		if _, err := atoiUint(bad); err == nil {
			t.Errorf("atoiUint(%q) should error", bad)
		}
	}
}
