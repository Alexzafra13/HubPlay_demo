package iptv

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
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

// Service gestiona libraries IPTV: M3U import, EPG sync, canales.
// Los sub-services (FavoritesOps, WatchHistoryOps, HealthOps,
// ChannelOrderOps) se promueven vía embedding.
type Service struct {
	*FavoritesOps
	*WatchHistoryOps
	*HealthOps
	*ChannelOrderOps

	channels    *db.ChannelRepository
	epgPrograms *db.EPGProgramRepository
	libraries   *db.LibraryRepository
	epgSources  *db.LibraryEPGSourceRepository
	overrides   *db.ChannelOverrideRepository
	logger      *slog.Logger

	mu        sync.Mutex
	refreshes map[string]bool // tracks ongoing refreshes by library ID

	httpClient *http.Client

	// httpInsecureClient — lazy, cacheado para no reconstruir el transport.
	httpInsecureClient *http.Client
	httpInsecureOnce   sync.Once

	// pub — contenedor compartido del *event.Bus opcional.
	// Un único SetEventBus muta los publishers de todos los sub-services.
	pub *publisher

	// proberWorker — post-construcción para evitar dep circular. Nil-safe.
	proberWorker proberRunner

	// bgCtx / bgCancel / bgWG — lifecycle de goroutines detached
	// (auto-EPG, auto-probe). Se drenan en Shutdown para no
	// escribir contra DB cerrada.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// publisher — contenedor compartido del *event.Bus. Un puntero
// compartido entre Service y sub-services.
type publisher struct {
	bus *event.Bus
}

func (p *publisher) publish(e event.Event) {
	if p == nil || p.bus == nil {
		return
	}
	p.bus.Publish(e)
}

func (p *publisher) setBus(bus *event.Bus) {
	if p == nil {
		return
	}
	p.bus = bus
}

// proberRunner — interfaz mínima del prober worker (sink pattern).
type proberRunner interface {
	ProbeNow(ctx context.Context, libraryID string) (ProbeSummary, error)
}

// SetEventBus cablea el bus para publicar PlaylistRefreshed / EPGUpdated
// y ChannelHealthChanged. Nil-safe.
func (s *Service) SetEventBus(bus *event.Bus) {
	s.pub.setBus(bus)
}

// SetProberWorker cablea el prober worker. Opcional: sin él no hay
// auto-probe post-import (el tick periódico los cubrirá).
func (s *Service) SetProberWorker(w proberRunner) { s.proberWorker = w }

// publish — atajo intra-paquete a pub.publish.
func (s *Service) publish(e event.Event) {
	s.pub.publish(e)
}

// NewService crea un nuevo servicio IPTV.
func NewService(
	channels *db.ChannelRepository,
	epgPrograms *db.EPGProgramRepository,
	libraries *db.LibraryRepository,
	favorites *db.ChannelFavoritesRepository,
	channelOrder *db.UserChannelOrderRepository,
	libraryChannelOrder *db.LibraryChannelOrderRepository,
	epgSources *db.LibraryEPGSourceRepository,
	overrides *db.ChannelOverrideRepository,
	logoOverrides *db.ChannelLogoOverrideRepository,
	watchHistory *db.ChannelWatchHistoryRepository,
	logger *slog.Logger,
) *Service {
	iptvLogger := logger.With("module", "iptv")
	bgCtx, bgCancel := context.WithCancel(context.Background())
	pub := &publisher{}
	return &Service{
		// Sub-services con sus deps específicas.
		FavoritesOps:    newFavoritesOps(favorites),
		WatchHistoryOps: newWatchHistoryOps(channels, watchHistory),
		HealthOps:       newHealthOps(channels, pub, iptvLogger),
		ChannelOrderOps: newChannelOrderOps(channels, channelOrder, libraryChannelOrder, logoOverrides, iptvLogger),

		channels:    channels,
		epgPrograms: epgPrograms,
		libraries:   libraries,
		epgSources:  epgSources,
		overrides:   overrides,
		logger:      iptvLogger,
		refreshes:   make(map[string]bool),
		pub:         pub,
		bgCtx:       bgCtx,
		bgCancel:    bgCancel,
		httpClient: &http.Client{
			// 5 min: proveedores grandes (MEGAOTT, 8k+ canales) pueden
			// tardar 1-2 min en streaming residencial.
			Timeout: 5 * time.Minute,
		},
	}
}

// SpawnBackground lanza fn como goroutine del service con bgCtx
// (no context.Background). Se drena en Shutdown. Exportado para
// handlers que necesiten el mismo lifecycle.
func (s *Service) SpawnBackground(fn func(ctx context.Context)) {
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		fn(s.bgCtx)
	}()
}

// BackgroundContext devuelve el bgCtx del service. No usar
// context.Background directo — rompe el drain de Shutdown.
func (s *Service) BackgroundContext() context.Context {
	return s.bgCtx
}

// Shutdown cancela bgCtx y espera a que terminen las goroutines
// detached lanzadas vía SpawnBackground.
func (s *Service) Shutdown() {
	s.bgCancel()
	s.bgWG.Wait()
}

// ── HTTP fetching helpers ─────────────────────────────────────────
//
// Used by both the M3U and EPG refresh paths. They need to negotiate
// gzip with free CDNs that don't set Content-Encoding, so detection is
// layered: header → URL suffix → magic-byte sniff.

// fetchURL downloads content from a URL.
//
// `tlsInsecure` opts THIS fetch out of TLS certificate verification.
// Reserved for IPTV providers that ship expired or self-signed certs
// (extremely common in the space — the same toggle exists in
// Threadfin/xTeVe/Tuliprox). The flag only affects the HTTPS
// handshake performed here; the stream proxy keeps strict
// verification regardless. Off by default.
func (s *Service) fetchURL(ctx context.Context, url string, tlsInsecure bool) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	// Accept-Encoding: most EPG hosts publish a `.xml.gz` URL and expect
	// the client to gunzip. Some also negotiate via Content-Encoding.
	// We handle both: see maybeDecompress below.
	req.Header.Set("Accept-Encoding", "gzip")

	client := s.httpClient
	if tlsInsecure {
		client = s.insecureFetchClient()
		s.logger.Warn("fetching with TLS verification disabled",
			"url", url,
			"hint", "library has tls_insecure=1 — only use for trusted providers with bad certs")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}

	return maybeDecompress(resp.Body, resp.Header.Get("Content-Encoding"), url)
}

// insecureFetchClient lazily builds (and caches) an HTTP client whose
// transport accepts any server certificate. Same timeout budget as
// the strict client because IPTV M3U exports can be slow (5 min) but
// shouldn't hang forever.
func (s *Service) insecureFetchClient() *http.Client {
	s.httpInsecureOnce.Do(func() {
		// gosec G402: deliberately disabling TLS verification. Scope
		// is per-library and gated by the operator-set tls_insecure
		// flag — the column comment explains the trade-off.
		s.httpInsecureClient = &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	})
	return s.httpInsecureClient
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
