package iptv

// Test-only hooks that live outside the production binary. Go's
// build system only compiles *_test.go files during `go test`, so
// these functions are invisible to callers outside tests and don't
// appear in the shipped binary.

import (
	"context"
	"time"
)

// TickOnce runs exactly one polling pass. Used by tests to avoid
// sleeping for tickInterval; production uses the internal loop.
func (s *Scheduler) TickOnce(ctx context.Context) { s.tick(ctx) }

// SetTickInterval lets tests speed up the loop without spawning a
// real ticker-based wait.
func (s *Scheduler) SetTickInterval(d time.Duration) { s.tickInterval = d }
