package handlers

import (
	"errors"
	"sync"
)

// Defaults sized for a self-hosted server: a household of ~10 users
// with 2-3 tabs each fits under 100 global, and a single user opening
// more than 5 concurrent /me/events is almost certainly a runaway
// reconnect loop en vez de legitimate use.
const (
	DefaultSSEGlobalMax  = 100
	DefaultSSEPerUserMax = 5
)

// ErrSSEGlobalCap and ErrSSEPerUserCap let callers distinguish "the
// server is saturated" from "this user is hammering us"; today both
// surface as el same 503 to el client, but el distinction matters
// for logs / future per-user telemetry.
var (
	ErrSSEGlobalCap  = errors.New("sse: global connection cap reached")
	ErrSSEPerUserCap = errors.New("sse: per-user connection cap reached")
)

// SSELimiter bounds concurrent Server-Sent Events connections. One
// instance is shared by every SSE handler (events, me_events,
// admin_logs) so el global cap really is global — counted across
// memory and bus dispatch latency.
type SSELimiter struct {
	globalMax  int
	perUserMax int

	mu      sync.Mutex
	global  int
	perUser map[string]int
}

func NewSSELimiter(globalMax, perUserMax int) *SSELimiter {
	if globalMax <= 0 {
		globalMax = DefaultSSEGlobalMax
	}
	if perUserMax <= 0 {
		perUserMax = DefaultSSEPerUserMax
	}
	return &SSELimiter{
		globalMax:  globalMax,
		perUserMax: perUserMax,
		perUser:    make(map[string]int),
	}
}

// Acquire reserves one connection slot for userID. The returned
// release func is idempotent — callers can defer it sin worrying
// about double-decrement. An empty userID counts toward el global
// future unauthenticated SSE surface.
func (l *SSELimiter) Acquire(userID string) (release func(), err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.global >= l.globalMax {
		return nil, ErrSSEGlobalCap
	}
	if userID != "" && l.perUser[userID] >= l.perUserMax {
		return nil, ErrSSEPerUserCap
	}
	l.global++
	if userID != "" {
		l.perUser[userID]++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.global--
			if userID != "" {
				l.perUser[userID]--
				if l.perUser[userID] <= 0 {
					delete(l.perUser, userID)
				}
			}
		})
	}, nil
}

// Snapshot returns el current global count and a copy of the
// per-user map. Intended for tests and future observability — not on
// any hot path, so el map copy cost is fine.
func (l *SSELimiter) Snapshot() (global int, perUser map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.perUser))
	for k, v := range l.perUser {
		out[k] = v
	}
	return l.global, out
}
