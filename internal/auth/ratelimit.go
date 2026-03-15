package auth

import (
	"sync"
	"time"
)

// loginRateLimiter tracks failed login attempts per username/IP to prevent brute-force.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptRecord
	window   time.Duration
	maxFails int
	lockout  time.Duration
}

type attemptRecord struct {
	failures  int
	firstFail time.Time
	lockedUntil time.Time
}

func newLoginRateLimiter(maxFails int, window, lockout time.Duration) *loginRateLimiter {
	rl := &loginRateLimiter{
		attempts: make(map[string]*attemptRecord),
		window:   window,
		maxFails: maxFails,
		lockout:  lockout,
	}
	// Background cleanup of stale entries every 10 minutes
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

// isLocked returns true if the key (username or IP) is currently locked out.
func (rl *loginRateLimiter) isLocked(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, ok := rl.attempts[key]
	if !ok {
		return false
	}

	now := time.Now()

	// Check if lockout has expired
	if !rec.lockedUntil.IsZero() && now.After(rec.lockedUntil) {
		delete(rl.attempts, key)
		return false
	}

	// Check if window has expired
	if now.Sub(rec.firstFail) > rl.window {
		delete(rl.attempts, key)
		return false
	}

	return !rec.lockedUntil.IsZero() && now.Before(rec.lockedUntil)
}

// recordFailure records a failed login attempt. Returns true if the account is now locked.
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

// recordSuccess clears the failure record for a key.
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
