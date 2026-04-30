package federation

import (
	"sync"
	"time"

	"hubplay/internal/clock"
)

// nonceCache tracks recently-seen JWT nonces so a captured-and-replayed
// token is rejected on its second arrival. The companion to jwt.go's
// short TTL: TTL bounds the *window* a token is valid for, the cache
// bounds *how many times* it can be redeemed (exactly once).
//
// Each entry expires when its token would have expired anyway, so
// memory is bounded by (paired peers × peer rate-limit × token TTL).
// At default config (10 peers × 60 req/min × 5 min) that's ~3000
// entries, ~100 KB. Sweep happens inline on every check; no separate
// goroutine needed.
//
// Concurrency: a single mutex covers the map. Federation traffic is
// not the hot path (rate-limited to 60 req/min/peer), so contention
// over a single lock is irrelevant compared to the SQLite writes that
// happen later in the handler.
type nonceCache struct {
	clock clock.Clock

	mu   sync.Mutex
	seen map[string]time.Time // nonce → expiry (when the token expires)
}

// newNonceCache wires an empty cache. Caller passes the same clock the
// rest of the manager uses for deterministic tests.
func newNonceCache(clk clock.Clock) *nonceCache {
	return &nonceCache{
		clock: clk,
		seen:  make(map[string]time.Time),
	}
}

// checkAndStore returns true if `nonce` has not been seen before
// (and records it for future calls), false if it's a replay. `exp` is
// the JWT's expiry — the cache drops the entry past that point so a
// nonce reused after its parent token would have expired anyway is
// not a "replay" (the token is independently rejected by ValidatePeerToken
// for being expired, so this is just bookkeeping).
//
// An empty nonce is treated as a replay (false). A token without a
// nonce field set is malformed and the middleware rejects it before
// reaching this cache; this is belt-and-braces.
func (c *nonceCache) checkAndStore(nonce string, exp time.Time) bool {
	if nonce == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock.Now()

	// Sweep expired entries inline. O(n) but n is bounded; cheaper
	// than a separate goroutine + signal plumbing for this volume.
	for n, e := range c.seen {
		if now.After(e) {
			delete(c.seen, n)
		}
	}

	if _, ok := c.seen[nonce]; ok {
		return false
	}
	c.seen[nonce] = exp
	return true
}

// size returns the current number of tracked nonces. Test-only;
// production code should not depend on cache size.
func (c *nonceCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}
