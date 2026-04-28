package iptv

import (
	"sync"
	"time"

	"hubplay/internal/clock"
)

// breakerState is the three-state machine of a per-channel circuit
// breaker.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

func (s breakerState) String() string {
	switch s {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// breakerEntry is the runtime state for one channel inside the
// breaker. Mutated only under channelBreaker.mu.
type breakerEntry struct {
	state         breakerState
	failures      int       // consecutive upstream failures while closed
	openedAt      time.Time // when it transitioned to open
	cooldown      time.Duration
	lastChange    time.Time // for pruning idle entries
	trialInFlight bool      // at most one half-open probe at a time
}

const (
	breakerThreshold       = 5
	breakerInitialCooldown = 30 * time.Second
	breakerMaxCooldown     = 5 * time.Minute
	// If a half-open trial neither succeeds nor fails within this
	// window, the slot is forfeited so another caller can probe.
	// Guards against the (rare) path where the trialling fetch
	// neither completes nor reports — e.g. the request context is
	// cancelled mid-fetch and the cancel branch skips Record* (we
	// don't tick the breaker on client cancels).
	breakerTrialTimeout = 30 * time.Second
	// Closed entries with no recent failures are evicted after this
	// idle window so a server with millions of channels doesn't
	// grow an unbounded breaker map.
	breakerIdleEvictAfter = 10 * time.Minute
)

// channelBreaker is the per-channel fast-fail switch in front of the
// stream proxy's upstream. Self-contained: no DB, no HTTP. The zero
// value is invalid — always go through newChannelBreaker.
//
// Rationale for keying on channelID (not URL or host):
//   - Per-URL: blows up on segment fetches (each segment is its own
//     short-lived URL).
//   - Per-host: too coarse — one expired token on a shared CDN would
//     punish every working channel hosted there.
//   - Per-channel: matches the user-visible concept and the existing
//     ChannelHealthReporter granularity. Stream-URL overrides reset
//     the channel naturally on the prober's next pass.
type channelBreaker struct {
	mu      sync.Mutex
	entries map[string]*breakerEntry
	clk     clock.Clock
}

func newChannelBreaker(clk clock.Clock) *channelBreaker {
	if clk == nil {
		clk = clock.New()
	}
	return &channelBreaker{
		entries: make(map[string]*breakerEntry),
		clk:     clk,
	}
}

// Allow reports whether a fresh upstream attempt is allowed for the
// channel right now. The second return is the time the caller should
// wait before retrying when allowed=false (zero when allowed=true).
//
// Side-effect: when the cooldown of an open breaker has expired, this
// call promotes it to half-open and reserves the trial slot for the
// caller. Concurrent callers see allowed=false until the trial
// resolves via RecordSuccess / RecordFailure (or the trial-timeout
// window expires). This is what stops a thundering herd from
// re-hammering a freshly recovered upstream.
func (b *channelBreaker) Allow(channelID string) (bool, time.Duration) {
	if channelID == "" {
		// No channel context (test paths, ProxyURL with empty ID):
		// skip rather than create a phantom shared entry under "".
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	e, ok := b.entries[channelID]
	if !ok {
		return true, 0
	}

	now := b.clk.Now()
	switch e.state {
	case breakerClosed:
		return true, 0
	case breakerOpen:
		elapsed := now.Sub(e.openedAt)
		if elapsed < e.cooldown {
			return false, e.cooldown - elapsed
		}
		// Cooldown expired — promote to half-open and reserve the
		// trial slot for this caller.
		e.state = breakerHalfOpen
		e.trialInFlight = true
		e.lastChange = now
		return true, 0
	case breakerHalfOpen:
		if e.trialInFlight && now.Sub(e.lastChange) < breakerTrialTimeout {
			return false, breakerTrialTimeout - now.Sub(e.lastChange)
		}
		// Either no trial in flight, or the previous trial has
		// gone stale and is forfeited.
		e.trialInFlight = true
		e.lastChange = now
		return true, 0
	}
	return true, 0
}

// RecordSuccess closes the breaker and clears the failure counter.
// Idempotent for unknown channels and already-closed states.
func (b *channelBreaker) RecordSuccess(channelID string) {
	if channelID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[channelID]
	if !ok {
		// No prior failures — nothing to record.
		return
	}
	e.state = breakerClosed
	e.failures = 0
	e.cooldown = 0
	e.trialInFlight = false
	e.lastChange = b.clk.Now()
}

// RecordFailure increments the failure counter (when closed) or
// re-opens the breaker (when a half-open trial fails). On the
// closed→open transition the cooldown starts at breakerInitialCooldown;
// on every half-open→open re-trip it doubles up to breakerMaxCooldown.
func (b *channelBreaker) RecordFailure(channelID string) {
	if channelID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	e, ok := b.entries[channelID]
	if !ok {
		e = &breakerEntry{state: breakerClosed, lastChange: now}
		b.entries[channelID] = e
	}
	switch e.state {
	case breakerClosed:
		e.failures++
		if e.failures >= breakerThreshold {
			e.state = breakerOpen
			e.openedAt = now
			e.cooldown = breakerInitialCooldown
			e.lastChange = now
		}
	case breakerHalfOpen:
		// Trial failed — back to open with a longer cooldown.
		e.state = breakerOpen
		e.openedAt = now
		next := e.cooldown * 2
		if next < breakerInitialCooldown {
			next = breakerInitialCooldown
		}
		if next > breakerMaxCooldown {
			next = breakerMaxCooldown
		}
		e.cooldown = next
		e.trialInFlight = false
		e.lastChange = now
	case breakerOpen:
		// Already open. The Allow gate normally prevents reaching
		// here; the rare path is a caller that bypassed Allow (for
		// instance a test poking RecordFailure directly). Refresh
		// lastChange so eviction doesn't sweep it as idle.
		e.lastChange = now
	}
}

// State returns a human label and the remaining cooldown for the
// channel. Used by the admin dashboard to show "this channel is in
// a 25 s cooldown after 5 consecutive upstream failures".
func (b *channelBreaker) State(channelID string) (string, time.Duration) {
	if channelID == "" {
		return "closed", 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[channelID]
	if !ok {
		return "closed", 0
	}
	switch e.state {
	case breakerOpen:
		now := b.clk.Now()
		remaining := e.cooldown - now.Sub(e.openedAt)
		if remaining < 0 {
			remaining = 0
		}
		return "open", remaining
	case breakerHalfOpen:
		return "half-open", 0
	default:
		return "closed", 0
	}
}

// Prune drops closed entries with no recent failures so the map
// doesn't grow unbounded over weeks of uptime. Idempotent; safe to
// call from a background ticker.
func (b *channelBreaker) Prune() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clk.Now()
	for k, e := range b.entries {
		if e.state == breakerClosed && e.failures == 0 &&
			now.Sub(e.lastChange) > breakerIdleEvictAfter {
			delete(b.entries, k)
		}
	}
}
