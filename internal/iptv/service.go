package iptv

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// ErrRefreshInProgress is returned by RefreshM3U / RefreshEPG when the
// per-library refresh lock is already held. Exposed as a sentinel so
// callers (scheduler, handlers) can distinguish "concurrent refresh"
// from real upstream failures — the scheduler treats this as benign and
// skips recording it as a job-level error.
var ErrRefreshInProgress = errors.New("refresh already in progress")

// Service manages IPTV libraries: M3U import, EPG sync, channel operations.
//
// The methods split across several service_*.go files by concern
// (favorites, M3U, EPG, channels, health, overrides, epg sources). They
// all hang off the same struct — Go allows methods in multiple files on
// the same package, and keeping them on one Service means callers (the
// HTTP handlers) inject a single dependency instead of six.
type Service struct {
	channels    *db.ChannelRepository
	epgPrograms *db.EPGProgramRepository
	libraries   *db.LibraryRepository
	favorites   *db.ChannelFavoritesRepository
	epgSources  *db.LibraryEPGSourceRepository
	overrides   *db.ChannelOverrideRepository
	logger      *slog.Logger

	mu        sync.Mutex
	refreshes map[string]bool // tracks ongoing refreshes by library ID

	httpClient *http.Client

	bus *event.Bus // optional; nil-safe
}

// SetEventBus wires an event bus so the service publishes PlaylistRefreshed
// / EPGUpdated events at the end of the respective refresh. Nil-safe.
func (s *Service) SetEventBus(bus *event.Bus) { s.bus = bus }

func (s *Service) publish(e event.Event) {
	if s.bus != nil {
		s.bus.Publish(e)
	}
}

// NewService creates a new IPTV service.
func NewService(
	channels *db.ChannelRepository,
	epgPrograms *db.EPGProgramRepository,
	libraries *db.LibraryRepository,
	favorites *db.ChannelFavoritesRepository,
	epgSources *db.LibraryEPGSourceRepository,
	overrides *db.ChannelOverrideRepository,
	logger *slog.Logger,
) *Service {
	return &Service{
		channels:    channels,
		epgPrograms: epgPrograms,
		libraries:   libraries,
		favorites:   favorites,
		epgSources:  epgSources,
		overrides:   overrides,
		logger:      logger.With("module", "iptv"),
		refreshes:   make(map[string]bool),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Shutdown is a no-op today — the service has no owned background
// goroutines. Kept as a symmetric counterpart to the other package
// services (stream.Manager, auth.Service, library.Service) so main.go
// keeps a consistent teardown order if long-running workers are added
// here later (scheduled EPG refresh, catalog poller, …).
func (s *Service) Shutdown() {}

// ── HTTP fetching helpers ─────────────────────────────────────────
//
// Used by both the M3U and EPG refresh paths. They need to negotiate
// gzip with free CDNs that don't set Content-Encoding, so detection is
// layered: header → URL suffix → magic-byte sniff.

// fetchURL downloads content from a URL.
func (s *Service) fetchURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	// Accept-Encoding: most EPG hosts publish a `.xml.gz` URL and expect
	// the client to gunzip. Some also negotiate via Content-Encoding.
	// We handle both: see maybeDecompress below.
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}

	return maybeDecompress(resp.Body, resp.Header.Get("Content-Encoding"), url)
}

// maybeDecompress returns a reader that transparently gunzips the body when
// it's gzipped. Detection uses three signals in order of reliability:
//
//  1. Content-Encoding header explicitly says "gzip" (standard HTTP).
//  2. URL ends in ".gz" (common for static hosts serving pre-gzipped files
//     with Content-Type: application/x-gzip and no Content-Encoding header —
//     GitHub raw does exactly this).
//  3. The first two bytes match the gzip magic (1f 8b). This catches hosts
//     that mis-serve `.xml` URLs as gzip bytes.
//
// Falls back to the raw body if nothing matches — never blows up a refresh
// because of detection uncertainty.
func maybeDecompress(body io.ReadCloser, contentEncoding, url string) (io.ReadCloser, error) {
	if strings.EqualFold(contentEncoding, "gzip") || strings.HasSuffix(strings.ToLower(url), ".gz") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("gunzip %s: %w", url, err)
		}
		return &gzipCloser{Reader: gz, underlying: body}, nil
	}

	// Sniff magic bytes as a last resort — wrap with a bufio peek that
	// doesn't lose data.
	br := bufio.NewReader(body)
	peek, _ := br.Peek(2)
	if len(peek) == 2 && peek[0] == 0x1f && peek[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			_ = body.Close()
			return nil, fmt.Errorf("gunzip %s: %w", url, err)
		}
		return &gzipCloser{Reader: gz, underlying: body}, nil
	}
	return &bufferedCloser{Reader: br, underlying: body}, nil
}

// gzipCloser closes both the gzip reader and the underlying HTTP body.
// The stdlib gzip.Reader doesn't chain Close() to its source.
type gzipCloser struct {
	io.Reader
	underlying io.Closer
}

func (g *gzipCloser) Close() error {
	if closer, ok := g.Reader.(io.Closer); ok {
		_ = closer.Close()
	}
	return g.underlying.Close()
}

// bufferedCloser wraps a bufio.Reader so its Close() reaches the underlying
// http.Response.Body.
type bufferedCloser struct {
	io.Reader
	underlying io.Closer
}

func (b *bufferedCloser) Close() error { return b.underlying.Close() }

// ── Shared helpers ────────────────────────────────────────────────

// assignNumber returns parsed if it's a positive channel number, else the
// 1-based position in the playlist. Used when the M3U entry omits a
// channel-number attribute.
func assignNumber(parsed, index int) int {
	if parsed > 0 {
		return parsed
	}
	return index
}

// generateID produces a 16-byte hex string. Used for channel, EPG program
// and EPG source primary keys. Not cryptographic — rand.Read never errors
// on modern platforms, and we discard the error for that reason.
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
