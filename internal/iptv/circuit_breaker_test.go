package iptv

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"hubplay/internal/clock"
)

// newTestBreaker returns a breaker driven by a controllable clock so
// state transitions can be exercised deterministically.
func newTestBreaker(t *testing.T) (*channelBreaker, *clock.Mock) {
	t.Helper()
	mc := &clock.Mock{CurrentTime: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)}
	return newChannelBreaker(mc, nil), mc
}

// newTestBreakerWithLogger es como newTestBreaker pero con un logger
// que escribe a un buffer para que los tests puedan verificar
// transiciones logueadas.
func newTestBreakerWithLogger(t *testing.T) (*channelBreaker, *clock.Mock, *bytes.Buffer) {
	t.Helper()
	mc := &clock.Mock{CurrentTime: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return newChannelBreaker(mc, logger), mc, &buf
}

// A fresh breaker with no entries should allow every channel and
// report "closed" state.
func TestBreaker_FreshState_AllowsAndReportsClosed(t *testing.T) {
	t.Parallel()
	b, _ := newTestBreaker(t)

	allowed, retry := b.Allow("c-1")
	if !allowed || retry != 0 {
		t.Fatalf("fresh breaker should allow; got allowed=%v retry=%v", allowed, retry)
	}
	state, remaining := b.State("c-1")
	if state != "closed" || remaining != 0 {
		t.Fatalf("fresh state = (%q, %v), want (closed, 0)", state, remaining)
	}
}

// Empty channelID is the test/anonymous-fetch case — the breaker
// must skip rather than create a phantom shared entry under "".
func TestBreaker_EmptyChannelID_SkipsAllSurfaces(t *testing.T) {
	t.Parallel()
	b, _ := newTestBreaker(t)

	for i := 0; i < breakerThreshold+5; i++ {
		b.RecordFailure("")
	}
	allowed, _ := b.Allow("")
	if !allowed {
		t.Fatal("empty channelID should always be allowed")
	}
	state, _ := b.State("")
	if state != "closed" {
		t.Fatalf("empty state = %q, want closed", state)
	}
}

// Five consecutive failures opens the breaker. Subsequent Allow calls
// during the cooldown return false with a non-zero retry-after.
func TestBreaker_OpensAfterThresholdConsecutiveFailures(t *testing.T) {
	t.Parallel()
	b, _ := newTestBreaker(t)

	// Threshold-1 failures: still closed.
	for i := 0; i < breakerThreshold-1; i++ {
		b.RecordFailure("c-1")
	}
	if state, _ := b.State("c-1"); state != "closed" {
		t.Fatalf("after %d failures state = %q, want closed",
			breakerThreshold-1, state)
	}

	// Final failure trips it.
	b.RecordFailure("c-1")
	state, remaining := b.State("c-1")
	if state != "open" {
		t.Fatalf("after %d failures state = %q, want open", breakerThreshold, state)
	}
	if remaining != breakerInitialCooldown {
		t.Fatalf("initial cooldown = %v, want %v", remaining, breakerInitialCooldown)
	}

	allowed, retry := b.Allow("c-1")
	if allowed {
		t.Fatal("Allow during open cooldown should refuse")
	}
	if retry <= 0 || retry > breakerInitialCooldown {
		t.Fatalf("retry-after = %v, want (0, %v]", retry, breakerInitialCooldown)
	}
}

// One success in the closed state resets the failure counter so a
// subsequent burst doesn't trip the breaker on its prior near-miss.
func TestBreaker_SuccessResetsClosedCounter(t *testing.T) {
	t.Parallel()
	b, _ := newTestBreaker(t)

	for i := 0; i < breakerThreshold-1; i++ {
		b.RecordFailure("c-1")
	}
	b.RecordSuccess("c-1")

	// Now do threshold-1 fresh failures — should still be closed.
	for i := 0; i < breakerThreshold-1; i++ {
		b.RecordFailure("c-1")
	}
	if state, _ := b.State("c-1"); state != "closed" {
		t.Fatalf("after success + %d new failures state = %q, want closed",
			breakerThreshold-1, state)
	}
}

// Once the cooldown elapses, the next Allow promotes the breaker to
// half-open and lets exactly one caller through. Concurrent callers
// during the trial must be refused.
func TestBreaker_CooldownExpires_PromotesToHalfOpen_OneTrialOnly(t *testing.T) {
	t.Parallel()
	b, mc := newTestBreaker(t)

	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-1")
	}
	if state, _ := b.State("c-1"); state != "open" {
		t.Fatalf("setup: want open, got %s", state)
	}

	// Advance past the cooldown window.
	mc.Advance(breakerInitialCooldown + time.Second)

	// First caller is the trial — allowed, breaker now half-open.
	allowed, _ := b.Allow("c-1")
	if !allowed {
		t.Fatal("first caller after cooldown should be the trial")
	}
	if state, _ := b.State("c-1"); state != "half-open" {
		t.Fatalf("after trial start state = %q, want half-open", state)
	}

	// Concurrent caller is refused while the trial is in flight.
	allowed2, retry2 := b.Allow("c-1")
	if allowed2 {
		t.Fatal("second caller during half-open trial should be refused")
	}
	if retry2 <= 0 {
		t.Fatalf("retry-after during half-open trial = %v, want > 0", retry2)
	}
}

