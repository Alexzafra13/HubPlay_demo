package iptv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hubplay/internal/clock"
)

// ChannelHealthReporter — interfaz para que el proxy registre
// outcomes upstream sin importar db. Nil-safe en call sites.
type ChannelHealthReporter interface {
	RecordProbeSuccess(ctx context.Context, channelID string)
	RecordProbeFailure(ctx context.Context, channelID string, err error)
}

// StreamProxy proxea streams IPTV y cuenta listeners concurrentes por canal.
type StreamProxy struct {
	mu       sync.Mutex
	relays   map[string]*relay // keyed by channel ID
	logger   *slog.Logger
	client   *http.Client
	reporter ChannelHealthReporter
	breaker  *channelBreaker
}

// SetHealthReporter cablea el reporter post-construcción. nil desactiva tracking.
func (p *StreamProxy) SetHealthReporter(reporter ChannelHealthReporter) {
	p.reporter = reporter
}

// relay — contador de listeners concurrentes per canal.
// El upstream NO se comparte: cada cliente abre su propia conexión.
type relay struct {
	channelID string
	streamURL string
	listeners int
}

// proxyTimeouts — timeouts de transporte para que un CDN muerto
// no bloquee goroutines indefinidamente. Solo handshake/header
// tienen wall-clock timeout; el body vive mientras el cliente conecte.
var proxyTimeouts = struct {
	dial           time.Duration
	tlsHandshake   time.Duration
	responseHeader time.Duration
	idleConn       time.Duration
}{
	dial:           10 * time.Second,
	tlsHandshake:   10 * time.Second,
	responseHeader: 20 * time.Second,
	idleConn:       90 * time.Second,
}

// NewStreamProxy crea un proxy con timeouts de red razonables.
func NewStreamProxy(logger *slog.Logger) *StreamProxy {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   proxyTimeouts.dial,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   proxyTimeouts.tlsHandshake,
		ResponseHeaderTimeout: proxyTimeouts.responseHeader,
		IdleConnTimeout:       proxyTimeouts.idleConn,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}
	return &StreamProxy{
		relays:  make(map[string]*relay),
		logger:  logger.With("module", "stream-proxy"),
		breaker: newChannelBreaker(clock.New()),
		client: &http.Client{
			Transport: transport,
			// Sin timeout de cliente: también contabilizaría la lectura del body,
			// matando cada stream tras N segundos. Los timeouts son de transporte.
		},
	}
}

// ErrCircuitOpen — el circuit breaker del canal está abierto.
var ErrCircuitOpen = errors.New("iptv: circuit open")

// CircuitOpenError lleva canal y cooldown restante para Retry-After.
type CircuitOpenError struct {
	ChannelID  string
	RetryAfter time.Duration
}

func (e *CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit open for channel %s, retry in %s",
		e.ChannelID, e.RetryAfter.Round(time.Second))
}

func (e *CircuitOpenError) Unwrap() error { return ErrCircuitOpen }

// writeCircuitOpenResponse renderiza 503 con Retry-After.
func writeCircuitOpenResponse(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, "upstream unavailable, retry later\n")
}

// BreakerState expone el estado del breaker para el dashboard admin.
func (p *StreamProxy) BreakerState(channelID string) (string, time.Duration) {
	if p.breaker == nil {
		return "closed", 0
	}
	return p.breaker.State(channelID)
}

// Breaker devuelve el circuit breaker como ChannelGate para que
// el TransmuxManager comparta la misma instancia: fallos en cualquier
// plano cierran el breaker para ambos.
func (p *StreamProxy) Breaker() ChannelGate {
	if p.breaker == nil {
		return nil
	}
	return p.breaker
}

