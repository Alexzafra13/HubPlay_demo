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

// RequireParam extrae un parámetro de URL de chi por nombre y escribe una
// respuesta 400 si está vacío. Devuelve el valor; los callers deben retornar
// inmediatamente cuando el resultado es "".
func RequireParam(w http.ResponseWriter, r *http.Request, name string) string {
	v := chi.URLParam(r, name)
	if v == "" {
		RespondError(w, r, http.StatusBadRequest, "MISSING_PARAM", "missing path parameter: "+name)
	}
	return v
}

// RequirePeer extrae el *federation.Peer del contexto que pone
// RequirePeerJWT. Si falta (handler montado fuera del group protegido,
// o test que se olvida de inyectar) escribe 500 y devuelve nil — los
// callers deben retornar inmediatamente. Análogo a RequireParam.
func RequirePeer(w http.ResponseWriter, r *http.Request) *federation.Peer {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		RespondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
	}
	return peer
}

// SetErrorRecorder instala el hook de observability que se dispara por cada
// AppError renderizado. Wrapper fino sobre apperror.SetRecorder mantenido en
// la superficie de handlers para que el wiring de router.go (que ya importa
// handlers) no necesite un segundo import.
func SetErrorRecorder(fn func(code string)) {
	apperror.SetRecorder(fn)
}

func RespondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// respondData es el atajo para el envelope canónico `{"data": payload}`.
// Elimina 115 sites de `map[string]any{"data": ...}` dispersos en los
// handlers (audit F14-6-a). Mismo wire shape — sólo compacta el caller.
func RespondData(w http.ResponseWriter, status int, payload any) {
	RespondJSON(w, status, struct {
		Data any `json:"data"`
	}{payload})
}

func DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

const PaginationMaxLimit = 500

// ParsePagination extrae offset y limit de los query parameters con
// validación: ambos deben ser no-negativos, limit se acota a
// PaginationMaxLimit. Devuelve (offset, limit, ok). Cuando ok es false
// la respuesta ya ha sido escrita.
func ParsePagination(w http.ResponseWriter, r *http.Request) (offset, limit int, ok bool) {
	return ParsePaginationFromValues(w, r, r.URL.Query())
}

func ParsePaginationFromValues(w http.ResponseWriter, r *http.Request, q url.Values) (offset, limit int, ok bool) {
	offset, _ = strconv.Atoi(q.Get("offset"))
	limit, _ = strconv.Atoi(q.Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if limit > PaginationMaxLimit {
		limit = PaginationMaxLimit
	}
	return offset, limit, true
}

// RespondAppError escribe un AppError como respuesta JSON. Wrapper fino
// sobre apperror.Write para que los call sites de handlers queden concisos y
// la lógica de envelope/recorder/Retry-After viva en un solo sitio.
func RespondAppError(w http.ResponseWriter, ctx context.Context, appErr *domain.AppError) {
	apperror.Write(w, ctx, appErr)
}

// RespondError escribe una respuesta de error ad-hoc. Preferir devolver un
// AppError desde la capa de servicio y dejar que HandleServiceError lo
// renderice; este helper existe para la validación de input local al handler
// donde construir un AppError es excesivo.
func RespondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	RespondAppError(w, r.Context(), &domain.AppError{
		Code:       code,
		HTTPStatus: status,
		Message:    message,
	})
}

// HandleServiceError mapea un error de la capa de servicio a una respuesta HTTP.
//
// Orden de resolución:
//  1. *AppError → renderizado directamente (caso más rico).
//  2. *ValidationError → 400 con detalles por campo.
//  3. Errores sentinel (errors.Is) → mapeados al AppError correspondiente.
//  4. Cualquier otra cosa → 500 con INTERNAL_ERROR, causa logueada para operadores.
func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()

	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		// Un 5xx que viene como AppError pre-construido (típico:
		// `domain.NewInternal(err)`) no pasa por el default case
		// de abajo, así que sin este log el operador veía un 500
		// en el frontend sin causa en los logs.
		if appErr.HTTPStatus >= 500 {
			slog.Error("service error",
				"error", err,
				"code", appErr.Code,
				"status", appErr.HTTPStatus,
				"request_id", middleware.GetReqID(ctx),
				"path", r.URL.Path,
			)
		}
		RespondAppError(w, ctx, appErr)
		return
	}

	var valErr *domain.ValidationError
	if errors.As(err, &valErr) {
		RespondAppError(w, ctx, domain.NewValidation(valErr.Fields))
		return
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		RespondAppError(w, ctx, domain.NewNotFound("resource"))
	case errors.Is(err, domain.ErrAlreadyExists):
		RespondAppError(w, ctx, domain.NewAlreadyExists("resource"))
	case errors.Is(err, domain.ErrInvalidPassword):
		RespondAppError(w, ctx, domain.NewInvalidCredentials())
	case errors.Is(err, domain.ErrTokenExpired):
		RespondAppError(w, ctx, domain.NewTokenExpired())
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidToken):
		RespondAppError(w, ctx, domain.NewUnauthorized(""))
	case errors.Is(err, domain.ErrAccessExpired):
		RespondAppError(w, ctx, domain.NewAccessExpired())
	case errors.Is(err, domain.ErrAccountDisabled):
		RespondAppError(w, ctx, domain.NewAccountDisabled())
	case errors.Is(err, domain.ErrForbidden):
		RespondAppError(w, ctx, domain.NewForbidden(""))
	case errors.Is(err, domain.ErrConflict):
		RespondAppError(w, ctx, domain.NewConflict("operation conflicts with current state"))
	case errors.Is(err, domain.ErrValidation):
		RespondAppError(w, ctx, domain.NewValidation(nil))
	default:
		// Error interno: loguear la causa (con request_id para correlación) pero
		// nunca exponerla al cliente.
		slog.Error("unhandled error",
			"error", err,
			"request_id", middleware.GetReqID(ctx),
			"path", r.URL.Path,
		)
		RespondAppError(w, ctx, domain.NewInternal(err))
	}
}
