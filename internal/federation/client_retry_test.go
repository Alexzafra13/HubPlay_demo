package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// newRetryTestManager wires a Manager with one paired peer pointing at
// `srv.URL`. Mirrors the setup used in client_stream_test.go but
// reusable across the retry tests below.
func newRetryTestManager(t *testing.T, srv *httptest.Server) *Manager {
	t.Helper()
	allowLoopbackForTests(t)
	ctx := context.Background()
	clk := clock.New()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	repo := &inMemoryFedRepo{}
	if _, err := LoadOrCreate(ctx, repo, clk, "Tester"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.HTTPTimeout = 5 * time.Second
	mgr, err := NewManager(ctx, cfg, repo, clk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	now := clk.Now()
	peer := &Peer{
		ID:         "peer-b",
		ServerUUID: "00000000-0000-0000-0000-00000000000b",
		Name:       "B",
		BaseURL:    srv.URL,
		PublicKey:  pub,
		Status:     PeerPaired,
		CreatedAt:  now,
		PairedAt:   &now,
	}
	if err := repo.InsertPeer(ctx, peer); err != nil {
		t.Fatal(err)
	}
	if err := mgr.refreshPeerCache(ctx); err != nil {
		t.Fatal(err)
	}
	return mgr
}

// TestFetchPeerLibraries_RetriesOn5xx asserts that a peer responding
// with a transient 503 on the first attempt is retried — the second
// attempt's 200 is what the caller sees. Without this, a peer that
// blipped during a backup cron would surface as "offline" to the user.
func TestFetchPeerLibraries_RetriesOn5xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			http.Error(w, "down for maintenance", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	libs, err := mgr.FetchPeerLibraries(context.Background(), "peer-b")
	if err != nil {
		t.Fatalf("FetchPeerLibraries after retry: %v", err)
	}
	if libs == nil {
		t.Fatal("expected non-nil libraries slice")
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected exactly 2 attempts (1 fail + 1 retry), got %d", got)
	}
}

// TestFetchPeerLibraries_GivesUpAfterMaxAttempts confirms the retry
// cap. Without a ceiling, a peer locked in a 5xx loop would tie up
// the user-facing request indefinitely.
func TestFetchPeerLibraries_GivesUpAfterMaxAttempts(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "still down", http.StatusBadGateway)
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	if _, err := mgr.FetchPeerLibraries(context.Background(), "peer-b"); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(&attempts); got != int32(peerFetchAttempts) {
		t.Fatalf("expected %d attempts, got %d", peerFetchAttempts, got)
	}
}

// TestFetchPeerLibraries_DoesNotRetry4xx makes sure we don't waste a
// roundtrip on a deterministic refusal. A 401 means our JWT is bad,
// a 403 means scope insufficient — neither will heal by retrying.
func TestFetchPeerLibraries_DoesNotRetry4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	if _, err := mgr.FetchPeerLibraries(context.Background(), "peer-b"); err == nil {
		t.Fatal("expected error on 403")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt for a 4xx, got %d", got)
	}
}

// TestFetchPeerLibraries_HonoursCancelledContext verifies the retry
// loop interrupts the backoff sleep when the caller's ctx fires.
// Previously a slow peer + cancelled user could keep the goroutine
// alive for the full peerFetchBackoff sequence.
func TestFetchPeerLibraries_HonoursCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel mid-first-backoff so the loop bails before attempt 2.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := mgr.FetchPeerLibraries(ctx, "peer-b")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error after cancellation")
	}
	// peerFetchBackoff is 250ms; we should bail well before the full
	// 250+500ms total a no-cancellation run would take.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("cancel was not honoured promptly (elapsed=%v)", elapsed)
	}
}

// TestProxyPeerStreamRequest_RefreshesJWTOn401 verifies the one-shot
// remint behaviour. Peer rejects the first request (simulating an
// expired token), the manager mints a fresh JWT and retries; the
// second response is what the caller sees.
func TestProxyPeerStreamRequest_RefreshesJWTOn401(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			http.Error(w, "token expired", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	resp, err := mgr.ProxyPeerStreamRequest(context.Background(), "peer-b", "/api/v1/peer/stream/session/abc/master.m3u8")
	if err != nil {
		t.Fatalf("ProxyPeerStreamRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after refresh, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected exactly 2 attempts (401 + refreshed), got %d", got)
	}
}

// TestProxyPeerStreamRequest_DoesNotRetryOn200 guards against a
// regression where the refresh-on-auth-failure logic accidentally
// fires on every response.
func TestProxyPeerStreamRequest_DoesNotRetryOn200(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n")
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	resp, err := mgr.ProxyPeerStreamRequest(context.Background(), "peer-b", "/api/v1/peer/stream/session/abc/master.m3u8")
	if err != nil {
		t.Fatalf("ProxyPeerStreamRequest: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt for happy path, got %d", got)
	}
}

// TestProxyPeerStreamRequest_GivesUpAfterRefresh confirms the
// refresh is one-shot. Two consecutive 401s mean something is
// genuinely wrong (clock skew, key rotation, peer side bug) and
// the caller must see the failure rather than a retry storm.
func TestProxyPeerStreamRequest_GivesUpAfterRefresh(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()

	mgr := newRetryTestManager(t, srv)

	resp, err := mgr.ProxyPeerStreamRequest(context.Background(), "peer-b", "/api/v1/peer/stream/session/abc/master.m3u8")
	if err != nil {
		// Body would be nil on error path.
		_ = err
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected the 401 to surface to the caller, got %d", resp.StatusCode)
		}
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected exactly 2 attempts (initial + one refresh), got %d", got)
	}
}