// reportOutcome registra un intento del proxy. Las cancelaciones
// del cliente se filtran: un usuario que cierra pestaña no implica
// upstream roto (no contabilizar como fallo).
func (p *StreamProxy) reportOutcome(ctx, fetchCtx context.Context, channelID string, err error) {
	if channelID == "" {
		return
	}
	if err == nil {
		// Breaker antes que reporter para cerrar incluso con reporter nil.
		p.breaker.RecordSuccess(channelID)
		if p.reporter != nil {
			p.reporter.RecordProbeSuccess(ctx, channelID)
		}
		return
	}
	// Desconexión del cliente no debe contaminar health ni breaker.
	if errors.Is(err, context.Canceled) || errors.Is(fetchCtx.Err(), context.Canceled) {
		return
	}
	// DeadlineExceeded SÍ cuenta — upstream superó nuestro timeout.
	p.breaker.RecordFailure(channelID)
	if p.reporter != nil {
		p.reporter.RecordProbeFailure(ctx, channelID, err)
	}
}

// ErrUnsafeUpstream — protección SSRF: la URL resuelve a dirección
// bloqueada (loopback, link-local, privada, multicast).
var ErrUnsafeUpstream = errors.New("iptv: unsafe upstream address")

// isSafeUpstream verifica que la URL resuelva solo a direcciones públicas.
func isSafeUpstream(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parse: %v", ErrUnsafeUpstream, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q", ErrUnsafeUpstream, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrUnsafeUpstream)
	}
	// IP literal: verificar directo sin DNS.
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrUnsafeUpstream, ip)
		}
		return nil
	}
	// Hostname — resolver y verificar cada dirección.
	addrs, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range addrs {
		if blockedIP(ip) {
			return fmt.Errorf("%w: %s → %s", ErrUnsafeUpstream, host, ip)
		}
	}
	return nil
}

// blockedIP — overridable en tests para que httptest.NewServer funcione.
var blockedIP = func(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate()
}

// ProxyStream transmite un canal IPTV al HTTP response writer.
// Cada cliente abre su propia conexión upstream.
func (p *StreamProxy) ProxyStream(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	// Circuit breaker: rechazar rápido (503) en vez de abrir conexiones
	// condenadas contra un CDN muerto.
	if allowed, retryAfter := p.breaker.Allow(channelID); !allowed {
		writeCircuitOpenResponse(w, retryAfter)
		return &CircuitOpenError{ChannelID: channelID, RetryAfter: retryAfter}
	}

	p.mu.Lock()
	if r, ok := p.relays[channelID]; ok {
		r.listeners++
	} else {
		p.relays[channelID] = &relay{
			channelID: channelID,
			streamURL: streamURL,
			listeners: 1,
		}
	}
	p.mu.Unlock()
	defer p.removeListener(channelID)

	p.logger.Info("proxying stream", "channel", channelID, "url", streamURL)

	return p.streamWithReconnect(ctx, w, channelID, streamURL)
}

// streamWithReconnect maneja reconexión upstream con backoff exponencial.
func (p *StreamProxy) streamWithReconnect(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	backoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	attempt := 0

	for {
		err := p.streamOnceWithChannel(ctx, w, channelID, streamURL)
		if err == nil || ctx.Err() != nil {
			return err
		}

		if attempt >= len(backoffs) {
			return fmt.Errorf("stream %s failed after %d attempts: %w", channelID, attempt+1, err)
		}

		p.logger.Warn("stream disconnected, reconnecting",
			"channel", channelID,
			"attempt", attempt+1,
			"backoff", backoffs[attempt],
			"error", err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffs[attempt]):
		}

		attempt++
	}
}

// fetchUpstream hace GET con headers IPTV. Cada hop (incluso redirects)
// se valida contra isSafeUpstream para prevenir SSRF vía 302.
func (p *StreamProxy) fetchUpstream(ctx context.Context, targetURL string) (*http.Response, string, error) {
	if err := isSafeUpstream(targetURL); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	// Headers que muchos CDNs IPTV esperan
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	// Referer desde el origin de la URL (algunos CDNs lo verifican)
	if parsed, err := url.Parse(targetURL); err == nil {
		req.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
		req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	}

	// Validar redirects para no seguir a CDN hacia rango privado.
	redirectingClient := *p.client
	redirectingClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return isSafeUpstream(req.URL.String())
	}

	resp, err := redirectingClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("connect: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, "", fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	// URL final post-redirects: crucial para resolver URLs relativas en HLS.
	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return resp, finalURL, nil
}

