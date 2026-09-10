// Package torrentstream is a BitTorrent streaming engine for LEGAL
// sources: Internet Archive, public-domain / Creative Commons content,
// and magnets the operator adds and is entitled to use. It deliberately
// contains NO indexer scraping — the in-app search (search.go) queries
// legitimate catalogues only.
//
// What it does: given a magnet (or an http(s) .torrent URL), it resolves
// the metadata, picks the main file, and exposes a *sequential* reader so
// the file can be served over HTTP (with Range support) and played while
// it is still downloading. The session model mirrors the IPTV transmux
// manager: one shared session per content, lazily started, reaped on idle,
// capped by a max-sessions limit.
package torrentstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"hubplay/internal/imaging"
)

// maxTorrentFileBytes caps how large a fetched .torrent metainfo file may
// be. Generous for big multi-file torrents, small enough to bound memory.
const maxTorrentFileBytes = 8 << 20

// Sentinel errors. Handlers map these to HTTP status codes; keeping them
// here (rather than constructing AppError in the engine) preserves the
// project's layering — the engine has no HTTP dependency.
var (
	// ErrDisabled is returned by New when torrent streaming is off.
	ErrDisabled = errors.New("torrentstream: disabled")
	// ErrTooManySessions is returned when the active-session cap is hit.
	ErrTooManySessions = errors.New("torrentstream: max sessions reached")
	// ErrMetadataTimeout is returned when a magnet's metadata cannot be
	// resolved within MetadataTimeout (no reachable peers/trackers).
	ErrMetadataTimeout = errors.New("torrentstream: timed out resolving metadata")
	// ErrNoFiles is returned when a resolved torrent has no files.
	ErrNoFiles = errors.New("torrentstream: torrent has no files")
	// ErrDownloadActive se devuelve al intentar descartar un job que sigue
	// en vuelo (hay que dejarlo terminar o, en el futuro, cancelarlo).
	ErrDownloadActive = errors.New("torrentstream: download still in progress")
)

// Options configures the manager. Zero values are filled with sane
// defaults by withDefaults so callers can pass a near-empty struct.
type Options struct {
	Enabled         bool
	DataDir         string        // scratch root; per-session subdirs underneath
	MaxSessions     int           // active torrents cap (default 4)
	IdleTimeout     time.Duration // close a session after this long without reads (default 5m)
	Readahead       int64         // sequential read-ahead window in bytes (default 16 MiB)
	MetadataTimeout time.Duration // wait for magnet→info (default 60s)
	// AllowPrivateUpstreams relaja el guard SSRF al bajar un .torrent por
	// http(s): permite hosts privados/LAN (p.ej. el Prowlarr empaquetado en
	// docker, que resuelve a una IP privada). Opt-in del operador. Default
	// false (igual que iptv.allow_private_upstreams).
	AllowPrivateUpstreams bool
	// Sink recibe los cambios de estado de las descargas para empujarlos por
	// SSE. Opcional (nil ⇒ el front cae al fetch puntual).
	Sink DownloadEventSink
}

func (o Options) withDefaults() Options {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 4
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = 5 * time.Minute
	}
	if o.Readahead <= 0 {
		o.Readahead = 16 << 20
	}
	if o.MetadataTimeout <= 0 {
		o.MetadataTimeout = 60 * time.Second
	}
	if o.DataDir == "" {
		o.DataDir = filepath.Join(os.TempDir(), "hubplay-torrent")
	}
	return o
}

