package iptv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// StreamProxy proxies IPTV streams to clients with shared relay support.
type StreamProxy struct {
	mu      sync.Mutex
	relays  map[string]*relay // keyed by channel ID
	logger  *slog.Logger
	client  *http.Client
}

// relay manages a shared upstream connection for a channel.
type relay struct {
	channelID string
	streamURL string
	listeners int
	cancel    context.CancelFunc
}

// NewStreamProxy creates a new stream proxy.
func NewStreamProxy(logger *slog.Logger) *StreamProxy {
	return &StreamProxy{
		relays: make(map[string]*relay),
		logger: logger.With("module", "stream-proxy"),
		client: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
	}
}

// ProxyStream streams an IPTV channel to the HTTP response writer.
// It connects to the upstream URL and copies the response body to the client.
func (p *StreamProxy) ProxyStream(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	p.mu.Lock()
	if r, ok := p.relays[channelID]; ok {
		r.listeners++
		p.mu.Unlock()
		defer p.removeListener(channelID)
	} else {
		relayCtx, cancel := context.WithCancel(ctx)
		p.relays[channelID] = &relay{
			channelID: channelID,
			streamURL: streamURL,
			listeners: 1,
			cancel:    cancel,
		}
		p.mu.Unlock()

		defer func() {
			cancel()
			p.removeListener(channelID)
		}()
		_ = relayCtx // Used by cancel above
	}

	p.logger.Info("proxying stream", "channel", channelID, "url", streamURL)

	return p.streamWithReconnect(ctx, w, channelID, streamURL)
}

// streamWithReconnect handles the upstream connection with exponential backoff reconnection.
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

// isHLSContent checks if the content type or URL indicates an HLS playlist.
func isHLSContent(contentType, streamURL string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "apple") {
		return true
	}
	lower := strings.ToLower(streamURL)
	return strings.HasSuffix(lower, ".m3u8") || strings.Contains(lower, ".m3u8?")
}

// hlsURLPattern matches URLs in m3u8 playlists (lines that are not comments and not empty).
var hlsURLPattern = regexp.MustCompile(`(?i)(https?://[^\s\r\n]+)`)

// rewriteHLSPlaylist rewrites URLs in an m3u8 playlist to route through our proxy.
func rewriteHLSPlaylist(body []byte, baseURL, proxyPrefix string) []byte {
	base, err := url.Parse(baseURL)
	if err != nil {
		return body
	}

	lines := strings.Split(string(body), "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments (but check for URI= in EXT tags)
		if trimmed == "" {
			result = append(result, line)
			continue
		}

		// Handle EXT-X-KEY, EXT-X-MAP, etc. with URI="..." attributes
		if strings.HasPrefix(trimmed, "#") {
			rewritten := line
			// First handle URI="..." attributes (may contain relative paths)
			if strings.Contains(rewritten, "URI=\"") {
				rewritten = rewriteURIAttribute(rewritten, base, proxyPrefix)
			}
			// Then rewrite any remaining absolute URLs not inside URI=""
			rewritten = hlsURLPattern.ReplaceAllStringFunc(rewritten, func(u string) string {
				// Skip if already proxied
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

// rewriteURIAttribute rewrites URI="..." attributes in EXT tags.
func rewriteURIAttribute(line string, base *url.URL, proxyPrefix string) string {
	// Match URI="value"
	re := regexp.MustCompile(`URI="([^"]+)"`)
	return re.ReplaceAllStringFunc(line, func(match string) string {
		// Extract the URI value
		inner := match[5 : len(match)-1] // strip URI=" and "
		if !strings.HasPrefix(inner, "http://") && !strings.HasPrefix(inner, "https://") {
			inner = resolveURL(base, inner)
		}
		return `URI="` + proxyPrefix + url.QueryEscape(inner) + `"`
	})
}

// streamOnceWithChannel connects to the upstream. For HLS content, it rewrites the playlist.
func (p *StreamProxy) streamOnceWithChannel(ctx context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "HubPlay/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t"
	}

	// If this is HLS content and we have a channel ID, rewrite the playlist
	if channelID != "" && isHLSContent(ct, streamURL) {
		return p.serveRewrittenPlaylist(ctx, w, resp, channelID, streamURL, ct)
	}

	// Otherwise, pipe raw bytes (TS streams, segments, etc.)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil
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
func (p *StreamProxy) serveRewrittenPlaylist(_ context.Context, w http.ResponseWriter, resp *http.Response, channelID, streamURL, ct string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2MB limit for playlists
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	// Check if it actually looks like an m3u8
	if !bytes.Contains(body, []byte("#EXTM3U")) && !bytes.Contains(body, []byte("#EXT")) {
		// Not actually a playlist, serve as-is
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_, err = w.Write(body)
		return err
	}

	proxyPrefix := "/api/v1/channels/" + channelID + "/proxy?url="
	rewritten := rewriteHLSPlaylist(body, streamURL, proxyPrefix)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, err = w.Write(rewritten)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "HubPlay/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch upstream: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream error", resp.StatusCode)
		return nil
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t"
	}

	// If this is a sub-playlist (m3u8), rewrite it too
	if isHLSContent(ct, upstream) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		if err != nil {
			return fmt.Errorf("read sub-playlist: %w", err)
		}
		if bytes.Contains(body, []byte("#EXTM3U")) || bytes.Contains(body, []byte("#EXT")) {
			proxyPrefix := "/api/v1/channels/" + channelID + "/proxy?url="
			rewritten := rewriteHLSPlaylist(body, upstream, proxyPrefix)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			_, err = w.Write(rewritten)
			return err
		}
		// Not a real playlist, serve raw
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache, no-store")
		_, err = w.Write(body)
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
		r.cancel()
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

// Shutdown stops all active relays.
func (p *StreamProxy) Shutdown() {
	p.mu.Lock()
	for id, r := range p.relays {
		r.cancel()
		delete(p.relays, id)
	}
	p.mu.Unlock()
}