// looksLikeHLSPlaylist verifica si el cuerpo parece un playlist m3u8.
func looksLikeHLSPlaylist(body []byte) bool {
	// Buscar marcadores HLS en el primer 1KB
	check := body
	if len(check) > 1024 {
		check = check[:1024]
	}
	return bytes.Contains(check, []byte("#EXTM3U")) ||
		bytes.Contains(check, []byte("#EXT-X-")) ||
		bytes.Contains(check, []byte("#EXTINF:"))
}

// isHLSContentType verifica si el content-type indica playlist HLS.
func isHLSContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "mpegurl") ||
		strings.Contains(ct, "apple.mpegurl") ||
		strings.Contains(ct, "x-mpegurl")
}

// isHLSURL verifica si la URL parece un playlist HLS.
func isHLSURL(streamURL string) bool {
	lower := strings.ToLower(streamURL)
	// Quitar query params para verificar extensión
	if idx := strings.IndexByte(lower, '?'); idx >= 0 {
		lower = lower[:idx]
	}
	return strings.HasSuffix(lower, ".m3u8") || strings.HasSuffix(lower, ".m3u")
}

// IsHLSURL — versión exportada para código fuera del proxy (el handler
// de channel-stream la usa para decidir entre passthrough y transmux).
func IsHLSURL(streamURL string) bool { return isHLSURL(streamURL) }

// hlsURLPattern matchea URLs absolutas en playlists m3u8.
var hlsURLPattern = regexp.MustCompile(`(?i)(https?://[^\s\r\n"]+)`)

// rewriteHLSPlaylist reescribe URLs del m3u8 para enrutar por nuestro proxy.
func rewriteHLSPlaylist(body []byte, baseURL, proxyPrefix string) []byte {
	base, err := url.Parse(baseURL)
	if err != nil {
		return body
	}

	lines := strings.Split(string(body), "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			result = append(result, line)
			continue
		}

		// Procesar tags EXT que pueden contener URIs
		if strings.HasPrefix(trimmed, "#") {
			rewritten := line
			// Primero URI="..." (puede contener paths relativos)
			if strings.Contains(strings.ToUpper(rewritten), "URI=\"") {
				rewritten = rewriteURIAttribute(rewritten, base, proxyPrefix)
			}
			// Luego reescribir URLs absolutas restantes
			rewritten = hlsURLPattern.ReplaceAllStringFunc(rewritten, func(u string) string {
				if strings.Contains(u, "/proxy?url=") {
					return u
				}
				return proxyPrefix + url.QueryEscape(u)
			})
			result = append(result, rewritten)
			continue
		}

		// Línea no-comentario = URL de segmento o playlist
		resolved := resolveURL(base, trimmed)
		result = append(result, proxyPrefix+url.QueryEscape(resolved))
	}

	return []byte(strings.Join(result, "\n"))
}

// resolveURL resuelve una URL potencialmente relativa contra la base.
func resolveURL(base *url.URL, rawURL string) string {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	ref, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return base.ResolveReference(ref).String()
}

// uriAttrPattern matchea URI="value" en tags EXT (case-insensitive).
var uriAttrPattern = regexp.MustCompile(`(?i)URI="([^"]+)"`)

// rewriteURIAttribute reescribe atributos URI="..." en tags EXT.
func rewriteURIAttribute(line string, base *url.URL, proxyPrefix string) string {
	return uriAttrPattern.ReplaceAllStringFunc(line, func(match string) string {
		// Extraer valor URI (saltar prefijo 'URI="' y sufijo '"')
		inner := match[5 : len(match)-1]
		if !strings.HasPrefix(inner, "http://") && !strings.HasPrefix(inner, "https://") {
			inner = resolveURL(base, inner)
		}
		// Preserve original case of URI=
		prefix := match[:4] // "URI=" (preservar case)
		return prefix + `"` + proxyPrefix + url.QueryEscape(inner) + `"`
	})
}

