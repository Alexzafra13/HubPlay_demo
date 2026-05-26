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

	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
)

// fakeLibLister returns a fixed list. Used to drive the worker
// without spinning up a real DB.
type fakeLibLister struct {
	libs []*librarymodel.Library
	err  error

	mu       sync.Mutex
	listed   int
	notify   chan struct{} // opcional; señaliza cada List() para tests.
}

func (f *fakeLibLister) List(_ context.Context) ([]*librarymodel.Library, error) {
	f.mu.Lock()
	f.listed++
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	return f.libs, f.err
}

// fakeChanLister returns per-library channels. Tracks calls so
// tests can assert which libraries the worker walks.
type fakeChanLister struct {
	mu       sync.Mutex
	byLib    map[string][]*iptvmodel.Channel
	calls    map[string]int
	failOnce bool
	failErr  error
	// notify señaliza cada ListByLibrary; nil = sin notificación.
	notify chan struct{}
}

func (f *fakeChanLister) ListByLibrary(_ context.Context, libraryID string, _ bool) ([]*iptvmodel.Channel, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[libraryID]++
	failOnce := f.failOnce
	if failOnce {
		f.failOnce = false
	}
	failErr := f.failErr
	byLib := f.byLib[libraryID]
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	if failOnce {
		return nil, failErr
	}
	return byLib, nil
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
	libs := &fakeLibLister{libs: []*librarymodel.Library{
		{ID: "L1", ContentType: "movies"},
		{ID: "L2", ContentType: "livetv"},
		{ID: "L3", ContentType: "shows"},
		{ID: "L4", ContentType: "livetv"},
	}}
	chans := &fakeChanLister{
		byLib: map[string][]*iptvmodel.Channel{
			"L2": {{ID: "c1", StreamURL: ""}},
			"L4": {{ID: "c2", StreamURL: ""}},
		},
		notify: make(chan struct{}, 32),
	}
	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	w.SetInterval(time.Hour)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// Espera al run inicial: 2 librerías livetv ⇒ 2 ListByLibrary.
	deadline := time.After(2 * time.Second)
	for chans.callCount("L2") == 0 || chans.callCount("L4") == 0 {
		select {
		case <-chans.notify:
		case <-deadline:
			t.Fatalf("initial run timed out: L2=%d L4=%d",
				chans.callCount("L2"), chans.callCount("L4"))
		}
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
	libs := &fakeLibLister{libs: []*librarymodel.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &fakeChanLister{
		byLib:  map[string][]*iptvmodel.Channel{"L1": {}},
		notify: make(chan struct{}, 32),
	}

	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	w.SetInterval(40 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// Esperamos ≥ 3 calls (1 inicial + ~5 ticks dentro de 500 ms).
	deadline := time.After(500 * time.Millisecond)
	for chans.callCount("L1") < 3 {
		select {
		case <-chans.notify:
		case <-deadline:
			t.Fatalf("worker did not tick: calls=%d", chans.callCount("L1"))
		}
	}
}

func TestProberWorker_StopDrainsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{libs: []*librarymodel.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &fakeChanLister{byLib: map[string][]*iptvmodel.Channel{"L1": {}}}

	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
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
	libs := &fakeLibLister{libs: []*librarymodel.Library{{ID: "L1", ContentType: "livetv"}}}
	chans := &blockingChanLister{hang: hang, called: make(chan struct{}, 1)}

	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	w.SetInterval(time.Hour)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())

	// Espera a que el loop entre en la llamada in-flight ListByLibrary.
	select {
	case <-chans.called:
	case <-time.After(time.Second):
		t.Fatal("worker never entered ListByLibrary")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = w.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded when run is hung, got %v", err)
	}
}

type blockingChanLister struct {
	hang   chan struct{}
	called chan struct{} // señaliza una vez al entrar en ListByLibrary.
}

// ListByLibrary señaliza `called` (la primera vez) y bloquea en `hang`,
// ignorando ctx — simula un downstream wedged para forzar a Stop a
// depender de su propio deadline.
func (b *blockingChanLister) ListByLibrary(_ context.Context, _ string, _ bool) ([]*iptvmodel.Channel, error) {
	select {
	case b.called <- struct{}{}:
	default:
	}
	<-b.hang
	return nil, nil
}

func TestProberWorker_LibraryListErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{err: errors.New("db down"), notify: make(chan struct{}, 32)}
	chans := &fakeChanLister{}
	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	w.SetInterval(20 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	// Confirma que el worker sigue tickeando tras el error inicial —
	// esperamos al menos 3 List() (1 inicial + 2 ticks).
	deadline := time.After(2 * time.Second)
	got := 0
	for got < 3 {
		select {
		case <-libs.notify:
			got++
		case <-deadline:
			t.Fatalf("worker did not tick after libs.List error: got %d List() calls", got)
		}
	}
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
	notify := make(chan struct{}, 32)
	libs := &panicLibLister{calls: &calls, notify: notify}
	chans := &fakeChanLister{}
	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	w.SetInterval(20 * time.Millisecond)
	w.SetInitialDelay(time.Millisecond)
	w.Start(context.Background())
	defer func() { _ = w.Stop(context.Background()) }()

	deadline := time.After(500 * time.Millisecond)
	for calls.Load() < 3 {
		select {
		case <-notify:
		case <-deadline:
			t.Fatalf("worker died after panic: calls=%d", calls.Load())
		}
	}
}

type panicLibLister struct {
	calls  *atomic.Int32
	notify chan struct{}
}

func (p *panicLibLister) List(_ context.Context) ([]*librarymodel.Library, error) {
	p.calls.Add(1)
	if p.notify != nil {
		select {
		case p.notify <- struct{}{}:
		default:
		}
	}
	panic("boom")
}

func TestProberWorker_ProbeNowReturnsListError(t *testing.T) {
	t.Parallel()
	libs := &fakeLibLister{}
	chans := &fakeChanLister{
		byLib:    map[string][]*iptvmodel.Channel{},
		failOnce: true,
		failErr:  errors.New("table missing"),
	}
	w, err := NewProberWorker(newCheapProber(), libs, chans, quietLogger())
	if err != nil {
		t.Fatalf("NewProberWorker: %v", err)
	}
	_, err = w.ProbeNow(context.Background(), "L1")
	if err == nil || !errors.Is(err, err) {
		t.Fatalf("expected ListByLibrary error, got %v", err)
	}
}