// Manager owns the torrent client and the live sessions. Safe for
// concurrent use.
type Manager struct {
	opts   Options
	client *torrent.Client
	logger *slog.Logger

	providers []SearchProvider

	mu       sync.Mutex
	sessions map[string]*Session // keyed by infohash hex
	bySrc    map[string]*Session // keyed by the src (magnet/URL) used to start it
	// holders cuenta los "dueños" lógicos de cada torrent por infohash: una
	// sesión de streaming y una descarga del mismo contenido comparten el
	// *torrent.Torrent (el cliente deduplica por infohash). Sólo se hace
	// Drop + limpieza de scratch cuando el contador llega a cero, así una
	// descarga que termina no tira el stream activo y viceversa.
	holders map[string]int

	downloads map[string]*downloadJob // keyed by job id
	dlCtx     context.Context         // background ctx for download goroutines
	dlCancel  context.CancelFunc

	sink         DownloadEventSink
	allowPrivate bool

	reaperStop chan struct{}
	reaperDone chan struct{}
	closeOnce  sync.Once
	closeErr   error
	now        func() time.Time // injectable for tests
}

// New builds a Manager and starts its idle reaper. Returns ErrDisabled
// (and a nil Manager) when opts.Enabled is false so the caller can simply
// skip wiring the feature.
func New(opts Options, logger *slog.Logger) (*Manager, error) {
	if !opts.Enabled {
		return nil, ErrDisabled
	}
	opts = opts.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("torrentstream: create data dir: %w", err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = opts.DataDir
	cfg.DisableIPv6 = true // many container hosts have no IPv6 socket family
	cfg.Seed = false       // we are a leech-to-watch client, not a seeder

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: new client: %w", err)
	}

	dlCtx, dlCancel := context.WithCancel(context.Background())
	m := &Manager{
		opts:         opts,
		client:       client,
		logger:       logger,
		providers:    defaultProviders(),
		sink:         opts.Sink,
		allowPrivate: opts.AllowPrivateUpstreams,
		sessions:     make(map[string]*Session),
		bySrc:        make(map[string]*Session),
		holders:      make(map[string]int),
		downloads:    make(map[string]*downloadJob),
		dlCtx:        dlCtx,
		dlCancel:     dlCancel,
		reaperStop:   make(chan struct{}),
		reaperDone:   make(chan struct{}),
		now:          time.Now,
	}
	go m.reapLoop()
	return m, nil
}

// GetOrStart returns the session for uri (magnet or http(s) .torrent),
// starting it if needed. It blocks until the metadata resolves or
// MetadataTimeout / ctx fires.
func (m *Manager) GetOrStart(ctx context.Context, uri string) (*Session, error) {
	// Fast path: this exact src is already streaming — re-join without
	// re-adding the torrent or waiting on metadata again.
	if s, ok := m.GetActive(uri); ok {
		s.touch(m.now())
		return s, nil
	}

	t, err := m.addTorrent(uri)
	if err != nil {
		return nil, err
	}
	// Registramos un holder de inmediato (el infohash ya se conoce tras
	// addTorrent). Lo soltamos en toda salida que NO acabe creando sesión;
	// en la salida exitosa la sesión hereda este holder y lo libera al ser
	// reapeada.
	ih := t.InfoHash().HexString()
	m.acquire(ih)

	// Wait for metadata (magnet handshake) before we can pick a file.
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		m.release(t)
		return nil, ctx.Err()
	case <-time.After(m.opts.MetadataTimeout):
		m.release(t)
		return nil, ErrMetadataTimeout
	}

	m.mu.Lock()
	if s, ok := m.sessions[ih]; ok {
		s.touch(m.now())
		// Registra también este src como alias de la sesión: el mismo
		// contenido puede llegar como magnet y como URL .torrent, y sin
		// el alias cada GetActive(uri) fallaría y cada GetOrStart
		// repetiría addTorrent + espera de metadata.
		m.bySrc[uri] = s
		m.mu.Unlock()
		m.release(t) // la sesión existente ya tiene su propio holder
		return s, nil
	}
	// Enforce the cap only for *new* content — re-joining an existing
	// session never trips it (retries/reconnects shouldn't be blocked).
	if len(m.sessions) >= m.opts.MaxSessions {
		m.mu.Unlock()
		m.release(t)
		return nil, ErrTooManySessions
	}

	file := largestFile(t)
	if file == nil {
		m.mu.Unlock()
		m.release(t)
		return nil, ErrNoFiles
	}
	s := &Session{
		infoHash:  ih,
		torrent:   t,
		file:      file,
		readahead: m.opts.Readahead,
	}
	s.touch(m.now())
	m.sessions[ih] = s
	m.bySrc[uri] = s
	m.mu.Unlock()
	m.logger.Info("torrentstream: session started",
		"infohash", ih, "name", t.Name(), "file", file.DisplayPath(), "bytes", file.Length())
	return s, nil
}