// streamOnceWithChannel conecta upstream. Para HLS, reescribe el playlist.
func (p *StreamProxy) streamOnceWithChannel(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	resp, finalURL, err := p.fetchUpstream(ctx, streamURL)
	// Solo este path reporta health — ProxyURL (segmentos HLS) es
	// demasiado frecuente y floodearía la DB.
	p.reportOutcome(context.Background(), ctx, channelID, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t"
	}

	// HLS: detectar por content-type, extensión URL, o contenido
	if channelID != "" && (isHLSContentType(ct) || isHLSURL(finalURL)) {
		return p.serveRewrittenPlaylist(w, resp, channelID, finalURL, ct)
	}

	// Para content-type ambiguo, peek del body para detectar HLS
	if channelID != "" && isAmbiguousStreamCT(ct) {
		peek, isHLS, peekErr := peekForHLS(resp.Body)
		if peekErr != nil {
			return fmt.Errorf("read upstream: %w", peekErr)
		}
		if isHLS {
			return p.absorbAndRewriteHLS(w, peek, resp.Body, channelID, finalURL)
		}

		// No es HLS — escribir los datos ya leídos y seguir.
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Connection", "keep-alive")
		if _, writeErr := w.Write(peek); writeErr != nil {
			return nil
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return p.pipeStream(w, resp.Body)
	}

	// Caso contrario: pipe de bytes crudos (TS, segmentos, etc.)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")

	return p.pipeStream(w, resp.Body)
}

// isAmbiguousStreamCT — content-type tan genérico que debemos peek
// el body (común en CDNs IPTV gratuitos que sirven HLS con label erróneo).
func isAmbiguousStreamCT(ct string) bool {
	switch ct {
	case "video/mp2t", "application/octet-stream", "text/plain", "binary/octet-stream":
		return true
	}
	return false
}

// peekForHLS lee hasta 512 bytes y reporta si parece HLS.
// Devuelve los bytes consumidos para que el caller los use.
func peekForHLS(body io.Reader) (peek []byte, isHLS bool, err error) {
	buf := make([]byte, 512)
	n, readErr := io.ReadAtLeast(body, buf, 1)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return nil, false, readErr
	}
	peek = buf[:n]
	return peek, looksLikeHLSPlaylist(peek), nil
}

// absorbAndRewriteHLS lee el resto del body, prepende los bytes
// peek y sirve como playlist HLS reescrito.
func (p *StreamProxy) absorbAndRewriteHLS(w http.ResponseWriter, head []byte, tail io.Reader, channelID, baseURL string) error {
	rest, err := io.ReadAll(io.LimitReader(tail, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}
	body := append(head, rest...)
	return p.serveRewrittenPlaylistBody(w, body, channelID, baseURL)
}

// pipeStream copia datos del reader al HTTP response con flush.
func (p *StreamProxy) pipeStream(w http.ResponseWriter, body io.Reader) error {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil // Cliente desconectado
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return fmt.Errorf("upstream closed connection")
			}
			return readErr
		}
	}
}

// serveRewrittenPlaylist lee el m3u8 completo, reescribe URLs y lo sirve.
func (p *StreamProxy) serveRewrittenPlaylist(w http.ResponseWriter, resp *http.Response, channelID, finalURL, ct string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	if !looksLikeHLSPlaylist(body) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_, err = w.Write(body)
		return err
	}

	return p.serveRewrittenPlaylistBody(w, body, channelID, finalURL)
}

// serveRewrittenPlaylistBody reescribe y sirve un body m3u8.
func (p *StreamProxy) serveRewrittenPlaylistBody(w http.ResponseWriter, body []byte, channelID, baseURL string) error {
	proxyPrefix := "/api/v1/channels/" + channelID + "/proxy?url="
	rewritten := rewriteHLSPlaylist(body, baseURL, proxyPrefix)

	p.logger.Debug("serving rewritten HLS playlist",
		"channel", channelID,
		"baseURL", baseURL,
		"bodyLen", len(body),
		"rewrittenLen", len(rewritten),
	)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, err := w.Write(rewritten)
	return err
}

