package handlers

import (
	"errors"
	"sync"
	"time"
)

// Defaults sized for a self-hosted server: a household of ~10 users
// with 2-3 tabs each fits under 100 global, and a single user opening
// more than 5 concurrent /me/events is almost certainly a runaway
// reconnect loop rather than legitimate use.
const (
	DefaultSSEGlobalMax  = 100
	DefaultSSEPerUserMax = 5
)

// ErrSSEGlobalCap and ErrSSEPerUserCap let callers distinguish "the
// server is saturated" from "this user is hammering us"; today both
// surface as the same 503 to the client, but the distinction matters
// for logs / future per-user telemetry.
var (
	ErrSSEGlobalCap  = errors.New("sse: global connection cap reached")
	ErrSSEPerUserCap = errors.New("sse: per-user connection cap reached")
)

// SSELimiter bounds concurrent Server-Sent Events connections. One
// instance is shared by every SSE handler (events, me_events,
// admin_logs) so the global cap really is global — counted across
// surfaces, not per-handler.
//
// Each SSE connection also subscribes 1-20 callbacks to the event
// bus and holds a goroutine + buffered channel; without a cap, a
// malicious or buggy client can open thousands and exhaust both
// memory and bus dispatch latency.
type SSELimiter struct {
	globalMax  int
	perUserMax int

	mu      sync.Mutex
	global  int
	perUser map[string]int

	// changes señaliza cada Acquire/release para que los tests puedan
	// esperar transiciones sin polling con time.Sleep. Buffer 32 +
	// envío non-blocking: producción nunca lee.
	changes chan struct{}
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
		changes:    make(chan struct{}, 32),
	}
}

// Acquire reserves one connection slot for userID. The returned
// release func is idempotent — callers can defer it without worrying
// about double-decrement. An empty userID counts toward the global
// cap only (no per-user tracking); current callers always supply
// claims.UserID, but the carve-out keeps the API usable for any
// future unauthenticated SSE surface.
func (l *SSELimiter) Acquire(userID string) (release func(), err error) {
	l.mu.Lock()
	if l.global >= l.globalMax {
		l.mu.Unlock()
		return nil, ErrSSEGlobalCap
	}
	if userID != "" && l.perUser[userID] >= l.perUserMax {
		l.mu.Unlock()
		return nil, ErrSSEPerUserCap
	}
	l.global++
	if userID != "" {
		l.perUser[userID]++
	}
	l.mu.Unlock()
	l.notifyChange()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.global--
			if userID != "" {
				l.perUser[userID]--
				if l.perUser[userID] <= 0 {
					delete(l.perUser, userID)
				}
			}
			l.mu.Unlock()
			l.notifyChange()
		})
	}, nil
}

func (l *SSELimiter) notifyChange() {
	select {
	case l.changes <- struct{}{}:
	default:
	}
}

// Snapshot returns the current global count and a copy of the
// per-user map. Intended for tests and future observability — not on
// any hot path, so the map copy cost is fine.
func (l *SSELimiter) Snapshot() (global int, perUser map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.perUser))
	for k, v := range l.perUser {
		out[k] = v
	}
	return l.global, out
}

// WaitForGlobal bloquea hasta que el contador global == want o el
// timeout vence. Devuelve true si la condición se cumplió. Test-only.
func (l *SSELimiter) WaitForGlobal(want int, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		l.mu.Lock()
		got := l.global
		l.mu.Unlock()
		if got == want {
			return true
		}
		select {
		case <-l.changes:
		case <-deadline:
			l.mu.Lock()
			got := l.global
			l.mu.Unlock()
			return got == want
		}
	}
}
