package library

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"hubplay/internal/domain"
)

// FSWatcher reacts to filesystem events under each library's
// configured paths and triggers a library scan when a quiet period
// follows a burst of changes. Complement to the periodic Scheduler:
// the scheduler covers correctness (15-min sweep so nothing rots
// undiscovered), the watcher covers responsiveness (a file copied
// at 21:03:14 appears in the catalog around 21:03:16, not at the
// next 15-min tick).
//
// Design notes:
//
//   - **Cheap re-scan**: Scanner.ScanLibrary uses a per-file
//     fingerprint cache (sha256 of size + first 64 KiB) so re-running
//     it on a library where only one file changed is essentially
//     free for the unchanged files. We never need a per-file scan
//     entrypoint — calling Service.Scan(libraryID) is the right
//     primitive even when only one file moved.
//
//   - **Concurrent-burst dedup**: a 10 GiB copy fires hundreds of
//     filesystem events over a few seconds. We coalesce per-library
//     into one scan request after `debounce` seconds of quiet (2s
//     by default). Service.Scan additionally locks per-library so
//     two debouncers racing the same library still produce one scan.
//
//   - **Recursive subscriptions**: fsnotify is one-level (it watches
//     a directory, not its subtree). The watcher walks every library
//     path tree at startup and adds a watch for every directory.
//     Newly-created subdirectories pick up a watch the moment their
//     create event fires, so deep copies (S03/E04/extra-features/…)
//     are caught even though the leaf paths didn't exist when we
//     started.
//
//   - **Lazy reconcile**: the periodic refresh of "what libraries
//     exist" only WALKS THE TREE when a library's identity or paths
//     have changed since the previous tick. Unchanged libraries are
//     a single map-comparison; their thousands of subdirs stay
//     untouched. Cost of the periodic tick on a stable deployment
//     is one cheap SQL query plus a paths-equality check per
//     library — independent of how big each library's tree is.
//
//   - **Fail-soft on inotify-less environments**: Docker on Windows
//     with bind mounts cannot deliver inotify events into the
//     container. Either the initial NewWatcher call or the first
//     Add() will error in that case. We log a warning and the
//     watcher goroutine exits cleanly; the periodic Scheduler keeps
//     covering the library so the operator is not left with a stuck
//     catalog. No retry loop: once the host filesystem support is
//     missing, retrying every minute is just noise.
type FSWatcher struct {
	service *Service
	logger  *slog.Logger

	// debounce is the quiet-period before firing a scan. Configurable
	// for tests (sub-second) but fixed at 2s in production.
	debounce time.Duration

	// reconcileEvery controls how often we re-list libraries to pick
	// up new ones / drop deleted ones. 5 min in production.
	reconcileEvery time.Duration

	// watch is the live fsnotify handle. nil before Start, nil after
	// Stop. Owned exclusively by the dispatcher goroutine.
	watch *fsnotify.Watcher

	// debouncers are per-library timers, keyed by library id. Owned
	// exclusively by the dispatcher goroutine.
	debouncers map[string]*time.Timer

	// watchedRoots maps every directory currently under watch to the
	// library id that owns it. Owned exclusively by the dispatcher.
	watchedRoots map[string]string

	// lastSeen is the snapshot of (libID → sorted paths) from the
	// previous reconcile tick. Used to skip the expensive tree walk
	// for libraries that didn't change.
	lastSeen map[string][]string

	// walksDone counts how many times addLibraryTree() has run.
	// Test-only observability — prod code never reads this. Atomic so
	// tests can sample it from outside the dispatcher goroutine
	// without holding any locks.
	walksDone       atomic.Int64
	walksDoneNotify chan struct{}

	reconcileDone       atomic.Int64
	reconcileDoneNotify chan struct{}

	// mu guards stop visibility from Stop() to the dispatcher loop.
	mu      sync.Mutex
	stopCh  chan struct{}
	stopped bool
}

// NewFSWatcher constructs a watcher with production defaults. Tests
// can swap the debounce and reconcile intervals via setters before
// calling Start.
func NewFSWatcher(service *Service, logger *slog.Logger) *FSWatcher {
	return &FSWatcher{
		service:         service,
		logger:          logger.With("module", "fs_watcher"),
		debounce:        2 * time.Second,
		reconcileEvery:  5 * time.Minute,
		debouncers:      make(map[string]*time.Timer),
		watchedRoots:    make(map[string]string),
		lastSeen:        make(map[string][]string),
		stopCh:              make(chan struct{}),
		walksDoneNotify:     make(chan struct{}, 32),
		reconcileDoneNotify: make(chan struct{}, 32),
	}
}

