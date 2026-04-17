package iptv

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// silentLogger returns a logger that drops output — keeps test runs quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// unblockLoopback lets tests hit httptest servers (which bind to 127.0.0.1)
// without tripping the SSRF guard. Restored on cleanup.
func unblockLoopback(t *testing.T) {
	t.Helper()
	prev := blockedIP
	blockedIP = func(net.IP) bool { return false }
	t.Cleanup(func() { blockedIP = prev })
}

// ─── isSafeUpstream ─────────────────────────────────────────────────────────

func TestIsSafeUpstream_RejectsNonHTTP(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"ftp://example.com/x",
		"javascript:alert(1)",
		"data:text/plain,hi",
		"",
	}
	for _, in := range cases {
		err := isSafeUpstream(in)
		if !errors.Is(err, ErrUnsafeUpstream) {
			t.Errorf("%q: want ErrUnsafeUpstream, got %v", in, err)
		}
	}
}

func TestIsSafeUpstream_RejectsLiteralPrivateIPs(t *testing.T) {
	cases := []string{
		"http://127.0.0.1/",
		"http://127.0.0.1:8080/x",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest/meta-data/", // AWS IMDS
		"http://172.16.0.1/",
		"http://[::1]/",
		"http://0.0.0.0/",
	}
	for _, in := range cases {
		err := isSafeUpstream(in)
		if !errors.Is(err, ErrUnsafeUpstream) {
			t.Errorf("%q: want ErrUnsafeUpstream, got %v", in, err)
		}
	}
}

func TestIsSafeUpstream_AcceptsPublicIPs(t *testing.T) {
	// Override blockedIP so only the scheme + parse checks apply.
	unblockLoopback(t)
	if err := isSafeUpstream("http://1.1.1.1/"); err != nil {
		t.Errorf("1.1.1.1 should be allowed, got %v", err)
	}
	if err := isSafeUpstream("https://example.com/"); err != nil {
		t.Errorf("example.com should be allowed, got %v", err)
	}
}

// ─── fetchUpstream ──────────────────────────────────────────────────────────

func TestStreamProxy_FetchUpstream_LoopbackRejectedWhenGuardActive(t *testing.T) {
	// Don't override blockedIP — the production guard should reject this.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pwned"))
	}))
	defer srv.Close()

	p := NewStreamProxy(silentLogger())
	_, _, err := p.fetchUpstream(context.Background(), srv.URL)
	if !errors.Is(err, ErrUnsafeUpstream) {
		t.Fatalf("want ErrUnsafeUpstream, got %v", err)
	}
}

func TestStreamProxy_FetchUpstream_RedirectIntoLoopbackBlocked(t *testing.T) {
	// A public-looking upstream that 302s to a loopback target. The CheckRedirect
	// validator must reject the hop. Simulated by running TWO servers — we
	// unblock loopback for the INITIAL call but keep the redirect validator
	// active, so the hop gets caught.
	//
	// To make this testable we stand up a single server and have its handler
	// redirect to a literal private-IP URL that would resolve without a DNS hit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, nil, "http://169.254.169.254/", http.StatusFound)
	}))
	defer srv.Close()

	// The initial call to srv (on 127.0.0.1) must pass the guard; the redirect
	// to 169.254... must NOT. Override blockedIP to allow loopback but still
	// reject link-local.
	prev := blockedIP
	blockedIP = func(ip net.IP) bool {
		return ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	defer func() { blockedIP = prev }()

	p := NewStreamProxy(silentLogger())
	_, _, err := p.fetchUpstream(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected redirect-into-link-local to be blocked, got nil error")
	}
	// The error bubbles up wrapped — we just need to confirm it fired.
	if !strings.Contains(err.Error(), "connect") && !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("err should indicate redirect rejection: %v", err)
	}
}

func TestStreamProxy_FetchUpstream_SuccessOnAllowedUpstream(t *testing.T) {
	unblockLoopback(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	p := NewStreamProxy(silentLogger())
	resp, finalURL, err := p.fetchUpstream(context.Background(), srv.URL+"/p")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	_ = resp.Body.Close()
	if finalURL == "" {
		t.Error("finalURL empty")
	}
}

// ─── ProxyURL (the endpoint an authenticated client can hit) ────────────────

func TestStreamProxy_ProxyURL_RejectsSchemeChange(t *testing.T) {
	p := NewStreamProxy(silentLogger())
	rr := httptest.NewRecorder()
	err := p.ProxyURL(context.Background(), rr, "c-1", "file:///etc/passwd")
	// ProxyURL catches the scheme at the top-level parse and returns an error
	// directly (handler will log and ignore).
	if err == nil {
		t.Fatal("expected error for file:// scheme")
	}
}

func TestStreamProxy_ProxyURL_RejectsLoopbackWithoutOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("leak"))
	}))
	defer srv.Close()

	p := NewStreamProxy(silentLogger())
	rr := httptest.NewRecorder()
	// ProxyURL receives a URL-encoded value (that's how the handler passes
	// it); simulate that.
	_ = p.ProxyURL(context.Background(), rr, "c-1", srv.URL)
	// fetchUpstream returns ErrUnsafeUpstream which ProxyURL converts to a
	// 502 Bad Gateway.
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d want 502", rr.Code)
	}
}

// ─── Listener counter (relay cleanup regression) ────────────────────────────

func TestStreamProxy_ActiveRelays_IncrementsAndDecrements(t *testing.T) {
	p := NewStreamProxy(silentLogger())
	// Manually manipulate the relay map (ProxyStream opens a real upstream
	// which is out of scope for this unit test).
	p.mu.Lock()
	p.relays["c-1"] = &relay{channelID: "c-1", streamURL: "x", listeners: 2}
	p.mu.Unlock()

	if got := p.ActiveRelays(); got != 1 {
		t.Errorf("ActiveRelays: got %d want 1", got)
	}

	p.removeListener("c-1")
	if got := p.ActiveRelays(); got != 1 {
		t.Errorf("after one removeListener with 2 listeners, relay should still exist; ActiveRelays=%d", got)
	}

	p.removeListener("c-1")
	if got := p.ActiveRelays(); got != 0 {
		t.Errorf("after removing last listener, relay should be gone; ActiveRelays=%d", got)
	}
}

func TestStreamProxy_Shutdown_ClearsRelays(t *testing.T) {
	p := NewStreamProxy(silentLogger())
	p.mu.Lock()
	p.relays["a"] = &relay{listeners: 1}
	p.relays["b"] = &relay{listeners: 3}
	p.mu.Unlock()

	p.Shutdown()
	if got := p.ActiveRelays(); got != 0 {
		t.Errorf("after Shutdown, ActiveRelays = %d want 0", got)
	}
}
