package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/domain"
)

// errorPayload is the envelope returned for every API error.
// Fields with omitempty are only rendered when the AppError sets them,
// keeping the response compact for simple cases.
type errorPayload struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Hint      string         `json:"hint,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

// errorRecorder is the observability hook invoked for every rendered
// AppError. It is overwritten via SetErrorRecorder at wiring time; tests and
// the default path use the no-op to avoid a hard dependency on Prometheus
// (and the import cycle that would come with it).
var errorRecorder = func(code string) {}

// SetErrorRecorder installs a function that will be called with the Code of
// every AppError rendered by the handler package. Pass nil to disable.
//
// The recorder is called after the HTTP response has been written so a slow
// metrics backend cannot delay client-facing latency.
func SetErrorRecorder(fn func(code string)) {
	if fn == nil {
		errorRecorder = func(string) {}
		return
	}
	errorRecorder = fn
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// respondAppError writes an AppError as a JSON response, attaching the chi
// request ID for correlation and setting Retry-After when present. It also
// notifies the observability hook (see SetErrorRecorder) so every rendered
// error code shows up as a metric — no handler code needed on call sites.
func respondAppError(w http.ResponseWriter, ctx context.Context, appErr *domain.AppError) {
	if appErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(appErr.RetryAfter.Seconds())))
	}
	respondJSON(w, appErr.HTTPStatus, map[string]any{
		"error": errorPayload{
			Code:      appErr.Code,
			Message:   appErr.Message,
			Hint:      appErr.Hint,
			Details:   appErr.Details,
			RequestID: middleware.GetReqID(ctx),
		},
	})
	errorRecorder(appErr.Code)
}

// respondError writes an ad-hoc error response. Prefer returning an AppError
// from the service layer and letting handleServiceError render it; this helper
// exists for handler-local input validation where building an AppError is
// overkill.
func respondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	respondAppError(w, r.Context(), &domain.AppError{
		Code:       code,
		HTTPStatus: status,
		Message:    message,
	})
}

// handleServiceError maps a service-layer error to an HTTP response.
//
// Resolution order:
//  1. *AppError → rendered directly (richest case).
//  2. *ValidationError → 400 with per-field details.
//  3. Sentinel errors (errors.Is) → mapped to the matching AppError.
//  4. Anything else → 500 with INTERNAL_ERROR, cause logged for operators.
func handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()

	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		respondAppError(w, ctx, appErr)
		return
	}

	var valErr *domain.ValidationError
	if errors.As(err, &valErr) {
		respondAppError(w, ctx, domain.NewValidation(valErr.Fields))
		return
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		respondAppError(w, ctx, domain.NewNotFound("resource"))
	case errors.Is(err, domain.ErrAlreadyExists):
		respondAppError(w, ctx, domain.NewAlreadyExists("resource"))
	case errors.Is(err, domain.ErrInvalidPassword):
		respondAppError(w, ctx, domain.NewInvalidCredentials())
	case errors.Is(err, domain.ErrTokenExpired):
		respondAppError(w, ctx, domain.NewTokenExpired())
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidToken):
		respondAppError(w, ctx, domain.NewUnauthorized(""))
	case errors.Is(err, domain.ErrAccountDisabled):
		respondAppError(w, ctx, domain.NewAccountDisabled())
	case errors.Is(err, domain.ErrForbidden):
		respondAppError(w, ctx, domain.NewForbidden(""))
	case errors.Is(err, domain.ErrConflict):
		respondAppError(w, ctx, domain.NewConflict("operation conflicts with current state"))
	case errors.Is(err, domain.ErrValidation):
		respondAppError(w, ctx, domain.NewValidation(nil))
	default:
		// Internal error: log the cause (with request_id for correlation) but
		// never expose it to the client.
		slog.Error("unhandled error",
			"error", err,
			"request_id", middleware.GetReqID(ctx),
			"path", r.URL.Path,
		)
		respondAppError(w, ctx, domain.NewInternal(err))
	}
}
