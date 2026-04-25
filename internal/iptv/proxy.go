package iptv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ChannelHealthReporter lets the proxy flag upstream outcomes without
// pulling the DB layer into this package. Nil-safe at the call sites:
// a proxy constructed without a reporter simply doesn't record health
// (tests that don't care can pass nil; main.go always wires the real
// one).
//
// Success/failure are routed here from the proxy; the implementation
// is expected to persist quickly (single-row UPDATE) so the hot path
// isn't stalled.
type ChannelHealthReporter interface {
	RecordProbeSuccess(ctx context.Context, channelID string)
	RecordProbeFailure(ctx context.Context, channelID string, err error)
}

// StreamProxy proxies IPTV streams to clients and counts concurrent listeners
// per channel (for observability).
type StreamProxy struct {
	mu       sync.Mutex
	relays   map[string]*relay // keyed by channel ID
	logger   *slog.Logger
	client   *http.Client
	reporter ChannelHealthReporter
}

// SetHealthReporter wires the reporter after construction so main.go
// can build the proxy before the IPTV service exists (the reporter
// lives on the service). Nil is allowed and turns health tracking off.
func (p *StreamProxy) SetHealthReporter(reporter ChannelHealthReporter) {
	p.reporter = reporter
}

// relay tracks how many concurrent clients are watching a channel. Kept as a
// listener counter for ActiveRelays/observability; the upstream is NOT shared
// — each client opens its own connection (trivial to reason about, matches
// the behaviour of every HLS CDN we talk to).
type relay struct {
	channelID string
	streamURL string
	listeners int
}

// proxyTimeouts bounds upstream interactions so a dead CDN can't pin a
// goroutine + socket indefinitely. Only the handshake/header phase uses a
// wall-clock timeout; the body is read as long as the client stays connected
// (streaming = long-lived by design, but stalls are caught by TCP keepalive
// + the per-response ResponseHeaderTimeout).
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

// NewStreamProxy creates a new stream proxy with sane network timeouts.
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
		relays: make(map[string]*relay),
		logger: logger.With("module", "stream-proxy"),
		client: &http.Client{
			Transport: transport,
			// No client-level timeout — it also clocks the body read, which
			// would kill every stream after N seconds. Timeouts are at the
			// transport level above.
		},
	}
}

// reportOutcome records a proxy attempt against the channel's health.
// Client-initiated cancellations are filtered out: if the user hit
// stop / closed the tab, the upstream wasn't necessarily broken, and
// counting that as a failure would pile bogus counts on every channel
// every time a viewer clicks away. The fetchCtx is the context passed
// to the fetch; ctx.Err() distinguishes "cancelled" (don't record)
// from other failures.
func (p *StreamProxy) reportOutcome(ctx, fetchCtx context.Context, channelID string, err error) {
	if p.reporter == nil || channelID == "" {
		return
	}
	if err == nil {
		p.reporter.RecordProbeSuccess(ctx, channelID)
		return
	}
	// Client disconnect / explicit cancellation should not pollute the
	// health counter. If the fetch context was cancelled because the
	// REQUEST context was cancelled (the viewer navigated away) we
	// swallow the outcome.
	if errors.Is(err, context.Canceled) || errors.Is(fetchCtx.Err(), context.Canceled) {
		return
	}
	// A DeadlineExceeded we DO count — it means upstream took longer
	// than our transport timeout, which is a real operational issue.
	p.reporter.RecordProbeFailure(ctx, channelID, err)
}

// ErrUnsafeUpstream is returned when a proxy fetch target resolves to a
// blocked address (loopback, link-local, RFC1918 private, multicast,
// unspecified). Protects against SSRF from user-supplied proxy URLs and from
// upstream CDNs that redirect to an internal address.
var ErrUnsafeUpstream = errors.New("iptv: unsafe upstream address")

// isSafeUpstream reports whether the given URL resolves entirely to
// internet-routable addresses. Called before every upstream fetch. Follows
// the same ruleset as imaging.BlockedIP (the image SafeGet helper).
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
	// If the host is a literal IP we can check it directly without a DNS
	// lookup; literal IPv6 in URL is bracketed and Hostname() strips it.
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrUnsafeUpstream, ip)
		}
		return nil
	}
	// Hostname — resolve and check every returned address.
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

// blockedIP is overridable at test time so httptest.NewServer (on 127.0.0.1)
// stays usable. Production path returns true for any address that must not
// be reached from outbound fetches. Mirrors the logic in imaging.DefaultBlockedIP.
var blockedIP = func(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsPrivate()
}

