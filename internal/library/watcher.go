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

// FSWatcher reacciona a eventos filesystem bajo los paths de cada library
// y dispara un scan tras un período de quietud. Complementa al Scheduler
// periódico: el scheduler cubre corrección (sweep cada 15 min), el watcher
// cubre responsividad (archivo copiado aparece en ~2s).
//
// Notas de diseño:
//   - Re-scan barato: ScanLibrary usa caché de fingerprint por fichero,
//     así un re-scan con un solo cambio es esencialmente gratis.
//   - Dedup de ráfagas: un copy de 10 GiB genera cientos de eventos;
//     se coalescen per-library a un scan tras `debounce` segundos de quietud.
//   - Suscripciones recursivas: fsnotify vigila un directorio (no subtree);
//     se añade watch por cada subdir al arrancar. Subdirs nuevos se captan
//     inline al recibir su evento CREATE.
//   - Reconcile lazy: solo se re-walka el árbol de una library si su
//     identidad o paths cambiaron desde el tick anterior.
//   - Fail-soft: Docker en Windows con bind mounts no entrega inotify.
//     Se loggea warning y el scheduler periódico cubre la library.
type FSWatcher struct {
	service *Service
	logger  *slog.Logger

	debounce       time.Duration
	reconcileEvery time.Duration

	watch      *fsnotify.Watcher
	debouncers map[string]*time.Timer
	watchedRoots map[string]string // dir → library id
	lastSeen     map[string][]string // libID → sorted paths

	// walksDone: observabilidad para tests — cuántos tree walks se hicieron.
	walksDone atomic.Int64

	mu      sync.Mutex
	stopCh  chan struct{}
	stopped bool
}

func NewFSWatcher(service *Service, logger *slog.Logger) *FSWatcher {
	return &FSWatcher{
		service:        service,
		logger:         logger.With("module", "fs_watcher"),
		debounce:       2 * time.Second,
		reconcileEvery: 5 * time.Minute,
		debouncers:     make(map[string]*time.Timer),
		watchedRoots:   make(map[string]string),
		lastSeen:       make(map[string][]string),
		stopCh:         make(chan struct{}),
	}
}

// SetDebounce overridea el período de quietud (solo para tests).
func (w *FSWatcher) SetDebounce(d time.Duration) { w.debounce = d }

// SetReconcileEvery overridea la cadencia de reconcile (solo para tests).
func (w *FSWatcher) SetReconcileEvery(d time.Duration) { w.reconcileEvery = d }

func (w *FSWatcher) WalksDone() int64 { return w.walksDone.Load() }

// Start lanza la goroutine dispatcher. Retorna error solo si la
// plataforma no soporta fsnotify (Docker Windows con bind mounts).
func (w *FSWatcher) Start(ctx context.Context) error {
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watch = watch

	go w.dispatch(ctx)
	return nil
}

// Stop cierra el watcher. Idempotente.
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

// dispatch: goroutine única que posee el handle fsnotify, los debouncers
// y el mapa de watched-roots. Single-owner = sin locks adicionales.
func (w *FSWatcher) dispatch(ctx context.Context) {
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

// reconcile sincroniza suscripciones fsnotify con el set actual de libraries.
// Solo walka el árbol de libraries nuevas o con paths cambiados.
func (w *FSWatcher) reconcile(ctx context.Context) {
	libs, err := w.service.List(ctx)
	if err != nil {
		w.logger.Warn("reconcile: list libraries", "error", err)
		return
	}

	current := make(map[string][]string, len(libs))
	for _, lib := range libs {
		if lib.ScanMode != "auto" {
			continue
		}
		paths := append([]string(nil), lib.Paths...)
		sort.Strings(paths)
		current[lib.ID] = paths
	}

	// Limpiar watches de libraries eliminadas.
	for libID := range w.lastSeen {
		if _, stillThere := current[libID]; stillThere {
			continue
		}
		w.removeLibraryWatches(libID)
	}

	// Libraries nuevas o con paths cambiados — walkear y (re-)añadir.
	for libID, paths := range current {
		prev, seen := w.lastSeen[libID]
		if seen && pathsEqual(prev, paths) {
			continue
		}
		if seen {
			w.removeLibraryWatches(libID)
		}
		w.addLibraryTree(libID, paths)
	}

	w.lastSeen = current
}

// addLibraryTree walka cada path de una library y añade watch fsnotify
// por cada directorio. Errores per-path se loggean y saltan.
func (w *FSWatcher) addLibraryTree(libID string, paths []string) {
	w.walksDone.Add(1)
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				w.logger.Warn("walk for watcher",
					"path", path,
					"library_id", libID,
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
				w.logger.Warn("fsnotify add",
					"path", path,
					"library_id", libID,
					"error", err)
				return nil
			}
			w.watchedRoots[path] = libID
			return nil
		})
		if err != nil {
			w.logger.Warn("walk root for watcher",
				"root", root,
				"library_id", libID,
				"error", err)
		}
	}
}

// removeLibraryWatches elimina todos los watches de una library.
func (w *FSWatcher) removeLibraryWatches(libID string) {
	for path, owner := range w.watchedRoots {
		if owner != libID {
			continue
		}
		_ = w.watch.Remove(path)
		delete(w.watchedRoots, path)
	}
}

// handleEvent procesa un evento fsnotify. Caso especial: directorio
// nuevo bajo un root vigilado → añadir watch inline.
//
// Race con `mkdir -p deep/path`: el kernel crea inner dirs antes de
// que procesemos el CREATE del outer dir. Compensamos walkeando el
// nuevo dir y añadiendo watches para todo lo que ya exista. Idempotente
// contra el check de alreadyWatched.
func (w *FSWatcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	libID, ok := w.watchedRoots[filepath.Dir(ev.Name)]
	if !ok {
		return
	}

	if ev.Has(fsnotify.Create) {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			w.addSubtreeWatch(ev.Name, libID)
		}
	}

	w.kickDebounce(ctx, libID)
}

// addSubtreeWatch walka `root` y añade watch fsnotify por cada
// subdirectorio, atribuido a `libID`. Errores per-path se loggean
// y el walk continúa.
func (w *FSWatcher) addSubtreeWatch(root, libID string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			w.logger.Warn("subtree watch walk",
				"path", path,
				"library_id", libID,
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
			w.logger.Warn("fsnotify add (subtree)",
				"path", path,
				"library_id", libID,
				"error", err)
			return nil
		}
		w.watchedRoots[path] = libID
		return nil
	})
}

// kickDebounce inicia (o reinicia) el timer per-library. Al dispararse,
// llama Service.Scan una vez. Ráfagas colapsan en un solo scan.
func (w *FSWatcher) kickDebounce(ctx context.Context, libID string) {
	if t, ok := w.debouncers[libID]; ok {
		t.Stop()
	}
	w.debouncers[libID] = time.AfterFunc(w.debounce, func() {
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
