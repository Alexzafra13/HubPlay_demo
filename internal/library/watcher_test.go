package library_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/library"
	"hubplay/internal/probe"
	"hubplay/internal/scanner"
	"hubplay/internal/testutil"
)

// scanCounter subscribes to LibraryScanCompleted events on the
// given bus and exposes a thread-safe count of how many scans for
// `libID` have finished. Tests use it to assert "exactly one scan
// fired" or "scan was triggered at all", which is a more reliable
// signal than polling IsScanning — the mock scanner can finish a
// scan in <1 ms, well below any reasonable poll interval.
type scanCounter struct {
	mu     sync.Mutex
	count  int
	libID  string
	notify chan struct{}
}

func newScanCounter(bus *event.Bus, libID string) *scanCounter {
	c := &scanCounter{libID: libID, notify: make(chan struct{}, 32)}
	bus.Subscribe(event.LibraryScanCompleted, func(e event.Event) {
		id, _ := e.Data["library_id"].(string)
		if id != libID {
			return
		}
		c.mu.Lock()
		c.count++
		c.mu.Unlock()
		select {
		case c.notify <- struct{}{}:
		default:
		}
	})
	return c
}

func (c *scanCounter) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// waitForScan blocks until at least `n` scans have completed (or
// the timeout elapses). Returns true if the threshold was reached.
func (c *scanCounter) waitForScan(n int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if c.total() >= n {
			return true
		}
		select {
		case <-c.notify:
		case <-deadline:
			return c.total() >= n
		}
	}
}

// newTestServiceWithRoot wires a real Service against a temp DB and
// seeds one auto-mode library whose `paths` is the supplied dir.
// Returns the library id, plus the event bus so tests can attach a
// scanCounter and assert on completion events.
func newTestServiceWithRoot(t *testing.T, root string) (*library.Service, string, *event.Bus) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(slog.Default())
	prober := &watcherTestProber{}
	scnr := scanner.New(
		repos.Items, repos.MediaStreams, repos.Metadata, repos.ExternalIDs,
		repos.Images, repos.Chapters, repos.People, repos.ItemValues,
		repos.Studios, repos.Collections, repos.ItemMetadataLocks,
		nil, prober, bus, "", nil, slog.Default(),
	)
	svc := library.NewService(
		repos.Libraries, repos.Items, repos.MediaStreams, repos.Images,
		repos.Channels, repos.ItemValues, scnr, slog.Default(),
	)
	t.Cleanup(svc.Shutdown)

	now := time.Now()
	if err := repos.Libraries.Create(context.Background(), &librarymodel.Library{
		ID: "lib-watch", Name: "Watch", ContentType: "movies",
		ScanMode: "auto", ScanInterval: "6h",
		CreatedAt: now, UpdatedAt: now, Paths: []string{root},
	}); err != nil {
		t.Fatalf("create lib: %v", err)
	}
	return svc, "lib-watch", bus
}

type watcherTestProber struct{}

func (p *watcherTestProber) Probe(ctx context.Context, path string) (*probe.Result, error) {
	return &probe.Result{
		Format: probe.Format{Size: 1024, FormatName: "matroska,webm"},
	}, nil
}

func TestFSWatcher_StartStop_Idempotent(t *testing.T) {
	dir := t.TempDir()
	svc, _, _ := newTestServiceWithRoot(t, dir)
	w := library.NewFSWatcher(svc, slog.Default())
	w.SetDebounce(50 * time.Millisecond)
	w.SetReconcileEvery(50 * time.Millisecond)

	if err := w.Start(context.Background()); err != nil {
		t.Skipf("fsnotify unsupported on this platform: %v", err)
	}
	w.Stop()
	w.Stop() // idempotent — second call must be a no-op, not panic.
}

// TestFSWatcher_NewFileTriggersScan: the headline contract — drop a
// file into a watched library path, the watcher should debounce and
// fire a scan within a few seconds.
func TestFSWatcher_NewFileTriggersScan(t *testing.T) {
	dir := t.TempDir()
	svc, libID, bus := newTestServiceWithRoot(t, dir)
	counter := newScanCounter(bus, libID)
	w := library.NewFSWatcher(svc, slog.Default())
	w.SetDebounce(80 * time.Millisecond)
	w.SetReconcileEvery(60 * time.Second) // disable mid-test reconcile

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Skipf("fsnotify unsupported on this platform: %v", err)
	}
	defer w.Stop()

	// Reconcile fires inside Start at t=0; allow a moment for the
	// initial walk to register watches before we drop the file.
	time.Sleep(100 * time.Millisecond)

	moviePath := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(moviePath, []byte("fake-mkv-bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if !counter.waitForScan(1, 3*time.Second) {
		t.Fatalf("expected watcher to trigger a scan after file write")
	}
}

