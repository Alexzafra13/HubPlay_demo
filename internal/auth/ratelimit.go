package auth

import (
	"sync"
	"time"
)

// loginRateLimiter trackea login fails por username/IP para frenar
// brute-force. Cierra su goroutine de cleanup vía Stop() — el dueño
// (`auth.Service`) lo llama desde StopSessionCleaner para evitar
// leak de goroutine en tests que crean varios services
// (audit olor RR).
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptRecord
	window   time.Duration
	maxFails int
	lockout  time.Duration

	stopCh chan struct{}
	once   sync.Once
}

type attemptRecord struct {
	failures    int
	firstFail   time.Time
	lockedUntil time.Time
}

func newLoginRateLimiter(maxFails int, window, lockout time.Duration) *loginRateLimiter {
	rl := &loginRateLimiter{
		attempts: make(map[string]*attemptRecord),
		window:   window,
		maxFails: maxFails,
		lockout:  lockout,
		stopCh:   make(chan struct{}),
	}
	// Cleanup periódico de entradas obsoletas. Sale al cerrar stopCh.
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-rl.stopCh:
				return
			}
		}
	}()
	return rl
}

// Stop detiene la goroutine de cleanup. Idempotente.
func (rl *loginRateLimiter) Stop() {
	rl.once.Do(func() { close(rl.stopCh) })
}

// isLocked indica si la key (username o IP) está actualmente bloqueada.
func (rl *loginRateLimiter) isLocked(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, ok := rl.attempts[key]
	if !ok {
		return false
	}

	now := time.Now()

	// Si el lockout expiró, limpiar.
	if !rec.lockedUntil.IsZero() && now.After(rec.lockedUntil) {
		delete(rl.attempts, key)
		return false
	}

	// Si la ventana expiró, limpiar.
	if now.Sub(rec.firstFail) > rl.window {
		delete(rl.attempts, key)
		return false
	}

	return !rec.lockedUntil.IsZero() && now.Before(rec.lockedUntil)
}

// recordFailure cuenta un fallo. Devuelve true si la key acaba bloqueada.
func (rl *loginRateLimiter) recordFailure(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rec, ok := rl.attempts[key]
	if !ok || now.Sub(rec.firstFail) > rl.window {
		rec = &attemptRecord{firstFail: now}
		rl.attempts[key] = rec
	}

	rec.failures++
	if rec.failures >= rl.maxFails {
		rec.lockedUntil = now.Add(rl.lockout)
		return true
	}
	return false
}

// recordSuccess limpia el registro de fallos para una key.
func (rl *loginRateLimiter) recordSuccess(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, key)
}

func (rl *loginRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, rec := range rl.attempts {
		if !rec.lockedUntil.IsZero() && now.After(rec.lockedUntil) {
			delete(rl.attempts, key)
		} else if now.Sub(rec.firstFail) > rl.window {
			delete(rl.attempts, key)
		}
	}
}
