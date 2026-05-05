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

// recorder is the observability hook fired after every rendered error.
// Defaults to a no-op so tests and packages that don't wire metrics
// don't pay an import-cycle cost on the observability package.
var recorder = func(code string) {}

// SetRecorder installs the per-error observability hook. Pass nil to
// reset to the no-op. The recorder runs after the response is written
// so a slow metrics backend never adds latency to the client.
func SetRecorder(fn func(code string)) {
	if fn == nil {
		recorder = func(string) {}
		return
	}
	recorder = fn
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
	recorder(appErr.Code)
}
