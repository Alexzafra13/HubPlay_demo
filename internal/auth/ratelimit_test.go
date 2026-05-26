package auth

import (
	"testing"
	"time"

	"hubplay/internal/clock"
)

// TestLoginRateLimiter_StopIsIdempotent verifica que Stop() puede
// llamarse N veces sin panic ("close of closed channel"). El dueño
// (auth.Service.StopSessionCleaner) lo invoca como parte del
// shutdown; si el operador llama Shutdown dos veces, no se rompe
// el proceso (audit olor RR).
func TestLoginRateLimiter_StopIsIdempotent(t *testing.T) {
	rl := newLoginRateLimiter(5, time.Minute, 5*time.Minute, clock.New())
	rl.Stop()
	rl.Stop() // no debe panic
	rl.Stop() // tampoco esta tercera
}

// TestLoginRateLimiter_StopClosesGoroutine: tras Stop(), la
// goroutine de cleanup debería haber salido. Como el ticker es de
// 10 min no esperamos a un tick; verificamos solo que el cierre del
// canal libera al select.
func TestLoginRateLimiter_StopClosesGoroutine(t *testing.T) {
	rl := newLoginRateLimiter(5, time.Minute, 5*time.Minute, clock.New())
	rl.Stop()
	// El canal está cerrado: cualquier receive devuelve inmediatamente
	// el zero-value. Confirma que Stop hizo lo prometido.
	select {
	case <-rl.stopCh:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stopCh no se cerró tras Stop()")
	}
}

func TestLoginRateLimiter_LockoutAndExpiry(t *testing.T) {
	t.Parallel()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	rl := newLoginRateLimiter(3, 10*time.Minute, 5*time.Minute, clk)
	t.Cleanup(rl.Stop)

	rl.recordFailure("user1")
	rl.recordFailure("user1")
	locked := rl.recordFailure("user1")
	if !locked {
		t.Fatal("expected lockout after 3 failures")
	}
	if !rl.isLocked("user1") {
		t.Fatal("expected user1 to be locked")
	}

	clk.Advance(4 * time.Minute)
	if !rl.isLocked("user1") {
		t.Fatal("expected user1 still locked before lockout expires")
	}

	clk.Advance(2 * time.Minute)
	if rl.isLocked("user1") {
		t.Fatal("expected user1 unlocked after lockout expires")
	}
}

func TestLoginRateLimiter_WindowReset(t *testing.T) {
	t.Parallel()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	rl := newLoginRateLimiter(3, time.Minute, 5*time.Minute, clk)
	t.Cleanup(rl.Stop)

	rl.recordFailure("user2")
	rl.recordFailure("user2")

	clk.Advance(2 * time.Minute)
	locked := rl.recordFailure("user2")
	if locked {
		t.Fatal("window expired — third failure should start a new window, not lock")
	}
}

func TestLoginRateLimiter_CleanupWithMockClock(t *testing.T) {
	t.Parallel()
	clk := &clock.Mock{CurrentTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	rl := newLoginRateLimiter(5, time.Minute, 2*time.Minute, clk)
	t.Cleanup(rl.Stop)

	rl.recordFailure("a")
	rl.recordFailure("b")

	clk.Advance(3 * time.Minute)
	rl.cleanup()

	rl.mu.Lock()
	remaining := len(rl.attempts)
	rl.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected 0 entries after cleanup, got %d", remaining)
	}
}