// A successful trial closes the breaker and clears the cooldown.
func TestBreaker_HalfOpenSuccess_Closes(t *testing.T) {
	t.Parallel()
	b, mc := newTestBreaker(t)
	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-1")
	}
	mc.Advance(breakerInitialCooldown + time.Second)
	_, _ = b.Allow("c-1") // claim the trial
	b.RecordSuccess("c-1")

	state, remaining := b.State("c-1")
	if state != "closed" || remaining != 0 {
		t.Fatalf("after half-open success state = (%q, %v), want (closed, 0)",
			state, remaining)
	}

	// Subsequent calls are unrestricted.
	allowed, _ := b.Allow("c-1")
	if !allowed {
		t.Fatal("after recovery the breaker should let traffic through")
	}
}

// A failed trial re-opens the breaker with a longer cooldown
// (exponential backoff up to breakerMaxCooldown).
func TestBreaker_HalfOpenFailure_ReOpensWithLongerCooldown(t *testing.T) {
	t.Parallel()
	b, mc := newTestBreaker(t)
	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-1")
	}

	prevCooldown := breakerInitialCooldown
	for trip := 0; trip < 5; trip++ {
		mc.Advance(prevCooldown + time.Second)
		_, _ = b.Allow("c-1") // claim trial
		b.RecordFailure("c-1")

		state, remaining := b.State("c-1")
		if state != "open" {
			t.Fatalf("trip %d: state = %q, want open", trip, state)
		}
		if remaining < prevCooldown {
			t.Fatalf("trip %d: cooldown shrank %v -> %v", trip, prevCooldown, remaining)
		}
		if remaining > breakerMaxCooldown {
			t.Fatalf("trip %d: cooldown %v exceeds max %v",
				trip, remaining, breakerMaxCooldown)
		}
		prevCooldown = remaining
	}

	if prevCooldown != breakerMaxCooldown {
		t.Fatalf("cooldown should have saturated at %v, got %v",
			breakerMaxCooldown, prevCooldown)
	}
}

// If a half-open trial neither succeeds nor fails within the trial
// timeout, the slot is forfeited and another caller can probe.
func TestBreaker_HalfOpenTrialTimeout_NextCallerProbes(t *testing.T) {
	t.Parallel()
	b, mc := newTestBreaker(t)
	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-1")
	}
	mc.Advance(breakerInitialCooldown + time.Second)

	// First caller claims the trial slot but never reports.
	allowed1, _ := b.Allow("c-1")
	if !allowed1 {
		t.Fatal("setup: first allow should succeed")
	}

	// Inside the trial-timeout window the slot is locked.
	mc.Advance(breakerTrialTimeout / 2)
	if a, _ := b.Allow("c-1"); a {
		t.Fatal("inside trial-timeout window the slot must be locked")
	}

	// Past the trial timeout, the slot is forfeited.
	mc.Advance(breakerTrialTimeout)
	if a, _ := b.Allow("c-1"); !a {
		t.Fatal("after trial timeout another caller should probe")
	}
}

// Prune drops idle closed entries so a long-running server with
// millions of channels doesn't grow an unbounded breaker map.
// Channels with current failures or non-closed states must survive.
func TestBreaker_Prune_DropsIdleClosedEntries(t *testing.T) {
	t.Parallel()
	b, mc := newTestBreaker(t)

	// "kept-open" — open, must survive.
	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("kept-open")
	}
	// "kept-failures" — closed but with failures>0, must survive.
	b.RecordFailure("kept-failures")
	// "evict" — closed, no failures, idle.
	b.RecordFailure("evict")
	b.RecordSuccess("evict") // counter back to 0, state closed

	mc.Advance(breakerIdleEvictAfter + time.Minute)
	b.Prune()

	if _, ok := b.entries["kept-open"]; !ok {
		t.Error("open entry should not be pruned")
	}
	if _, ok := b.entries["kept-failures"]; !ok {
		t.Error("closed-with-failures entry should not be pruned")
	}
	if _, ok := b.entries["evict"]; ok {
		t.Error("idle closed entry should have been pruned")
	}
}

