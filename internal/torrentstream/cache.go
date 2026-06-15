package torrentstream

import (
	"sync"
	"time"
)

// ttlCache is a tiny in-memory cache with per-entry expiry. Eviction is
// lazy (on read), which is fine for the small, low-cardinality keyspace it
// backs (one entry per imdbid+type). Safe for concurrent use.
type ttlCache[T any] struct {
	mu  sync.Mutex
	m   map[string]ttlEntry[T]
	ttl time.Duration
	now func() time.Time // injectable for tests
}

type ttlEntry[T any] struct {
	val T
	exp time.Time
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{
		m:   make(map[string]ttlEntry[T]),
		ttl: ttl,
		now: time.Now,
	}
}

// get returns the cached value and true when present and unexpired. An
// expired entry is dropped and reported as a miss.
func (c *ttlCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		var zero T
		return zero, false
	}
	if c.now().After(e.exp) {
		delete(c.m, key)
		var zero T
		return zero, false
	}
	return e.val, true
}

// ttlCacheMaxEntries bounds the cache so a high-cardinality keyspace (e.g.
// free-text searches) can't grow memory without limit.
const ttlCacheMaxEntries = 1024

// set stores value under key with a fresh TTL. When the cache is full it
// first drops expired entries and, if still full, resets — keeping memory
// bounded without an LRU.
func (c *ttlCache[T]) set(key string, val T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= ttlCacheMaxEntries {
		now := c.now()
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= ttlCacheMaxEntries {
			c.m = make(map[string]ttlEntry[T])
		}
	}
	c.m[key] = ttlEntry[T]{val: val, exp: c.now().Add(c.ttl)}
}

// clear drops every entry (used after an admin indexer change so the next
// search re-queries with the new configuration).
func (c *ttlCache[T]) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string]ttlEntry[T])
}
