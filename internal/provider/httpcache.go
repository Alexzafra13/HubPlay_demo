package provider

import (
	"bytes"
	"container/list"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// cachingTransport wraps another http.RoundTripper with three behaviours
// every external metadata/image API needs once a real library lands:
//
//  1. **In-memory response cache** keyed by full URL. Initial scans of a
//     1k-item library used to fire ~2k requests; with a 7-day TTL the
//     same scan repeats over the wire only when items are actually new
//     or refreshed manually. Bounded by an LRU to cap memory: TMDb and
//     Fanart responses are small (a few KB) so 10k entries is ~50 MB
//     ceiling worst-case.
//  2. **Retry on 429 / 5xx** with exponential backoff. Honours the
//     Retry-After header when the upstream sends it (TMDb does);
//     otherwise falls back to base * 2^attempt with jitter. Caps both
//     attempts and total wait so the scan never stalls indefinitely.
//  3. **Single-flight** to collapse parallel duplicate requests into
//     one upstream call. Matters during a fresh scan where multiple
//     items share an external ID (a series + its episodes both ask
//     for the same `/tv/{id}` URL).
//
// The transport only caches successful (200) GETs; everything else
// passes through. Failure responses are deliberately never cached so
// transient upstream blips don't poison the cache.
type cachingTransport struct {
	base    http.RoundTripper
	ttl     time.Duration
	cache   *lruCache
	group   singleflight.Group
	maxRetries int
	maxBackoff time.Duration
	now     func() time.Time // injectable for tests
	sleep   func(time.Duration)
}

func newCachingTransport(base http.RoundTripper, ttl time.Duration, capacity int) *cachingTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if capacity <= 0 {
		capacity = 10_000
	}
	return &cachingTransport{
		base:       base,
		ttl:        ttl,
		cache:      newLRUCache(capacity),
		maxRetries: 3,
		maxBackoff: 30 * time.Second,
		now:        time.Now,
		sleep:      time.Sleep,
	}
}

// newCachingClient returns an *http.Client that wraps requests with the
// cache + backoff + single-flight transport. `timeout` is the per-request
// timeout; the transport's retry loop runs *inside* that budget on
// purpose — a 429 with a 60 s Retry-After should not silently double the
// caller's deadline.
func newCachingClient(timeout time.Duration, cacheTTL time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newCachingTransport(http.DefaultTransport, cacheTTL, 0),
	}
}

func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only GETs are cacheable; mutating requests (none today, but keep
	// the contract honest) bypass the cache and the single-flight.
	if req.Method != http.MethodGet {
		return t.base.RoundTrip(req)
	}

	key := req.URL.String()

	// Cache hit — clone the cached body so callers can read it freely.
	if entry, ok := t.cache.get(key); ok && t.now().Before(entry.expires) {
		return cloneResponse(entry.resp, entry.body), nil
	}

	// Single-flight collapses concurrent identical requests. The first
	// caller does the round-trip + cache write; the others wait on its
	// result and get a clone of the same response.
	v, err, _ := t.group.Do(key, func() (any, error) {
		resp, body, err := t.doWithRetry(req)
		if err != nil {
			return nil, err
		}
		// Only cache 200 OK with a body. 304/3xx/4xx/5xx are passed
		// through but never persisted — a bad response shouldn't
		// poison the cache for the rest of the TTL.
		if resp.StatusCode == http.StatusOK && len(body) > 0 {
			t.cache.put(key, cachedEntry{
				resp:    cloneResponseShell(resp),
				body:    body,
				expires: t.now().Add(t.ttl),
			})
		}
		return cachedEntry{resp: resp, body: body}, nil
	})
	if err != nil {
		return nil, err
	}
	entry := v.(cachedEntry)
	return cloneResponse(entry.resp, entry.body), nil
}