// ProxyStream streams an IPTV channel to the HTTP response writer.
//
// Each client opens its own upstream connection; the per-channel counter is
// only used by ActiveRelays for observability. Previous versions kept a
// shared context.CancelFunc on the relay but it was never plumbed into the
// upstream fetch, so disconnects by the first listener would (harmlessly)
// cancel a context nobody used. That dead code is gone.
func (p *StreamProxy) ProxyStream(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
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

// streamWithReconnect handles upstream connection with exponential backoff reconnection.
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

// fetchUpstream performs an HTTP GET with proper headers for IPTV streams.
// Returns the response and the final URL after any redirects.
//
// Security: every hop is validated against isSafeUpstream — the initial URL
// AND every redirect target. Without the redirect check a malicious upstream
// could 302 us to http://169.254.169.254/ (cloud metadata) or
// http://127.0.0.1/admin (a local service) and bypass the initial guard.
func (p *StreamProxy) fetchUpstream(ctx context.Context, targetURL string) (*http.Response, string, error) {
	if err := isSafeUpstream(targetURL); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}

	// Set headers that many IPTV CDNs expect
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	// Set Referer from the origin of the URL (some CDNs check this)
	if parsed, err := url.Parse(targetURL); err == nil {
		req.Header.Set("Referer", parsed.Scheme+"://"+parsed.Host+"/")
		req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
	}

	// Route redirects through a validator so we don't follow a CDN into a
	// private-range target. Uses the request-scoped client because the
	// shared p.client has no redirect policy installed.
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

	// Get the final URL after redirects (Go's http.Client follows them automatically).
	// This is crucial for resolving relative URLs in HLS playlists.
	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return resp, finalURL, nil
}

// looksLikeHLSPlaylist checks if the body content looks like an m3u8 playlist.
func looksLikeHLSPlaylist(body []byte) bool {
	// Check first 1KB for HLS markers
	check := body
	if len(check) > 1024 {
		check = check[:1024]
	}
	return bytes.Contains(check, []byte("#EXTM3U")) ||
		bytes.Contains(check, []byte("#EXT-X-")) ||
		bytes.Contains(check, []byte("#EXTINF:"))
}

// isHLSContentType checks if the content type indicates an HLS playlist.
func isHLSContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "mpegurl") ||
		strings.Contains(ct, "apple.mpegurl") ||
		strings.Contains(ct, "x-mpegurl")
}

// isHLSURL checks if the URL looks like an HLS playlist.
func isHLSURL(streamURL string) bool {
	lower := strings.ToLower(streamURL)
	// Strip query params for extension check
	if idx := strings.IndexByte(lower, '?'); idx >= 0 {
		lower = lower[:idx]
	}
	return strings.HasSuffix(lower, ".m3u8") || strings.HasSuffix(lower, ".m3u")
}

// hlsURLPattern matches absolute URLs in m3u8 playlists.
var hlsURLPattern = regexp.MustCompile(`(?i)(https?://[^\s\r\n"]+)`)

// rewriteHLSPlaylist rewrites URLs in an m3u8 playlist to route through our proxy.
// baseURL is the final URL of the playlist (after redirects) used to resolve relative paths.
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

		// Handle EXT tags that may contain URIs
		if strings.HasPrefix(trimmed, "#") {
			rewritten := line
			// First handle URI="..." attributes (may contain relative paths)
			if strings.Contains(strings.ToUpper(rewritten), "URI=\"") {
				rewritten = rewriteURIAttribute(rewritten, base, proxyPrefix)
			}
			// Then rewrite any remaining absolute URLs
			rewritten = hlsURLPattern.ReplaceAllStringFunc(rewritten, func(u string) string {
				if strings.Contains(u, "/proxy?url=") {
					return u
				}
				return proxyPrefix + url.QueryEscape(u)
			})
			result = append(result, rewritten)
			continue
		}

		// Non-comment line = segment or playlist URL
		resolved := resolveURL(base, trimmed)
		result = append(result, proxyPrefix+url.QueryEscape(resolved))
	}

	return []byte(strings.Join(result, "\n"))
}

// resolveURL resolves a potentially relative URL against a base URL.
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

// uriAttrPattern matches URI="value" in EXT tags (case-insensitive).
var uriAttrPattern = regexp.MustCompile(`(?i)URI="([^"]+)"`)

// rewriteURIAttribute rewrites URI="..." attributes in EXT tags.
func rewriteURIAttribute(line string, base *url.URL, proxyPrefix string) string {
	return uriAttrPattern.ReplaceAllStringFunc(line, func(match string) string {
		// Extract the URI value (skip 'URI="' prefix and '"' suffix)
		inner := match[5 : len(match)-1]
		if !strings.HasPrefix(inner, "http://") && !strings.HasPrefix(inner, "https://") {
			inner = resolveURL(base, inner)
		}
		// Preserve original case of URI=
		prefix := match[:4] // "URI=" (preserving case)
		return prefix + `"` + proxyPrefix + url.QueryEscape(inner) + `"`
	})
}