// acquire registra un holder del torrent (por infohash). Ver Manager.holders.
func (m *Manager) acquire(ih string) {
	m.mu.Lock()
	m.holders[ih]++
	m.mu.Unlock()
}

// release suelta un holder; si era el último hace Drop del torrent y borra su
// copia de scratch del disco. Seguro de llamar sin tener m.mu cogido.
func (m *Manager) release(t *torrent.Torrent) {
	if t == nil {
		return
	}
	ih := t.InfoHash().HexString()
	name := t.Name()
	m.mu.Lock()
	if m.holders[ih] > 0 {
		m.holders[ih]--
	}
	last := m.holders[ih] <= 0
	if last {
		delete(m.holders, ih)
	}
	m.mu.Unlock()
	if !last {
		return
	}
	t.Drop()
	if p := m.scratchPath(name); p != "" {
		if err := os.RemoveAll(p); err != nil {
			m.logger.Warn("torrentstream: scratch cleanup failed", "path", p, "error", err)
		}
	}
}

// scratchPath devuelve la ruta en disco de los datos de un torrent bajo el
// DataDir de scratch, o "" si no puede determinarla con seguridad. anacrolix
// escribe en DataDir/<name> tanto para torrents de un fichero como de varios,
// así que ese es el nodo a borrar. El guard impide jamás devolver el propio
// DataDir o escapar de él (evita un RemoveAll catastrófico).
func (m *Manager) scratchPath(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	root := filepath.Clean(m.opts.DataDir)
	p := filepath.Clean(filepath.Join(root, name))
	if p == root || !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return ""
	}
	return p
}

func (m *Manager) addTorrent(uri string) (*torrent.Torrent, error) {
	// .torrent over HTTP vs magnet. The source is not restricted to any
	// single catalogue — the operator chooses what to add. For HTTP
	// .torrent URLs the fetch is SSRF-guarded (see addTorrentFromURL);
	// magnets resolve over the BitTorrent network, not a server fetch.
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return m.addTorrentFromURL(uri)
	}
	t, err := m.client.AddMagnet(uri)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: add magnet: %w", err)
	}
	return t, nil
}

func (m *Manager) addTorrentFromURL(rawURL string) (*torrent.Torrent, error) {
	// Defensive: a magnet must never reach the .torrent HTTP fetch path
	// (addTorrent already routes magnets to AddMagnet, but guard here too
	// so a future caller can't accidentally http-fetch a magnet).
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawURL)), "magnet:") {
		t, err := m.client.AddMagnet(rawURL)
		if err != nil {
			return nil, fmt.Errorf("torrentstream: add magnet: %w", err)
		}
		return t, nil
	}
	// A .torrent fetch is SSRF-guarded: imaging.SafeGetWith rejects URLs
	// resolving to loopback / LAN / link-local / cloud-metadata and
	// re-validates every redirect hop, so an arbitrary URL can't make the
	// server reach internal services.
	//
	// When the operator opts in (AllowPrivateUpstreams — e.g. the bundled
	// Prowlarr lives on the docker network at a private IP and serves the
	// .torrent via /{id}/download), loopback/RFC1918 become reachable, but
	// link-local (169.254.169.254 metadata), unspecified and multicast stay
	// blocked and redirects are still re-validated — the flag relaxes the
	// range, it never disables the guard.
	data, _, err := imaging.SafeGetWith(rawURL, maxTorrentFileBytes, 20*time.Second,
		imaging.SafeGetOpts{AllowPrivate: m.allowPrivate})
	if err != nil {
		return nil, fmt.Errorf("torrentstream: fetch .torrent: %w", err)
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("torrentstream: parse .torrent: %w", err)
	}
	t, err := m.client.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: add torrent: %w", err)
	}
	return t, nil
}

