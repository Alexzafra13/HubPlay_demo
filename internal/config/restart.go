package config

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// RestartRequester: dispara graceful shutdown desde un handler.
//
// En Docker (el target del proyecto) `restart: unless-stopped` resube el
// container — eso convierte el botón admin "Reiniciar" en una acción real
// sin que el operador toque el shell del host.
//
// Trigger one-shot: llamadas posteriores son no-op silenciosas (un doble
// click o retry de proxy flaky no programa dos cancels).
//
// El delay antes de cancel da tiempo al handler a flushear el 202 — sin él
// el operador ve connection reset, que parece error aunque el shutdown
// vaya bien.
type RestartRequester struct {
	cancel    context.CancelFunc
	delay     time.Duration
	triggered atomic.Bool
	logger    *slog.Logger
}

// NewRestartRequester: cablea el cancel que escucha el run loop principal.
// 100 ms basta para que cualquier framework HTTP flushee la respuesta.
func NewRestartRequester(cancel context.CancelFunc, logger *slog.Logger) *RestartRequester {
	return &RestartRequester{
		cancel: cancel,
		delay:  100 * time.Millisecond,
		logger: logger.With("module", "restart-requester"),
	}
}

// Request: programa shutdown tras el delay. Idempotente — siguientes calls
// devuelven false. `reason` queda en el log para greppear qué disparó.
func (r *RestartRequester) Request(reason string) bool {
	if !r.triggered.CompareAndSwap(false, true) {
		return false
	}
	r.logger.Info("restart requested", "reason", reason, "delay", r.delay)
	time.AfterFunc(r.delay, r.cancel)
	return true
}