// doWithRetry runs the underlying RoundTrip with retry on 429 and 5xx.
// Drains and replaces the response body on each retry so the final
// returned response is always a fresh, fully-buffered copy.
func (t *cachingTransport) doWithRetry(req *http.Request) (*http.Response, []byte, error) {
	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		// Each attempt needs its own clone so middleware can't read
		// req.Body twice (we don't carry bodies on GETs but keep this
		// honest in case it's ever extended).
		clone := req.Clone(req.Context())
		resp, err := t.base.RoundTrip(clone)
		if err != nil {
			// Network errors are retried; the request context's own
			// deadline still binds the loop so a context-canceled
			// error short-circuits naturally on the next iteration.
			lastErr = err
			if errors.Is(err, req.Context().Err()) {
				return nil, nil, err
			}
			if attempt == t.maxRetries {
				return nil, nil, err
			}
			t.waitBackoff(attempt, "")
			continue
		}

		// Read the body now so we can decide whether to retry without
		// leaving an unread response behind (which would leak the
		// underlying connection).
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt == t.maxRetries {
				return nil, nil, readErr
			}
			t.waitBackoff(attempt, "")
			continue
		}

		// Retry on 429 and 5xx. 4xx other than 429 (auth failures,
		// 404, etc.) are terminal — retrying won't help.
		if resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= 500 && resp.StatusCode < 600) {
			if attempt == t.maxRetries {
				return resp, body, nil
			}
			t.waitBackoff(attempt, resp.Header.Get("Retry-After"))
			continue
		}

		return resp, body, nil
	}
	return nil, nil, lastErr
}

// waitBackoff sleeps for retryAfter (when the server provides it,
// capped at maxBackoff) or otherwise exponential * jitter.
func (t *cachingTransport) waitBackoff(attempt int, retryAfter string) {
	if d := parseRetryAfter(retryAfter, t.now()); d > 0 {
		if d > t.maxBackoff {
			d = t.maxBackoff
		}
		t.sleep(d)
		return
	}
	t.sleep(backoffDelay(attempt, t.maxBackoff))
}

func backoffDelay(attempt int, max time.Duration) time.Duration {
	// Exponential (1 s, 2 s, 4 s, ...) capped at max, with ±25 % jitter
	// so a thundering herd of N callers don't synchronise their retries.
	base := time.Second << attempt //nolint:gosec // attempt is bounded by maxRetries
	if base > max {
		base = max
	}
	if base <= 0 {
		base = time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base) / 2))
	return base - base/4 + jitter
}

// parseRetryAfter accepts both formats the spec allows: a decimal number
// of seconds, or an HTTP-date.
func parseRetryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(h); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// ──────────────────── Cached entry & response cloning ───────────────────

type cachedEntry struct {
	resp    *http.Response
	body    []byte
	expires time.Time
}

// cloneResponse returns a new *http.Response sharing headers/status
// with src but with a fresh, independent body reader. Necessary because
// callers will read the body and close it; the cache must hand out a
// new copy to every consumer.
func cloneResponse(src *http.Response, body []byte) *http.Response {
	clone := *src
	clone.Header = src.Header.Clone()
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	return &clone
}

// cloneResponseShell strips the body from a response so the cache holds
// no reference to the original network reader.
func cloneResponseShell(src *http.Response) *http.Response {
	clone := *src
	clone.Header = src.Header.Clone()
	clone.Body = nil
	return &clone
}

// ──────────────────── LRU cache ────────────────────

// lruCache is a simple bounded LRU keyed by string. We reach for our
// own implementation rather than pulling in a dependency: the surface
// is tiny (get / put), the ordering invariant is straightforward, and
// the package is otherwise dependency-light.
type lruCache struct {
	mu       sync.Mutex
	capacity int
	order    *list.List              // front = most recently used
	index    map[string]*list.Element // key → list element (value is *cacheNode)
}

type cacheNode struct {
	key   string
	entry cachedEntry
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		order:    list.New(),
		index:    make(map[string]*list.Element, capacity),
	}
}

func (c *lruCache) get(key string) (cachedEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.index[key]
	if !ok {
		return cachedEntry{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheNode).entry, true
}

func (c *lruCache) put(key string, entry cachedEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		el.Value.(*cacheNode).entry = entry
		c.order.MoveToFront(el)
		return
	}
	node := &cacheNode{key: key, entry: entry}
	el := c.order.PushFront(node)
	c.index[key] = el

	if c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest != nil {
			c.order.Remove(oldest)
			delete(c.index, oldest.Value.(*cacheNode).key)
		}
	}
}
