package federation

import (
	"sync"
	"time"

	"hubplay/internal/clock"
)

// RateLimiter is a per-peer token-bucket. One bucket per peer_id;
// requests consume one token; tokens refill at a configurable rate
// up to the burst ceiling.
//
// The bucket lives entirely in memory for v1. Restarts grant peers a
// fresh full burst — acceptable because the table-backed persistence
// (Phase 2.5+) only matters if we want strict global enforcement
// across reboots. For a self-hosted deployment with a handful of
// trusted peers, in-memory is the right level of strictness.
//
// Concurrency: per-peer mutex via a sharded map. Single map mutex
// covers the per-peer lookup; the per-peer struct has its own mutex
// for the bucket arithmetic so two peers never block each other.
type RateLimiter struct {
	clock clock.Clock

	rate  float64 // tokens per second
	burst float64 // max tokens

	mu      sync.RWMutex
	buckets map[string]*peerBucket
}

type peerBucket struct {
	mu             sync.Mutex
	tokens         float64
	lastRefillUnix int64 // unix nanos for atomic-ish ops; we still hold mu when mutating
}

// NewRateLimiter creates a limiter where each peer is allowed
// `requestsPerMinute` sustained traffic with a `burst` ceiling for
// short spikes. `burst >= 1` is required.
func NewRateLimiter(clk clock.Clock, requestsPerMinute, burst int) *RateLimiter {
	if requestsPerMinute < 1 {
		requestsPerMinute = 60
	}
	if burst < 1 {
		burst = 30
	}
	return &RateLimiter{
		clock:   clk,
		rate:    float64(requestsPerMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*peerBucket),
	}
}

// Allow checks whether a peer may make one more request right now.
// Returns true on success (token consumed) and false on rate-limited
// (no tokens left, retry later).
func (rl *RateLimiter) Allow(peerID string) bool {
	if rl == nil {
		return true
	}
	bucket := rl.bucketFor(peerID)
	now := rl.clock.Now()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	last := time.Unix(0, bucket.lastRefillUnix)
	elapsed := now.Sub(last).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * rl.rate
		if bucket.tokens > rl.burst {
			bucket.tokens = rl.burst
		}
		bucket.lastRefillUnix = now.UnixNano()
	}

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}
	return false
}

// Tokens returns the current token count for inspection / metrics.
// Refills first so the value is current.
func (rl *RateLimiter) Tokens(peerID string) float64 {
	if rl == nil {
		return 0
	}
	bucket := rl.bucketFor(peerID)
	now := rl.clock.Now()

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	last := time.Unix(0, bucket.lastRefillUnix)
	elapsed := now.Sub(last).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * rl.rate
		if bucket.tokens > rl.burst {
			bucket.tokens = rl.burst
		}
		bucket.lastRefillUnix = now.UnixNano()
	}
	return bucket.tokens
}

// Reset wipes a peer's bucket — used on revoke so a previously-noisy
// peer can't retain residual state if they're ever re-paired.
func (rl *RateLimiter) Reset(peerID string) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, peerID)
}

func (rl *RateLimiter) bucketFor(peerID string) *peerBucket {
	rl.mu.RLock()
	b, ok := rl.buckets[peerID]
	rl.mu.RUnlock()
	if ok {
		return b
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok := rl.buckets[peerID]; ok {
		return b
	}
	b = &peerBucket{
		tokens:         rl.burst, // start with a full bucket — first request is always allowed
		lastRefillUnix: rl.clock.Now().UnixNano(),
	}
	rl.buckets[peerID] = b
	return b
}