// Concurrent Allow / RecordFailure / RecordSuccess from many
// goroutines must not race or deadlock. Only checked for safety here;
// state correctness is covered by the other tests.
func TestBreaker_ConcurrentAccess_NoRace(t *testing.T) {
	b, _ := newTestBreaker(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := "c-" + string(rune('a'+(id%5)))
			for j := 0; j < 100; j++ {
				switch j % 3 {
				case 0:
					b.Allow(ch)
				case 1:
					b.RecordFailure(ch)
				case 2:
					b.RecordSuccess(ch)
				}
			}
		}(i)
	}
	wg.Wait()
}

// Las 4 transiciones del breaker deben dejar rastro en el logger
// para que el operador sepa cuándo un canal entró/salió en
// cooldown sin tener que pollear BreakerState() desde la admin.
func TestBreaker_LogsClosedToOpenTransition(t *testing.T) {
	t.Parallel()
	b, _, buf := newTestBreakerWithLogger(t)

	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-trip")
	}

	logged := buf.String()
	if !bytes.Contains([]byte(logged), []byte("circuit breaker opened")) {
		t.Errorf("expected 'circuit breaker opened' log, got: %s", logged)
	}
	if !bytes.Contains([]byte(logged), []byte("channel=c-trip")) {
		t.Errorf("expected channel field in log, got: %s", logged)
	}
}

func TestBreaker_LogsHalfOpenToOpenTransition(t *testing.T) {
	t.Parallel()
	b, mc, buf := newTestBreakerWithLogger(t)

	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-retrip")
	}
	// Avanza el reloj para que el cooldown expire y la siguiente
	// Allow promueva a half-open.
	mc.CurrentTime = mc.CurrentTime.Add(breakerInitialCooldown + time.Second)
	allowed, _ := b.Allow("c-retrip")
	if !allowed {
		t.Fatal("Allow should have promoted to half-open")
	}
	// El trial falla → vuelve a open con cooldown doblado.
	buf.Reset() // sólo nos interesa el re-open
	b.RecordFailure("c-retrip")

	logged := buf.String()
	if !bytes.Contains([]byte(logged), []byte("circuit breaker re-opened after failed trial")) {
		t.Errorf("expected re-open log, got: %s", logged)
	}
}

func TestBreaker_LogsHalfOpenTrialAttempt(t *testing.T) {
	t.Parallel()
	b, mc, buf := newTestBreakerWithLogger(t)

	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-trial")
	}
	mc.CurrentTime = mc.CurrentTime.Add(breakerInitialCooldown + time.Second)
	buf.Reset() // ignoramos el log de open

	if allowed, _ := b.Allow("c-trial"); !allowed {
		t.Fatal("Allow should have promoted to half-open")
	}

	logged := buf.String()
	if !bytes.Contains([]byte(logged), []byte("circuit breaker half-open trial")) {
		t.Errorf("expected half-open trial log, got: %s", logged)
	}
}

func TestBreaker_LogsRecoveryFromHalfOpen(t *testing.T) {
	t.Parallel()
	b, mc, buf := newTestBreakerWithLogger(t)

	for i := 0; i < breakerThreshold; i++ {
		b.RecordFailure("c-recover")
	}
	mc.CurrentTime = mc.CurrentTime.Add(breakerInitialCooldown + time.Second)
	_, _ = b.Allow("c-recover") // promueve a half-open
	buf.Reset()                 // sólo nos interesa el recovery

	b.RecordSuccess("c-recover")

	logged := buf.String()
	if !bytes.Contains([]byte(logged), []byte("circuit breaker recovered")) {
		t.Errorf("expected recovered log, got: %s", logged)
	}
}

func TestBreaker_RecordSuccessOnClosedDoesNotLog(t *testing.T) {
	// RecordSuccess sobre un breaker closed sin half-open previo
	// no es transición — no debe ensuciar logs con "recovered"
	// cuando no había fallo del que recuperarse.
	t.Parallel()
	b, _, buf := newTestBreakerWithLogger(t)

	b.RecordFailure("c-quiet") // un fallo, queda closed (no llega al threshold)
	buf.Reset()
	b.RecordSuccess("c-quiet")

	if buf.Len() > 0 {
		t.Errorf("RecordSuccess on closed should not log, got: %s", buf.String())
	}
}