// SetDebounce overrides the quiet period before firing a scan. For
// tests only — production callers should use the default.
func (w *FSWatcher) SetDebounce(d time.Duration) { w.debounce = d }

// TestDebounce returns the current debounce duration. Test-only.
func (w *FSWatcher) TestDebounce() time.Duration { return w.debounce }

// SetReconcileEvery overrides the library-list refresh cadence. For
// tests only.
func (w *FSWatcher) SetReconcileEvery(d time.Duration) { w.reconcileEvery = d }

// WalksDone returns how many tree walks the watcher has performed
// since startup. Test-only observability for asserting that the
// reconcile loop is lazy — a stable deployment should see this
// counter increment once at startup and stay there for the rest of
// the process lifetime.
func (w *FSWatcher) WalksDone() int64 { return w.walksDone.Load() }

// WaitForWalksDone blocks until walksDone reaches at least n (or
// timeout fires). Returns true if the threshold was met. Test-only.
func (w *FSWatcher) WaitForWalksDone(n int64, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if w.walksDone.Load() >= n {
			return true
		}
		select {
		case <-w.walksDoneNotify:
		case <-deadline:
			return w.walksDone.Load() >= n
		}
	}
}

// WaitForReconcileDone blocks until reconcileDone reaches at least n.
// Test-only.
func (w *FSWatcher) WaitForReconcileDone(n int64, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		if w.reconcileDone.Load() >= n {
			return true
		}
		select {
		case <-w.reconcileDoneNotify:
		case <-deadline:
			return w.reconcileDone.Load() >= n
		}
	}
}

// Start spawns the dispatcher goroutine. Returns an error only when
// the platform doesn't support fsnotify (Docker on Windows with
// bind mounts is the realistic case); the caller logs and continues
// — the periodic Scheduler still covers the library.
func (w *FSWatcher) Start(ctx context.Context) error {
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watch = watch

	go w.dispatch(ctx)
	return nil
}

// Stop tears the watcher down. Idempotent; safe to call from a
// `defer` in main.go even if Start failed.
func (w *FSWatcher) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	close(w.stopCh)
	w.mu.Unlock()

	if w.watch != nil {
		_ = w.watch.Close()
	}
}

// dispatch is the single goroutine that owns the fsnotify handle,
// the debouncer map and the watched-roots map. Single-owner means
// no further locking is needed inside this loop.
func (w *FSWatcher) dispatch(ctx context.Context) {
	// First reconcile happens immediately so newly-started instances
	// catch up to the existing library set without waiting for the
	// first tick.
	w.reconcile(ctx)

	reconcileTicker := time.NewTicker(w.reconcileEvery)
	defer reconcileTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-reconcileTicker.C:
			w.reconcile(ctx)
		case ev, ok := <-w.watch.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, ev)
		case err, ok := <-w.watch.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error", "error", err)
		}
	}
}

// reconcile keeps the fsnotify subscription in sync with the current
// library set. The expensive part — walking each library's tree —
// runs ONLY for libraries that are new or whose paths changed since
// the previous tick. Unchanged libraries cost one paths-equality
// check apiece, regardless of how big their tree is.
//
// New subdirectories created at runtime under an already-watched
// library are picked up by the inline subscribe-on-CREATE path in
// handleEvent — they do not need a periodic tree walk to be seen.
func (w *FSWatcher) reconcile(ctx context.Context) {
	libs, err := w.service.List(ctx)
	if err != nil {
		w.logger.Warn("reconcile: list libraries", "error", err)
		return
	}

	current := make(map[string][]string, len(libs))
	for _, lib := range libs {
		// Auto-mode only — manual libraries are not walked by the
		// scheduler either, so respect the same operator intent.
		if lib.ScanMode != "auto" {
			continue
		}
		paths := append([]string(nil), lib.Paths...)
		sort.Strings(paths)
		current[lib.ID] = paths
	}

	// Libraries that disappeared — drop every watch they owned.
	for libID := range w.lastSeen {
		if _, stillThere := current[libID]; stillThere {
			continue
		}
		w.removeLibraryWatches(libID)
	}

	// Libraries that are new or whose paths changed — walk and
	// (re-)add. Unchanged libraries fall through this loop with no
	// I/O at all.
	for libID, paths := range current {
		prev, seen := w.lastSeen[libID]
		if seen && pathsEqual(prev, paths) {
			continue
		}
		// Paths changed: nuke the previous set first so a path that
		// was removed from the library no longer leaks a watch.
		if seen {
			w.removeLibraryWatches(libID)
		}
		w.addLibraryTree(libID, paths)
	}

	w.lastSeen = current

	w.reconcileDone.Add(1)
	select {
	case w.reconcileDoneNotify <- struct{}{}:
	default:
	}
}