// TestFSWatcher_DebounceCoalescesBurst: a burst of writes (simulating
// a chunked copy of a large file) must produce ONE scan, not N.
func TestFSWatcher_DebounceCoalescesBurst(t *testing.T) {
	dir := t.TempDir()
	svc, libID, bus := newTestServiceWithRoot(t, dir)
	counter := newScanCounter(bus, libID)
	w := library.NewFSWatcher(svc, slog.Default())
	w.SetDebounce(150 * time.Millisecond)
	w.SetReconcileEvery(60 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Skipf("fsnotify unsupported on this platform: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond) // initial walk

	// Drop 10 files in rapid succession — the debounce window must
	// collapse all of them into a single scan.
	for i := 0; i < 10; i++ {
		f := filepath.Join(dir, "f"+string(rune('a'+i))+".mkv")
		if err := os.WriteFile(f, []byte{0, 1}, 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	if !counter.waitForScan(1, 3*time.Second) {
		t.Fatalf("expected at least one scan after the burst")
	}
	// Allow a generous buffer beyond the debounce window so any
	// stragglers would have fired by now.
	time.Sleep(500 * time.Millisecond)
	if got := counter.total(); got != 1 {
		t.Errorf("expected exactly 1 scan, got %d — burst was not coalesced", got)
	}
}

// TestFSWatcher_ReconcileLazyOnUnchangedLibraries: the periodic
// reconcile tick must NOT walk the library tree when nothing
// changed. The cost of the watcher on a stable deployment should be
// O(libraries) per tick, not O(directories) per tick — that's the
// difference between "free" and "measurable on big collections".
//
// Strategy: start the watcher (initial reconcile = 1 walk), force a
// few extra reconcile ticks while the library set is unchanged,
// assert that walksDone never grew past the initial 1.
func TestFSWatcher_ReconcileLazyOnUnchangedLibraries(t *testing.T) {
	dir := t.TempDir()
	svc, _, _ := newTestServiceWithRoot(t, dir)
	w := library.NewFSWatcher(svc, slog.Default())
	w.SetDebounce(60 * time.Second)         // disable debouncer for this test
	w.SetReconcileEvery(50 * time.Millisecond) // tick fast

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Skipf("fsnotify unsupported on this platform: %v", err)
	}
	defer w.Stop()

	// Wait for the startup reconcile to complete and a few periodic
	// ticks to fire. 250 ms at 50 ms/tick = ~5 ticks.
	time.Sleep(250 * time.Millisecond)

	got := w.WalksDone()
	if got != 1 {
		t.Fatalf("expected exactly 1 tree walk after %d periodic ticks (only the startup walk), got %d", 5, got)
	}
}

// TestFSWatcher_NewSubdirGetsWatched: a directory created after the
// watcher started should be added to the watch set as soon as its
// CREATE event fires, so a file dropped in the new subdir still
// triggers a scan.
func TestFSWatcher_NewSubdirGetsWatched(t *testing.T) {
	root := t.TempDir()
	svc, libID, bus := newTestServiceWithRoot(t, root)
	counter := newScanCounter(bus, libID)
	w := library.NewFSWatcher(svc, slog.Default())
	w.SetDebounce(80 * time.Millisecond)
	w.SetReconcileEvery(60 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Skipf("fsnotify unsupported on this platform: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond) // initial walk

	// Create a subdir, then drop a file in it. The mkdir alone
	// triggers a scan via the parent-dir watch; we'll see that
	// counted toward `priorScans` before checking the deep-file
	// scan landed.
	sub := filepath.Join(root, "Movies", "2026")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	counter.waitForScan(1, 1*time.Second)
	time.Sleep(200 * time.Millisecond) // let the inner-subdir watch attach
	priorScans := counter.total()

	deepFile := filepath.Join(sub, "movie.mkv")
	if err := os.WriteFile(deepFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write deep: %v", err)
	}

	if !counter.waitForScan(priorScans+1, 3*time.Second) {
		t.Fatalf("expected another scan after file write in newly-created subdir (had %d, never reached %d)", priorScans, priorScans+1)
	}
}
