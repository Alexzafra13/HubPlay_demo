package iptv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// Once the breaker is open, ProxyURL must respond 503 with a
// Retry-After header WITHOUT touching the upstream. Verifies the
// fast-fail wiring: this is the one that stops 100 viewers from
// hammering a dead CDN.
func TestProxyURL_BreakerOpen_FastFails503(t *testing.T) {
	unblockLoopback(t)
	p := newTestProxy(nil)

	// Force the breaker into open state without hitting the network.
	for i := 0; i < breakerThreshold; i++ {
		p.breaker.RecordFailure("c-dead")
	}

	// httptest server that counts hits — must remain at zero while
	// the breaker is open.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	err := p.ProxyURL(context.Background(), rr, "c-dead", srv.URL)

	var coe *CircuitOpenError
	if !errors.As(err, &coe) {
		t.Fatalf("err = %v, want CircuitOpenError", err)
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err should unwrap to ErrCircuitOpen, got %v", err)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	retryAfter := rr.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("Retry-After header missing")
	}
	if secs, parseErr := strconv.Atoi(retryAfter); parseErr != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want positive integer seconds", retryAfter)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("upstream hit %d time(s) while breaker open, want 0", got)
	}
}

// 502 responses from upstream don't count as fetch failures (HTTP-
// level errors return resp + nil err from fetchUpstream's caller),
// so the breaker only trips on transport-level failures: dial
// errors, TLS errors, response-header timeouts. This test points
// the proxy at a closed port so DialContext fails fast — a real
// "no such host" -class error.
func TestProxyURL_TransportFailure_TripsBreakerAfterThreshold(t *testing.T) {
	unblockLoopback(t)
	p := newTestProxy(nil)

	// 127.0.0.1 with a port that's unlikely to be open.
	deadURL := "http://127.0.0.1:1/segment.ts"

	for i := 0; i < breakerThreshold; i++ {
		rr := httptest.NewRecorder()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = p.ProxyURL(ctx, rr, "c-flap", deadURL)
		cancel()
	}

	state, _ := p.BreakerState("c-flap")
	if state != "open" {
		t.Fatalf("after %d transport failures state = %q, want open",
			breakerThreshold, state)
	}
}

// A real upstream success closes the breaker even from a half-open
// trial — the recovery path that lets a channel come back online
// after a CDN blip.
func TestProxyURL_HalfOpenTrialSuccess_ClosesBreaker(t *testing.T) {
	unblockLoopback(t)
	p := newTestProxy(nil)

	// Inject a mock clock so we can advance past the cooldown window
	// without sleeping.
	mc := &clock.Mock{CurrentTime: time.Now()}
	p.breaker = newChannelBreaker(mc)

	for i := 0; i < breakerThreshold; i++ {
		p.breaker.RecordFailure("c-recover")
	}
	if state, _ := p.BreakerState("c-recover"); state != "open" {
		t.Fatalf("setup: want open, got %s", state)
	}
	mc.Advance(breakerInitialCooldown + time.Second)

	// Working upstream — fetch should succeed and close the breaker.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("seg-bytes"))
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	if err := p.ProxyURL(context.Background(), rr, "c-recover", srv.URL); err != nil {
		t.Fatalf("ProxyURL err = %v, want nil", err)
	}
	if state, _ := p.BreakerState("c-recover"); state != "closed" {
		t.Fatalf("after successful trial state = %q, want closed", state)
	}
}

// ProxyStream must apply the same fast-fail when the breaker is
// open: a 503 with Retry-After and no upstream hit.
func TestProxyStream_BreakerOpen_FastFails503(t *testing.T) {
	unblockLoopback(t)
	p := newTestProxy(nil)

	for i := 0; i < breakerThreshold; i++ {
		p.breaker.RecordFailure("c-down")
	}

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rr := httptest.NewRecorder()
	err := p.ProxyStream(context.Background(), rr, "c-down", srv.URL)

	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("upstream hit %d time(s) while breaker open, want 0", got)
	}
}