// ProxyURL descarga una URL upstream y pipe al cliente.
// Para segmentos HLS y sub-playlists.
func (p *StreamProxy) ProxyURL(ctx context.Context, w http.ResponseWriter, channelID, rawURL string) error {
	// Circuit breaker — misma lógica que ProxyStream.
	if allowed, retryAfter := p.breaker.Allow(channelID); !allowed {
		writeCircuitOpenResponse(w, retryAfter)
		return &CircuitOpenError{ChannelID: channelID, RetryAfter: retryAfter}
	}

	upstream, err := url.QueryUnescape(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	// Validar que sea URL HTTP(S)
	parsed, err := url.Parse(upstream)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid upstream URL scheme")
	}

	p.logger.Debug("proxying URL", "channel", channelID, "url", upstream)

	resp, finalURL, err := p.fetchUpstream(ctx, upstream)
	if err != nil {
		p.logger.Warn("proxy URL fetch failed", "channel", channelID, "url", upstream, "error", err)
		// Registrar fallo en health: sin esto, un canal cuyo master
		// funciona pero cuyos variants fallan quedaría como "ok"
		// indefinidamente. NO registramos success de segmentos (el
		// prober es la señal canónica de "sano de nuevo").
		// Skip ctx-cancel: el usuario cambió de canal, no es fallo upstream.
		if !errors.Is(ctx.Err(), context.Canceled) {
			// Breaker tracks segment-level failures: a fetch that
			// reaches the network and 5xx's means the URL is dead
			// right now, regardless of whether we tag the channel
			// as "unhealthy" in the DB (the prober owns that).
			p.breaker.RecordFailure(channelID)
			if p.reporter != nil {
				p.reporter.RecordProbeFailure(ctx, channelID, err)
			}
		}
		http.Error(w, "upstream error", http.StatusBadGateway)
		return nil
	}
	defer resp.Body.Close() //nolint:errcheck

	// Segment 200 is a valid "URL is up right now" signal for the
	// breaker, even though the DB-backed prober deliberately
	// IGNORES these (it owns the per-channel health bit and
	// segment-level resets would mask flaky variants between
	// failures). The breaker has different semantics: it gates
	// upstream attempts, not health reporting, so recovery should
	// be observed at the earliest reachability evidence.
	p.breaker.RecordSuccess(channelID)

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t"
	}

	// Check if this is an HLS sub-playlist (by content-type or URL)
	if isHLSContentType(ct) || isHLSURL(finalURL) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		if err != nil {
			return fmt.Errorf("read sub-playlist: %w", err)
		}
		if looksLikeHLSPlaylist(body) {
			return p.serveRewrittenPlaylistBody(w, body, channelID, finalURL)
		}
		// Not a real playlist, serve raw
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, err = w.Write(body)
		return err
	}

	// For segments with ambiguous content-type, peek to check for HLS.
	// The segment path treats `video/mp2t` as unambiguously non-HLS
	// (a TS segment is the expected payload for that CT), so we reuse
	// isAmbiguousStreamCT minus that entry via an inline check.
	if ct == "text/plain" || ct == "application/octet-stream" || ct == "binary/octet-stream" {
		peek, isHLS, peekErr := peekForHLS(resp.Body)
		if peekErr != nil {
			return fmt.Errorf("peek upstream: %w", peekErr)
		}
		if isHLS {
			return p.absorbAndRewriteHLS(w, peek, resp.Body, channelID, finalURL)
		}

		// Not a playlist — serve peeked data + rest
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if _, err := w.Write(peek); err != nil {
			return nil
		}
		_, err = io.Copy(w, resp.Body)
		return err
	}

	// Segmento crudo — pipe directo
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

func (p *StreamProxy) removeListener(channelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	r, ok := p.relays[channelID]
	if !ok {
		return
	}

	r.listeners--
	if r.listeners <= 0 {
		delete(p.relays, channelID)
		p.logger.Info("relay closed (no listeners)", "channel", channelID)
	}
}

// ActiveRelays devuelve el número de relays activos.
func (p *StreamProxy) ActiveRelays() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.relays)
}

// ClearRelays vacía el map de listeners. NO drena goroutines en
// vuelo: se cancelan vía el ctx del request cuando http.Server.Shutdown
// corta los requests.
func (p *StreamProxy) ClearRelays() {
	p.mu.Lock()
	for id := range p.relays {
		delete(p.relays, id)
	}
	p.mu.Unlock()
}
