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

// Service manages IPTV libraries: M3U import, EPG sync, channel
// operations.
//
// Cierre del olor CC del audit 2026-05-14 (god-service de 45 métodos
// en 11 sub-features). En CC fase 1 (PR #390) se extrajeron Favorites,
// WatchHistory y Health. En CC fase 2 (esta sesión) se extrae
// ChannelOrderOps — los 16 métodos del bloque "orden / visibilidad /
// logo" (per-user overlay + library admin curation + logo overrides +
// iptv-org refresh). Lo que queda en el core son M3U import + EPG
// sync + EPG sources + channel overrides + el lifecycle de refresh
// (httpClient + mu + refreshes map + bgCtx) — tightly-coupled.
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

	// httpInsecureClient is built lazily the first time a library
	// with TLSInsecure=true triggers a fetch. Cached so repeat
	// refreshes don't pay the transport-construction cost. Guarded
	// by httpInsecureOnce.
	httpInsecureClient *http.Client
	httpInsecureOnce   sync.Once

	// pub es el contenedor compartido del *event.Bus opcional. Service,
	// HealthOps (ChannelHealthChanged events) y futuros sub-services
	// que publiquen tienen un puntero al mismo `*publisher`, así un
	// único `SetEventBus(bus)` muta los publishers de todos a la vez
	// sin que cada uno exponga su propio setter.
	pub *publisher

	// proberWorker is wired post-construction (the worker depends on
	// the service for the ChannelHealthReporter interface, so we'd
	// otherwise have a circular dep at construction time). Nil-safe:
	// service methods that auto-trigger a probe (RefreshM3U) check
	// before calling.
	proberWorker proberRunner

	// bgCtx / bgCancel / bgWG forman el lifecycle de las goroutines
	// detached que el service lanza desde RefreshM3U (auto-EPG +
	// auto-probe) y desde los handlers de iptv_admin. Antes usaban
	// context.Background() y no se drenaban: shutdown durante un
	// refresh escribía contra una DB ya cerrada (audit olores DD +
	// GGGG). Patrón replica `library.Service`.
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

// publisher es el contenedor compartido del *event.Bus opcional. Los
// sub-services que publican (HealthOps con ChannelHealthChanged,
// Service con PlaylistRefreshed/EPGUpdated) tienen un puntero al
// mismo `*publisher`, así un único `Service.SetEventBus(bus)` mutate
// el campo de todos a la vez. Patrón replica del split QQ
// (auth.Service).
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

// proberRunner is the minimal capability surface the service needs
// from a prober worker. Defined on the consumer side (sink pattern)
// so the *Service has no dependency on the worker's internals.
type proberRunner interface {
	ProbeNow(ctx context.Context, libraryID string) (ProbeSummary, error)
}

// SetEventBus wires an event bus so the service publishes PlaylistRefreshed
// / EPGUpdated events at the end of the respective refresh. También
// hace que HealthOps publique ChannelHealthChanged. Nil-safe.
func (s *Service) SetEventBus(bus *event.Bus) {
	s.pub.setBus(bus)
}

// SetIPTVOrgLogos lo expone ChannelOrderOps vía method promotion —
// el setter externo `service.SetIPTVOrgLogos(l)` resuelve a
// `service.ChannelOrderOps.SetIPTVOrgLogos(l)` sin que el facade lo
// declare.

// SetProberWorker wires the active prober worker. Optional: a service
// without one still works, it just won't auto-probe channels after an
// M3U refresh — the periodic worker tick will catch them eventually.
func (s *Service) SetProberWorker(w proberRunner) { s.proberWorker = w }

// publish es un atajo intra-paquete a `pub.publish`. Los métodos del
// Service que publican (M3U → PlaylistRefreshed, EPG → EPGUpdated)
// lo llaman sin saber que es un wrapper sobre el publisher
// compartido.
func (s *Service) publish(e event.Event) {
	s.pub.publish(e)
}

// NewService creates a new IPTV service.
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
		// Sub-services con sus deps específicas. HealthOps comparte el
		// `pub` con Service así un único SetEventBus muta ambos.
		// ChannelOrderOps lleva los 4 repos del bloque order/visibility/
		// logo + el lookup iptv-org (post-construction setter via
		// method promotion).
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
			// 5 min ceiling: large M3U providers (e.g. MEGAOTT,
			// 8k+ channels) can take 1–2 min to stream the full
			// playlist over a residential connection. 60s used to
			// trip on these. Matches the upstream-refresh ctx
			// budget used elsewhere in this package.
			Timeout: 5 * time.Minute,
		},
	}
}

// SpawnBackground lanza fn como goroutine de background del service.
// fn recibe un ctx que se cancela en Shutdown y que tiene como
// padre el bgCtx del service (no context.Background) — el caller
// puede aplicarle un WithTimeout si quiere acotar la operación.
// El service trackea la goroutine en bgWG para drenarla en Shutdown
// (audit olores DD + GGGG).
//
// Exportado para que handlers del paquete `api/handlers` (en
// particular iptv_admin.go) puedan usar el mismo lifecycle en lugar
// de spawn-and-forget con context.Background().
func (s *Service) SpawnBackground(fn func(ctx context.Context)) {
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		fn(s.bgCtx)
	}()
}

// BackgroundContext devuelve el ctx de background del service para
// que los callers que necesiten encadenar timeouts puedan partir de
// él. No usar context.Background — se rompe el drain de Shutdown.
func (s *Service) BackgroundContext() context.Context {
	return s.bgCtx
}

// Shutdown cancela el bgCtx y espera a que terminen las goroutines
// detached lanzadas vía SpawnBackground (auto-EPG / auto-probe tras
// import M3U, refresh async desde handlers admin). Antes era no-op
// y el shutdown corría contra una DB que se cerraba a mitad —
// "sql: database is closed" en logs y, en patológico, writes
// parciales (audit olores DD + GGGG).
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
