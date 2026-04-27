package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Helper: a transport that drives the cachingTransport off a real
// httptest.Server. We don't reuse server.Client() because we need to
// inject a fake clock + sleep so the backoff tests don't actually
// wait for retries.
func newTestCachingTransport(t *testing.T, base http.RoundTripper, ttl time.Duration) (*cachingTransport, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)}
	tr := newCachingTransport(base, ttl, 0)
	tr.now = clock.Now
	tr.sleep = clock.Sleep
	return tr, clock
}

type fakeClock struct {
	mu        sync.Mutex
	now       time.Time
	totalSlept time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.totalSlept += d
	c.mu.Unlock()
}

func (c *fakeClock) Slept() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSlept
}

// ──────────────────── Cache hits ────────────────────

func TestCachingTransport_CachesSuccessfulGet(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"v":1}`))
	}))
	t.Cleanup(srv.Close)

	tr, _ := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	for i := 0; i < 5; i++ {
		resp, err := client.Get(srv.URL + "/foo?id=42")
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != `{"v":1}` {
			t.Fatalf("attempt %d body: %q", i, body)
		}
	}

	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("upstream hits: got %d want 1 (4 should be cache hits)", got)
	}
}

func TestCachingTransport_TTLExpiryRefetches(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)

	tr, clock := newTestCachingTransport(t, http.DefaultTransport, time.Minute)
	client := &http.Client{Transport: tr}

	if _, err := client.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	// Within TTL: cached.
	if _, err := client.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("after warm-cache: got %d hits, want 1", got)
	}

	// Skip past TTL: refetch.
	clock.mu.Lock()
	clock.now = clock.now.Add(2 * time.Minute)
	clock.mu.Unlock()
	if _, err := client.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("after TTL: got %d hits, want 2", got)
	}
}

func TestCachingTransport_DoesNotCacheNon200(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	tr, _ := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	for i := 0; i < 3; i++ {
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("404s should not be cached: got %d hits, want 3", got)
	}
}

// ──────────────────── Backoff on 429 ────────────────────

func TestCachingTransport_RetriesOn429UntilSuccess(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`finally`))
	}))
	t.Cleanup(srv.Close)

	tr, clock := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "finally" {
		t.Fatalf("body: %q", body)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("upstream hits: got %d want 3", got)
	}
	// Two 429s with Retry-After: 1 → at least 2 s of (fake) sleep.
	if clock.Slept() < 2*time.Second {
		t.Errorf("expected ≥2s of waitBackoff, slept %v", clock.Slept())
	}
}

func TestCachingTransport_GivesUpAfterMaxRetries(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	tr, _ := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("should return last response, not error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status: %d", resp.StatusCode)
	}
	// 1 initial + 3 retries.
	if got := atomic.LoadInt64(&hits); got != 4 {
		t.Errorf("upstream hits: got %d want 4 (1 initial + 3 retries)", got)
	}
}

func TestCachingTransport_RetryAfterCappedAtMaxBackoff(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if n < 2 {
			// Server says wait 1 hour — must be capped at maxBackoff (30 s).
			w.Header().Set("Retry-After", strconv.Itoa(int((time.Hour).Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)

	tr, clock := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	if _, err := client.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	if clock.Slept() > 31*time.Second {
		t.Errorf("waitBackoff should cap at 30s, slept %v", clock.Slept())
	}
}

// ──────────────────── Single-flight ────────────────────

func TestCachingTransport_SingleflightCollapsesConcurrent(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		// Hold the connection a moment so the other goroutines pile up.
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`shared`))
	}))
	t.Cleanup(srv.Close)

	tr, _ := newTestCachingTransport(t, http.DefaultTransport, time.Hour)
	client := &http.Client{Transport: tr}

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Errorf("err: %v", err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) != "shared" {
				t.Errorf("body: %q", body)
			}
		}()
	}
	wg.Wait()

	// Without singleflight all 8 would race to upstream. With it, the
	// first wins and the rest read from the in-flight result.
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("upstream hits: got %d want 1 (singleflight should collapse)", got)
	}
}

// ──────────────────── Ancillary ────────────────────

func TestParseRetryAfter_Seconds(t *testing.T) {
	now := time.Now()
	got := parseRetryAfter("42", now)
	if got != 42*time.Second {
		t.Errorf("seconds: %v", got)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	now := time.Now()
	when := now.Add(2 * time.Minute)
	got := parseRetryAfter(when.UTC().Format(http.TimeFormat), now)
	if got <= 0 || got > 2*time.Minute+time.Second {
		t.Errorf("date: %v", got)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	if got := parseRetryAfter("", time.Now()); got != 0 {
		t.Errorf("empty: %v", got)
	}
}

func TestLRUCache_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newLRUCache(2)
	c.put("a", cachedEntry{body: []byte("A")})
	c.put("b", cachedEntry{body: []byte("B")})
	if _, ok := c.get("a"); !ok {
		t.Fatal("a should be present")
	}
	c.put("c", cachedEntry{body: []byte("C")}) // evicts "b" (LRU)
	if _, ok := c.get("b"); ok {
		t.Errorf("b should have been evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Errorf("a should survive (was touched)")
	}
	if _, ok := c.get("c"); !ok {
		t.Errorf("c should be present")
	}
}

func TestCachingTransport_NetworkErrorRetries(t *testing.T) {
	// Round-tripper that always errors. Exercises the network-error
	// retry branch without standing up a flaky server.
	failing := roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
	tr, _ := newTestCachingTransport(t, failing, time.Hour)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
