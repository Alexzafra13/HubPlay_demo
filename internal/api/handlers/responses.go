package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/api/apperror"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// requireParam extracts a chi URL parameter by name and writes a 400
// response if it is empty. Returns the value; callers should return
// immediately when the result is "".
func requireParam(w http.ResponseWriter, r *http.Request, name string) string {
	v := chi.URLParam(r, name)
	if v == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAM", "missing path parameter: "+name)
	}
	return v
}

// requirePeer extrae el *federation.Peer del contexto que pone
// RequirePeerJWT. Si falta (handler montado fuera del group protegido,
// o test que se olvida de inyectar) escribe 500 y devuelve nil — los
// callers deben retornar inmediatamente. Análogo a requireParam.
func requirePeer(w http.ResponseWriter, r *http.Request) *federation.Peer {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
	}
	return peer
}

// SetErrorRecorder installs the observability hook fired for every
// rendered AppError. Thin wrapper around apperror.SetRecorder kept on
// the handlers surface so router.go wiring (which already imports
// handlers) doesn't need a second import.
func SetErrorRecorder(fn func(code string)) {
	apperror.SetRecorder(fn)
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// respondData es el atajo para el envelope canónico `{"data": payload}`.
// Elimina 115 sites de `map[string]any{"data": ...}` dispersos en los
// handlers (audit F14-6-a). Mismo wire shape — sólo compacta el caller.
func respondData(w http.ResponseWriter, status int, payload any) {
	respondJSON(w, status, struct {
		Data any `json:"data"`
	}{payload})
}

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

const paginationMaxLimit = 500

// parsePagination extracts offset and limit from query parameters with
// validation: both must be non-negative, limit is capped at
// paginationMaxLimit. Returns (offset, limit, ok). When ok is false
// the response has already been written.
func parsePagination(w http.ResponseWriter, r *http.Request) (offset, limit int, ok bool) {
	return parsePaginationFromValues(w, r, r.URL.Query())
}

func parsePaginationFromValues(w http.ResponseWriter, r *http.Request, q url.Values) (offset, limit int, ok bool) {
	offset, _ = strconv.Atoi(q.Get("offset"))
	limit, _ = strconv.Atoi(q.Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if limit > paginationMaxLimit {
		limit = paginationMaxLimit
	}
	return offset, limit, true
}

// respondAppError writes an AppError as a JSON response. Thin wrapper
// around apperror.Write so handler call sites stay terse and the
// envelope/recorder/Retry-After logic lives in one place.
func respondAppError(w http.ResponseWriter, ctx context.Context, appErr *domain.AppError) {
	apperror.Write(w, ctx, appErr)
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
	case errors.Is(err, domain.ErrAccessExpired):
		respondAppError(w, ctx, domain.NewAccessExpired())
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