// Close stops the reaper and the torrent client. Idempotent: a second call
// (lifecycle shutdown + defer en tests) devuelve el resultado de la primera
// en vez de cerrar dos veces el canal del reaper.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.dlCancel() // stop any in-flight downloads
		close(m.reaperStop)
		<-m.reaperDone
		if errs := m.client.Close(); len(errs) > 0 {
			m.closeErr = errs[0]
		}
	})
	return m.closeErr
}

func (m *Manager) reapLoop() {
	defer close(m.reaperDone)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.reaperStop:
			return
		case <-tick.C:
			now := m.now()
			m.reapIdle(now)
			m.pruneDownloads(now)
		}
	}
}

func (m *Manager) reapIdle(now time.Time) {
	// Recogemos las sesiones ociosas con el lock cogido, pero hacemos el
	// Drop + borrado de scratch (I/O de disco) ya fuera del lock para no
	// bloquear al resto del manager.
	var reaped []*Session
	m.mu.Lock()
	for ih, s := range m.sessions {
		if s.idle(now, m.opts.IdleTimeout) {
			delete(m.sessions, ih)
			// Drop every src alias pointing at this reaped session.
			for src, bs := range m.bySrc {
				if bs == s {
					delete(m.bySrc, src)
				}
			}
			reaped = append(reaped, s)
		}
	}
	m.mu.Unlock()
	for _, s := range reaped {
		m.release(s.torrent) // Drop + limpieza de scratch si es el último holder
		m.logger.Info("torrentstream: session reaped (idle)", "infohash", s.infoHash)
	}
}

// downloadRetention es cuánto se conserva un job terminal (completed/failed)
// antes de que el reaper lo purgue. Suficiente para que el panel admin lo vea
// un rato; sin esto el mapa de descargas crecería indefinidamente.
const downloadRetention = 10 * time.Minute

// pruneDownloads elimina los jobs terminales que llevan más de
// downloadRetention en ese estado.
func (m *Manager) pruneDownloads(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.downloads {
		if st, fin := j.terminalSince(); !isActiveDownload(st) && !fin.IsZero() &&
			now.Sub(fin) > downloadRetention {
			delete(m.downloads, id)
		}
	}
}

// RemoveDownload descarta un job terminal del listado (el "×" del panel). No
// permite descartar uno en vuelo — para eso habría que cancelarlo, que es
// otra feature. Devuelve ErrDownloadActive si sigue activo, o (false, nil) si
// el id no existe.
func (m *Manager) RemoveDownload(id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.downloads[id]
	if !ok {
		return false, nil
	}
	if s := j.get(); isActiveDownload(s.Status) {
		return false, ErrDownloadActive
	}
	delete(m.downloads, id)
	return true, nil
}

// GetActive returns the session previously started for src, if it is
// still live. Unlike GetOrStart it never starts a download — it's the
// "play what's already running" path, so any user can call it without
// spending bandwidth.
func (m *Manager) GetActive(src string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.bySrc[src]
	return s, ok
}

// activeCount is a test/observability helper.
func (m *Manager) activeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// largestFile picks the biggest file — the usual "main video" heuristic.
func largestFile(t *torrent.Torrent) *torrent.File {
	var best *torrent.File
	for _, f := range t.Files() {
		if best == nil || f.Length() > best.Length() {
			best = f
		}
	}
	return best
}

// guessContentType maps a filename to a Content-Type for the <video> tag.
// Browsers can play mp4/webm natively; mkv/avi/ts would want a remux step
// (future work, reusing the IPTV ffmpeg path) — we still label them so a
// direct download / VLC works.
func guessContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".ts":
		return "video/mp2t"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}