// streamOnceWithChannel connects to upstream. For HLS content, rewrites the playlist.
func (p *StreamProxy) streamOnceWithChannel(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	resp, finalURL, err := p.fetchUpstream(ctx, streamURL)
	// Record the outcome against the channel's health. Only this path
	// reports — ProxyURL (HLS segments) fires dozens of times per minute
	// and would flood the DB; the master playlist fetch is the one-shot
	// signal that matters for "is this upstream reachable".
	p.reportOutcome(context.Background(), ctx, channelID, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t"
	}

	// For HLS: detect by content-type, URL extension, or body content
	if channelID != "" && (isHLSContentType(ct) || isHLSURL(finalURL)) {
		return p.serveRewrittenPlaylist(w, resp, channelID, finalURL, ct)
	}

	// For streams with unknown content-type, peek at the body to check for HLS
	if channelID != "" && isAmbiguousStreamCT(ct) {
		peek, isHLS, peekErr := peekForHLS(resp.Body)
		if peekErr != nil {
			return fmt.Errorf("read upstream: %w", peekErr)
		}
		if isHLS {
			return p.absorbAndRewriteHLS(w, peek, resp.Body, channelID, finalURL)
		}

		// Not HLS — write the peeked data and continue streaming.
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

	// Otherwise, pipe raw bytes (TS streams, segments, etc.)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")

	return p.pipeStream(w, resp.Body)
}

// isAmbiguousStreamCT reports whether the Content-Type is generic enough
// that we should peek at the body to decide if it's actually an HLS
// playlist served under a non-HLS label (common on free IPTV CDNs).
func isAmbiguousStreamCT(ct string) bool {
	switch ct {
	case "video/mp2t", "application/octet-stream", "text/plain", "binary/octet-stream":
		return true
	}
	return false
}

// peekForHLS reads up to 512 bytes from body and reports whether those
// bytes look like an HLS playlist. Returns the bytes consumed so the
// caller can either keep reading (HLS path — concatenate with the rest)
// or write them first then stream the remainder (raw path).
func peekForHLS(body io.Reader) (peek []byte, isHLS bool, err error) {
	buf := make([]byte, 512)
	n, readErr := io.ReadAtLeast(body, buf, 1)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return nil, false, readErr
	}
	peek = buf[:n]
	return peek, looksLikeHLSPlaylist(peek), nil
}

// absorbAndRewriteHLS reads the rest of the body, prepends any
// already-peeked bytes, and serves the whole thing as a rewritten
// HLS playlist.
func (p *StreamProxy) absorbAndRewriteHLS(w http.ResponseWriter, head []byte, tail io.Reader, channelID, baseURL string) error {
	rest, err := io.ReadAll(io.LimitReader(tail, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}
	body := append(head, rest...)
	return p.serveRewrittenPlaylistBody(w, body, channelID, baseURL)
}

// pipeStream copies data from reader to HTTP response with flushing.
func (p *StreamProxy) pipeStream(w http.ResponseWriter, body io.Reader) error {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)

	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil // Client disconnected
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

// serveRewrittenPlaylist reads the full m3u8 body, rewrites URLs, and serves it.
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

// serveRewrittenPlaylistBody rewrites and serves an m3u8 playlist body.
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

// ProxyURL fetches an arbitrary upstream URL and pipes the response to the client.
// Used for proxying HLS segments and sub-playlists.
func (p *StreamProxy) ProxyURL(ctx context.Context, w http.ResponseWriter, channelID, rawURL string) error {
	upstream, err := url.QueryUnescape(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	// Validate it's an HTTP(S) URL
	parsed, err := url.Parse(upstream)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid upstream URL scheme")
	}

	p.logger.Debug("proxying URL", "channel", channelID, "url", upstream)

	resp, finalURL, err := p.fetchUpstream(ctx, upstream)
	if err != nil {
		p.logger.Warn("proxy URL fetch failed", "channel", channelID, "url", upstream, "error", err)
		// Flag the channel for the health system. Without this, a
		// channel whose master playlist works but whose variants fail
		// upstream (Pluto stitcher with bad embedPartner, expired
		// session tokens, geo-blocked CDN edges …) keeps showing as
		// "ok" forever because ProxyStream never re-runs after the
		// initial handshake. We do NOT record success on the happy
		// path of this handler — segment 200s don't prove the channel
		// is healthy, and counting them would let a flaky variant
		// reset the counter between failures. The prober's next pass
		// is the canonical "healthy again" signal.
		// Skip ctx-cancel — that's the user changing channel, not
		// upstream rot, and counting it would punish zapping.
		if !errors.Is(ctx.Err(), context.Canceled) && p.reporter != nil {
			p.reporter.RecordProbeFailure(ctx, channelID, err)
		}
		http.Error(w, "upstream error", http.StatusBadGateway)
		return nil
	}
	defer resp.Body.Close() //nolint:errcheck

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

	// Raw segment — pipe through
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

// ActiveRelays returns the number of active stream relays.
func (p *StreamProxy) ActiveRelays() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.relays)
}

// Shutdown drops the per-channel listener counters. Client request contexts
// do the actual cancellation of in-flight streams — this is just map hygiene.
func (p *StreamProxy) Shutdown() {
	p.mu.Lock()
	for id := range p.relays {
		delete(p.relays, id)
	}
	p.mu.Unlock()
}
