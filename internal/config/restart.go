package config

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// RestartRequester triggers a graceful self-shutdown from a handler.
//
// Under Docker (the deployment shape this project targets) the
// `restart: unless-stopped` policy brings the container back up
// automatically, which is what makes the admin "Restart" button a
// real button — the operator does not have to touch the host shell
// to apply a config change that requires re-init (driver swap, JWT
// rotation seeds, etc.).
//
// The trigger is one-shot: subsequent calls are silent no-ops so a
// double-click on the panel or a retry from a flaky proxy can't
// schedule two cancellations.
//
// The delay before cancel gives the HTTP handler a chance to flush
// the JSON 202 response — without it the operator sees a connection
// reset on the restart click, which looks like a failure even though
// the server is shutting down correctly.
type RestartRequester struct {
	cancel    context.CancelFunc
	delay     time.Duration
	triggered atomic.Bool
	logger    *slog.Logger
}

// NewRestartRequester wires the cancel function the main run loop
// listens on. Delay is the wait before cancel fires; 100ms is enough
// for any reasonable HTTP framework to flush the response.
func NewRestartRequester(cancel context.CancelFunc, logger *slog.Logger) *RestartRequester {
	return &RestartRequester{
		cancel: cancel,
		delay:  100 * time.Millisecond,
		logger: logger.With("module", "restart-requester"),
	}
}

// Request schedules a graceful shutdown after the configured delay.
// Idempotent — subsequent calls return false and do not re-trigger.
// Reason is recorded in the log line so the operator can grep for
// what flipped the switch.
func (r *RestartRequester) Request(reason string) bool {
	if !r.triggered.CompareAndSwap(false, true) {
		return false
	}
	r.logger.Info("restart requested", "reason", reason, "delay", r.delay)
	time.AfterFunc(r.delay, r.cancel)
	return true
}
