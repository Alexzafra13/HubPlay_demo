package auth

import (
	"testing"
	"time"
)

// TestLoginRateLimiter_StopIsIdempotent verifica que Stop() puede
// llamarse N veces sin panic ("close of closed channel"). El dueño
// (auth.Service.StopSessionCleaner) lo invoca como parte del
// shutdown; si el operador llama Shutdown dos veces, no se rompe
// el proceso (audit olor RR).
func TestLoginRateLimiter_StopIsIdempotent(t *testing.T) {
	rl := newLoginRateLimiter(5, time.Minute, 5*time.Minute)
	rl.Stop()
	rl.Stop() // no debe panic
	rl.Stop() // tampoco esta tercera
}

// TestLoginRateLimiter_StopClosesGoroutine: tras Stop(), la
// goroutine de cleanup debería haber salido. Como el ticker es de
// 10 min no esperamos a un tick; verificamos solo que el cierre del
// canal libera al select.
func TestLoginRateLimiter_StopClosesGoroutine(t *testing.T) {
	rl := newLoginRateLimiter(5, time.Minute, 5*time.Minute)
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