// addLibraryTree walks every path of a library and adds a fsnotify
// watch for each directory. Errors per-path are logged and skipped —
// an unreadable subtree should not block coverage of the rest.
//
// Increments walksDone for test observability.
func (w *FSWatcher) addLibraryTree(libID string, paths []string) {
	log := w.logger.With("library_id", libID)
	defer func() {
		w.walksDone.Add(1)
		select {
		case w.walksDoneNotify <- struct{}{}:
		default:
		}
	}()
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				log.Warn("walk for watcher",
					"path", path,
					"error", walkErr)
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			if _, alreadyWatched := w.watchedRoots[path]; alreadyWatched {
				return nil
			}
			if err := w.watch.Add(path); err != nil {
				log.Warn("fsnotify add",
					"path", path,
					"error", err)
				return nil
			}
			w.watchedRoots[path] = libID
			return nil
		})
		if err != nil {
			log.Warn("walk root for watcher",
				"root", root,
				"error", err)
		}
	}
}

// removeLibraryWatches drops every watch owned by a library. Called
// when a library disappeared from the catalog or when its `paths`
// changed (full rebuild is simpler than a per-path diff and the
// total number of watches is small enough not to matter).
func (w *FSWatcher) removeLibraryWatches(libID string) {
	for path, owner := range w.watchedRoots {
		if owner != libID {
			continue
		}
		_ = w.watch.Remove(path) // best-effort; path may already be gone
		delete(w.watchedRoots, path)
	}
}

// handleEvent processes one fsnotify event. Most events just kick
// the per-library debouncer; the special case is a directory being
// CREATED while we're already watching its parent — we add a watch
// for it inline so deep nested file copies don't escape coverage
// and we never need a periodic tree walk to discover them.
func (w *FSWatcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	libID, ok := w.watchedRoots[filepath.Dir(ev.Name)]
	if !ok {
		// Event in a directory we're not watching for any library.
		// Can happen briefly during reconcile races; ignore.
		return
	}

	// New subdirectory under a watched root → add a watch for it
	// before the user copies files into it.
	//
	// Race: `mkdir -p deep/path` (or any tool that creates several
	// nested dirs in one syscall burst) fires CREATE for the OUTER
	// dir while the parent is watched, but by the time we process
	// that event the kernel has already created the inner dirs —
	// their CREATE events fired against a parent that wasn't yet
	// watched, so they were never delivered. We compensate by
	// walking the new dir and adding watches for everything inside
	// it that already exists. New leaf dirs that appear AFTER this
	// walk land in our own watch (we now own their parent), so
	// they're handled by the standard per-event path. Idempotent
	// against the alreadyWatched check in addLibraryTree-style
	// loops — a dir already in watchedRoots is skipped.
	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			w.addSubtreeWatch(ev.Name, libID)
		}
	}

	w.kickDebounce(ctx, libID)
}

// addSubtreeWatch walks `root` and adds an fsnotify watch for every
// directory underneath, attributed to `libID`. Skips paths already
// in watchedRoots so the inline subscribe-on-CREATE remains
// idempotent against the periodic reconcile walk. Errors per-path
// are logged once and the walk continues — losing one branch is
// strictly better than losing the rest of the tree.
func (w *FSWatcher) addSubtreeWatch(root, libID string) {
	log := w.logger.With("library_id", libID)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Warn("subtree watch walk",
				"path", path,
				"error", walkErr)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if _, already := w.watchedRoots[path]; already {
			return nil
		}
		if err := w.watch.Add(path); err != nil {
			log.Warn("fsnotify add (subtree)",
				"path", path,
				"error", err)
			return nil
		}
		w.watchedRoots[path] = libID
		return nil
	})
}

// kickDebounce starts (or restarts) the per-library quiet-timer.
// When the timer fires, Service.Scan is called once. Bursts of
// thousands of events collapse into one scan request.
func (w *FSWatcher) kickDebounce(ctx context.Context, libID string) {
	if t, ok := w.debouncers[libID]; ok {
		t.Stop()
	}
	log := w.logger.With("library_id", libID)
	w.debouncers[libID] = time.AfterFunc(w.debounce, func() {
		if err := w.service.Scan(ctx, libID); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return
			}
			log.Warn("watcher-triggered scan failed", "error", err)
		} else {
			log.Info("watcher-triggered scan")
		}
	})
}

// pathsEqual compares two pre-sorted string slices for equality.
// reconcile() sorts before storing in lastSeen, so callers can rely
// on positional comparison rather than going through a set.
func pathsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
