package library

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
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
//   - **Reconcile loop**: libraries created or removed via the admin
//     UI need their paths added/dropped from the watcher without a
//     server restart. Every 5 min we re-list libraries and reconcile.
//     Cheap — the library count is in the dozens, not thousands.
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
	// exclusively by the dispatcher goroutine — the mu protects
	// only the cross-goroutine read of watchedRoots from outside.
	debouncers map[string]*time.Timer

	// watchedRoots tracks which library each currently-watched path
	// belongs to so we know which library debouncer to fire on an
	// event. Reverse map: dirPath → libraryID. Mutated only by the
	// dispatcher goroutine.
	watchedRoots map[string]string

	// libraries is the last-known set of (libID → []paths). Updated
	// each reconcile pass.
	libraries map[string][]string

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
		service:        service,
		logger:         logger.With("module", "fs_watcher"),
		debounce:       2 * time.Second,
		reconcileEvery: 5 * time.Minute,
		debouncers:     make(map[string]*time.Timer),
		watchedRoots:   make(map[string]string),
		libraries:      make(map[string][]string),
		stopCh:         make(chan struct{}),
	}
}

// SetDebounce overrides the quiet period before firing a scan. For
// tests only — production callers should use the default.
func (w *FSWatcher) SetDebounce(d time.Duration) { w.debounce = d }

// SetReconcileEvery overrides the library-list refresh cadence. For
// tests only.
func (w *FSWatcher) SetReconcileEvery(d time.Duration) { w.reconcileEvery = d }

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
	// catch up to the existing library set without waiting 5 min.
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

// reconcile lists every library, walks its paths, and brings the
// fsnotify subscription in sync. Idempotent. Safe to call from the
// dispatcher loop on a tick or after a library mutation event.
func (w *FSWatcher) reconcile(ctx context.Context) {
	libs, err := w.service.List(ctx)
	if err != nil {
		w.logger.Warn("reconcile: list libraries", "error", err)
		return
	}

	wantPaths := make(map[string]string) // path → libraryID
	wantLibs := make(map[string][]string)
	for _, lib := range libs {
		// Auto-mode only — manual libraries are not walked by the
		// scheduler either, so respect the same operator intent.
		if lib.ScanMode != "auto" {
			continue
		}
		wantLibs[lib.ID] = append([]string(nil), lib.Paths...)
		for _, root := range lib.Paths {
			collectDirs(root, lib.ID, wantPaths, w.logger)
		}
	}

	// Add new watches.
	for path, libID := range wantPaths {
		if _, alreadyWatched := w.watchedRoots[path]; alreadyWatched {
			continue
		}
		if err := w.watch.Add(path); err != nil {
			// fail-soft per-path: an unreadable subdirectory
			// shouldn't stop the rest of the tree from being watched.
			w.logger.Warn("fsnotify add",
				"path", path,
				"library_id", libID,
				"error", err)
			continue
		}
		w.watchedRoots[path] = libID
	}

	// Drop watches whose libraries / subdirs no longer exist.
	for path := range w.watchedRoots {
		if _, stillWanted := wantPaths[path]; stillWanted {
			continue
		}
		_ = w.watch.Remove(path) // best-effort; path may already be gone
		delete(w.watchedRoots, path)
	}

	w.libraries = wantLibs
}

// handleEvent processes one fsnotify event. Most events just kick
// the per-library debouncer; the special case is a directory being
// CREATED while we're already watching its parent — we add a watch
// for it inline so deep nested file copies don't escape coverage.
func (w *FSWatcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	libID, ok := w.watchedRoots[filepath.Dir(ev.Name)]
	if !ok {
		// Event in a directory we're not watching for any library.
		// Can happen briefly during reconcile races; ignore.
		return
	}

	// New subdirectory under a watched root → add a watch for it
	// before the user copies files into it.
	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			if err := w.watch.Add(ev.Name); err == nil {
				w.watchedRoots[ev.Name] = libID
			}
		}
	}

	w.kickDebounce(ctx, libID)
}

// kickDebounce starts (or restarts) the per-library quiet-timer.
// When the timer fires, Service.Scan is called once. Bursts of
// thousands of events collapse into one scan request.
func (w *FSWatcher) kickDebounce(ctx context.Context, libID string) {
	if t, ok := w.debouncers[libID]; ok {
		t.Stop()
	}
	w.debouncers[libID] = time.AfterFunc(w.debounce, func() {
		// Service.Scan dedups concurrent calls per library — if a
		// scheduled scan is already running we get ErrConflict and
		// silently skip; the in-flight scan will see our changes.
		if err := w.service.Scan(ctx, libID); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return
			}
			w.logger.Warn("watcher-triggered scan failed",
				"library_id", libID,
				"error", err)
		} else {
			w.logger.Info("watcher-triggered scan", "library_id", libID)
		}
	})
}

// collectDirs walks the tree rooted at `root` and adds every
// directory to `out` with the given libraryID. Errors during the
// walk are logged but never abort it — one unreadable subdirectory
// shouldn't disable watching for the rest.
func collectDirs(
	root, libraryID string,
	out map[string]string,
	logger *slog.Logger,
) {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			logger.Warn("walk for watcher",
				"path", path,
				"library_id", libraryID,
				"error", walkErr)
			// Skip unreadable subtree; continue with siblings.
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			out[path] = libraryID
		}
		return nil
	})
	if err != nil {
		logger.Warn("walk root for watcher",
			"root", root,
			"library_id", libraryID,
			"error", err)
	}
}
