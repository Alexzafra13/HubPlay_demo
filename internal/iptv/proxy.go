package iptv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
		err := p.streamOnce(ctx, w, streamURL)
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

// streamOnce connects to the upstream and copies data until disconnected or cancelled.
func (p *StreamProxy) streamOnce(ctx context.Context, w http.ResponseWriter, streamURL string) error {
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

	// Set content type from upstream
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "video/mp2t" // Default for IPTV
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")

	// Copy stream data to client
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil // Client disconnected — not an error
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
