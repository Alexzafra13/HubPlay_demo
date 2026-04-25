package iptv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/db"
)

// fakeLibLister returns a fixed list. Used to drive the worker
// without spinning up a real DB.
type fakeLibLister struct {
	libs []*db.Library
	err  error
}

func (f *fakeLibLister) List(_ context.Context) ([]*db.Library, error) {
	return f.libs, f.err
}

// fakeChanLister returns per-library channels. Tracks calls so
// tests can assert which libraries the worker walks.
type fakeChanLister struct {
	mu       sync.Mutex
	byLib    map[string][]*db.Channel
	calls    map[string]int
	failOnce bool
	failErr  error
}

func (f *fakeChanLister) ListByLibrary(_ context.Context, libraryID string, _ bool) ([]*db.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[libraryID]++
	if f.failOnce {
		f.failOnce = false
		return nil, f.failErr
	}
	return f.byLib[libraryID], nil
}

func (f *fakeChanLister) callCount(lib string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[lib]
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// proberStub is a Prober substitute. We can't easily fake the real
// *Prober because ProbeChannels takes a concrete type, so the tests
// drive the public surface via NewProber + a no-op reporter and
// rely on a httptest server inside ProbeNow to keep things hermetic.
// For worker-loop tests we reuse the real Prober but feed it
// channels with empty URLs (instant failure) so each library
// "completes" quickly.
func newCheapProber() *Prober {
	rep := &fakeReporter{ok: map[string]int{}, fails: map[string]error{}}
	p := NewProber(nil, rep)
	p.SetTimeout(50 * time.Millisecond)
	p.SetConcurrency(2)
	return p
}

func TestProberWorker_OnlyLivetvLibrariesAreProbed(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{libs: []*db.Library{
		{ID: "L1", ContentType: "movies"},
		{ID: "L2", ContentType: "livetv"},
		{ID: "L3", ContentType: "shows"},
		{ID: "L4", ContentType: "livetv"},
	}}
	chans := &fakeChanLister{byLib: map[string][]*db.Channel{
		"L2": {{ID: "c1", StreamURL: ""}},
		"L4": {{ID: "c2", StreamURL: ""}},
	}}
	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(time.Hour)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// Wait for the initial run to finish (initial delay + one pass).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if chans.callCount("L2") > 0 && chans.callCount("L4") > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if chans.callCount("L1") != 0 || chans.callCount("L3") != 0 {
		t.Fatalf("non-livetv libraries must be skipped: L1=%d L3=%d",
			chans.callCount("L1"), chans.callCount("L3"))
	}
	if chans.callCount("L2") == 0 || chans.callCount("L4") == 0 {
		t.Fatalf("livetv libraries must be probed: L2=%d L4=%d",
			chans.callCount("L2"), chans.callCount("L4"))
	}
}

func TestProberWorker_TickRunsAfterInterval(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{libs: []*db.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &fakeChanLister{byLib: map[string][]*db.Channel{"L1": {}}}

	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(40 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// We expect at least 3 calls within ~250 ms (1 initial + ~5 ticks).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if chans.callCount("L1") >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker did not tick: calls=%d", chans.callCount("L1"))
}

func TestProberWorker_StopDrainsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{libs: []*db.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &fakeChanLister{byLib: map[string][]*db.Channel{"L1": {}}}

	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(time.Hour)
	w.SetInitialDelay(time.Hour) // never run a tick — keep the loop pure
	w.Start(context.Background())

	// First Stop returns nil. Second Stop is a no-op (no panic, no
	// hang, no error).
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop #1: %v", err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatalf("Stop #2 must be a no-op: %v", err)
	}
}

func TestProberWorker_StopHonoursDeadline(t *testing.T) {
	t.Parallel()
	// Block the channel-list call so a run is in-flight when we Stop.
	hang := make(chan struct{})
	defer close(hang)
	libs := &fakeLibLister{libs: []*db.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &blockingChanLister{hang: hang}

	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(time.Hour)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())

	// Give the loop time to enter the in-flight ListByLibrary call.
	time.Sleep(50 * time.Millisecond)

	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := w.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded when run is hung, got %v", err)
	}
}

type blockingChanLister struct {
	hang chan struct{}
}

// ListByLibrary blocks on `hang` and ignores ctx — that's the whole
// point: simulate a wedged downstream call so Stop must rely on its
// own deadline, not on ctx-cancellation propagating into the work.
func (b *blockingChanLister) ListByLibrary(_ context.Context, _ string, _ bool) ([]*db.Channel, error) {
	<-b.hang
	return nil, nil
}

func TestProberWorker_LibraryListErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{err: errors.New("db down")}
	chans := &fakeChanLister{}
	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(20 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// Just confirm the worker keeps running through a few ticks
	// without panicking — no channel calls should ever happen.
	time.Sleep(120 * time.Millisecond)
	if chans.callCount("anything") != 0 {
		t.Fatalf("no library-list response means no channel calls, got %v", chans.calls)
	}
}

func TestProberWorker_PanicInPrologueIsRecovered(t *testing.T) {
	t.Parallel()
	// A library lister that panics on each call simulates a bug
	// (or a corrupt DB row) inside the prologue. The worker must
	// log + continue, not exit the goroutine.
	var calls atomic.Int32
	libs := &panicLibLister{calls: &calls}
	chans := &fakeChanLister{}
	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	w.SetInterval(20 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker died after panic: calls=%d", calls.Load())
}

type panicLibLister struct {
	calls *atomic.Int32
}

func (p *panicLibLister) List(_ context.Context) ([]*db.Library, error) {
	p.calls.Add(1)
	panic("boom")
}

func TestProberWorker_ProbeNowReturnsListError(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{}
	chans := &fakeChanLister{
		byLib:    map[string][]*db.Channel{},
		failOnce: true,
		failErr:  errors.New("table missing"),
	}
	w := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	_, err := w.ProbeNow(context.Background(), "L1")
	if err == nil || !errors.Is(err, err) {
		t.Fatalf("expected ListByLibrary error, got %v", err)
	}
}
