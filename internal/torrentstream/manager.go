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
)

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

	mu       sync.Mutex
	sessions map[string]*Session // keyed by infohash hex

	reaperStop chan struct{}
	reaperDone chan struct{}
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

	m := &Manager{
		opts:       opts,
		client:     client,
		logger:     logger,
		sessions:   make(map[string]*Session),
		reaperStop: make(chan struct{}),
		reaperDone: make(chan struct{}),
		now:        time.Now,
	}
	go m.reapLoop()
	return m, nil
}

// GetOrStart returns the session for uri (magnet or http(s) .torrent),
// starting it if needed. It blocks until the metadata resolves or
// MetadataTimeout / ctx fires.
func (m *Manager) GetOrStart(ctx context.Context, uri string) (*Session, error) {
	t, err := m.addTorrent(uri)
	if err != nil {
		return nil, err
	}

	// Wait for metadata (magnet handshake) before we can pick a file.
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.opts.MetadataTimeout):
		t.Drop()
		return nil, ErrMetadataTimeout
	}

	ih := t.InfoHash().HexString()

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[ih]; ok {
		s.touch(m.now())
		return s, nil
	}
	// Enforce the cap only for *new* content — re-joining an existing
	// session never trips it (retries/reconnects shouldn't be blocked).
	if len(m.sessions) >= m.opts.MaxSessions {
		t.Drop()
		return nil, ErrTooManySessions
	}

	file := largestFile(t)
	if file == nil {
		t.Drop()
		return nil, ErrNoFiles
	}
	s := &Session{
		infoHash: ih,
		torrent:  t,
		file:     file,
		readahead: m.opts.Readahead,
	}
	s.touch(m.now())
	m.sessions[ih] = s
	m.logger.Info("torrentstream: session started",
		"infohash", ih, "name", t.Name(), "file", file.DisplayPath(), "bytes", file.Length())
	return s, nil
}

func (m *Manager) addTorrent(uri string) (*torrent.Torrent, error) {
	// .torrent over HTTP vs magnet. The caller is responsible for the
	// source being legal (operator-added magnet, or a legal-catalogue
	// .torrent URL surfaced by search.go).
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return m.addTorrentFromURL(uri)
	}
	t, err := m.client.AddMagnet(uri)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: add magnet: %w", err)
	}
	return t, nil
}

func (m *Manager) addTorrentFromURL(url string) (*torrent.Torrent, error) {
	resp, err := httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: fetch .torrent: %w", err)
	}
	defer resp.Close()
	mi, err := metainfo.Load(resp)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: parse .torrent: %w", err)
	}
	t, err := m.client.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: add torrent: %w", err)
	}
	return t, nil
}

// Close stops the reaper and the torrent client.
func (m *Manager) Close() error {
	close(m.reaperStop)
	<-m.reaperDone
	errs := m.client.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
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
			m.reapIdle(m.now())
		}
	}
}

func (m *Manager) reapIdle(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ih, s := range m.sessions {
		if s.idle(now, m.opts.IdleTimeout) {
			if s.torrent != nil {
				s.torrent.Drop()
			}
			delete(m.sessions, ih)
			m.logger.Info("torrentstream: session reaped (idle)", "infohash", ih)
		}
	}
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
