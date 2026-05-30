// Package apperror is the canonical writer for HTTP error responses.
//
// Every error rendered by the API — handlers, middleware, CSRF — flows
// through Write so the wire envelope, the request_id correlation, the
// Retry-After header, and the metrics recorder stay consistent across
// layers. Before this package existed, auth.Middleware and csrf.go
// hand-rolled JSON strings via http.Error, which bypassed the error
// recorder (errors didn't show up in hubplay_errors_total) and emitted
// Content-Type: text/plain. Centralising here closes that gap.
//
// Why a separate package and not inside `handlers`: the auth middleware
// lives in `internal/auth`, which `handlers` already imports — letting
// handlers expose the writer would create an import cycle. apperror
// has no dependency on either side, so both can call it freely.
package apperror

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/domain"
)

// Payload is the wire envelope every API error response uses.
// Fields with omitempty stay off the wire when the AppError leaves
// them blank, keeping responses compact for the simple cases.
type Payload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Hint      string         `json:"hint,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// recorder es el hook de observabilidad que se dispara tras renderizar
// cada error. Se guarda en un atomic.Pointer para que SetRecorder (init
// del proceso, o tests en paralelo con sus propios fakes) y la lectura
// en Write nunca compitan: antes era un `var recorder func(...)` plano
// que un t.Parallel podía pisar a mitad de un Write (data race). Por
// defecto es un no-op para que tests y paquetes sin métricas no paguen
// el coste de un ciclo de import contra observability.
var recorder atomic.Pointer[func(code string)]

func init() {
	noop := func(string) {}
	recorder.Store(&noop)
}

// SetRecorder instala el hook de observabilidad por-error. Pasa nil para
// volver al no-op. El recorder corre DESPUÉS de escribir la respuesta,
// así un backend de métricas lento nunca añade latencia al cliente.
func SetRecorder(fn func(code string)) {
	if fn == nil {
		noop := func(string) {}
		recorder.Store(&noop)
		return
	}
	recorder.Store(&fn)
}

// Write renders an AppError as the canonical JSON envelope and fires
// the metrics recorder. Sets Retry-After when the AppError carries a
// non-zero RetryAfter (rate-limit responses).
//
// Callers MUST NOT have written to the ResponseWriter before this
// call — Write owns Content-Type + status code.
func Write(w http.ResponseWriter, ctx context.Context, appErr *domain.AppError) {
	if appErr == nil {
		appErr = domain.NewInternal(nil)
	}
	if appErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(appErr.RetryAfter.Seconds())))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)
	body := map[string]any{
		"error": Payload{
			Code:      appErr.Code,
			Message:   appErr.Message,
			Hint:      appErr.Hint,
			Details:   appErr.Details,
			RequestID: middleware.GetReqID(ctx),
		},
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("apperror: encode response failed", "error", err)
	}
	(*recorder.Load())(appErr.Code)
}
