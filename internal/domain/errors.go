package domain

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Sentinels para errors.Is
var (
	// Recursos
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("conflict")

	// Auth
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrInvalidToken    = errors.New("invalid token")
	ErrTokenExpired    = errors.New("token expired")
	ErrInvalidPassword = errors.New("invalid password")
	ErrAccountDisabled = errors.New("account disabled")
	ErrAccessExpired   = errors.New("access expired")

	// Validación
	ErrValidation = errors.New("validation error")

	// Streaming
	ErrTranscodeBusy    = errors.New("transcode slots full")
	ErrUnsupportedCodec = errors.New("unsupported codec")

	// Federación
	ErrPeerOffline           = errors.New("peer offline")
	ErrPeerUnauthorized      = errors.New("peer not authorized")
	ErrPeerNotFound          = errors.New("peer not found")
	ErrPeerKeyMismatch       = errors.New("peer public key mismatch")
	ErrPeerScopeInsufficient = errors.New("peer scope insufficient")
	ErrPeerRateLimited       = errors.New("peer rate limited")
	ErrPeerRevoked           = errors.New("peer revoked")
	ErrPeerReplay            = errors.New("peer token replay")
	ErrPeerURLUnsafe         = errors.New("peer URL points to unsafe address")
	ErrInviteNotFound        = errors.New("invite not found")
	ErrInviteExpired         = errors.New("invite expired")
	ErrInviteAlreadyUsed     = errors.New("invite already used")
	ErrInviteInvalidFormat   = errors.New("invite invalid format")
	ErrServerIdentityMissing = errors.New("server identity not initialised")

	// ErrPairingRequestsDisabled: el admin local ha apagado el toggle
	// federation.accept_pairing_requests. Los endpoints publicos
	// devuelven 403 + mensaje generico ("not accepting"); el sender
	// ve un error claro para que sepa que tiene que probar otro
	// canal (codigo invite legacy, llamar al admin, etc.).
	ErrPairingRequestsDisabled = errors.New("pairing requests are disabled")

	// ErrPairingRequestQuotaExceeded: cap defensivo sobre el numero
	// de incoming pending. Defense-in-depth contra un atacante que
	// burla el rate-limit (e.g. usando muchos servidores remotos
	// distintos) y llena la tabla.
	ErrPairingRequestQuotaExceeded = errors.New("pairing request quota exceeded")

	// Plugin
	ErrPluginCrashed = errors.New("plugin crashed")
	ErrPluginTimeout = errors.New("plugin timeout")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %v", e.Fields)
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func NewValidationError(fields map[string]string) *ValidationError {
	return &ValidationError{Fields: fields}
}

// AppError es un error tipado que la API renderiza como JSON sin filtrar
// detalles internos. Lleva un code estable, el status HTTP, el mensaje
// para el cliente y datos opcionales (hint, details, retry-after).
//
// Uso: `return nil, domain.NewTranscodeBusy(active, max)`.
type AppError struct {
	Code       string
	HTTPStatus int
	Message    string
	Hint       string
	Details    map[string]any
	RetryAfter time.Duration

	kind  error // sentinel para errors.Is (nil en errores puntuales)
	cause error // causa interna; sólo para logs y tests, nunca al cliente
}

// Error devuelve el mensaje listo para mostrar; si hay causa interna, la
// añade al final para que aparezca en los logs.
func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.cause
}

// Is permite que `errors.Is(err, domain.ErrNotFound)` siga funcionando
// aunque el servicio ahora devuelva un *AppError.
func (e *AppError) Is(target error) bool {
	if e.kind == nil {
		return false
	}
	return errors.Is(e.kind, target)
}

// WithCause guarda el error original (de BD, FFmpeg, provider, etc.) para
// que aparezca en los logs sin filtrarlo al cliente.
func (e *AppError) WithCause(cause error) *AppError {
	e.cause = cause
	return e
}

// WithDetails añade detalles estructurados. Conviene que el mapa sea pequeño.
func (e *AppError) WithDetails(details map[string]any) *AppError {
	e.Details = details
	return e
}

func (e *AppError) WithHint(hint string) *AppError {
	e.Hint = hint
	return e
}

// --- Constructors ----------------------------------------------------------
// Cada uno fija sentinel + status HTTP + code, para que usarlos sea siempre
// una línea y el catálogo de errores se mantenga consistente.

func NewNotFound(resource string) *AppError {
	return &AppError{
		Code:       "NOT_FOUND",
		HTTPStatus: http.StatusNotFound,
		Message:    resource + " not found",
		kind:       ErrNotFound,
	}
}

func NewAlreadyExists(resource string) *AppError {
	return &AppError{
		Code:       "ALREADY_EXISTS",
		HTTPStatus: http.StatusConflict,
		Message:    resource + " already exists",
		kind:       ErrAlreadyExists,
	}
}

func NewConflict(message string) *AppError {
	return &AppError{
		Code:       "CONFLICT",
		HTTPStatus: http.StatusConflict,
		Message:    message,
		kind:       ErrConflict,
	}
}

func NewUnauthorized(message string) *AppError {
	if message == "" {
		message = "authentication required"
	}
	return &AppError{
		Code:       "UNAUTHORIZED",
		HTTPStatus: http.StatusUnauthorized,
		Message:    message,
		kind:       ErrUnauthorized,
	}
}

// NewInvalidCredentials usa un mensaje vago a propósito para no chivar si
// lo que falló fue el usuario o la contraseña.
func NewInvalidCredentials() *AppError {
	return &AppError{
		Code:       "INVALID_CREDENTIALS",
		HTTPStatus: http.StatusUnauthorized,
		Message:    "invalid username or password",
		kind:       ErrInvalidPassword,
	}
}

func NewTokenExpired() *AppError {
	return &AppError{
		Code:       "TOKEN_EXPIRED",
		HTTPStatus: http.StatusUnauthorized,
		Message:    "access token has expired",
		Hint:       "refresh the token or log in again",
		kind:       ErrTokenExpired,
	}
}

func NewForbidden(message string) *AppError {
	if message == "" {
		message = "insufficient permissions"
	}
	return &AppError{
		Code:       "FORBIDDEN",
		HTTPStatus: http.StatusForbidden,
		Message:    message,
		kind:       ErrForbidden,
	}
}

func NewAccountDisabled() *AppError {
	return &AppError{
		Code:       "ACCOUNT_DISABLED",
		HTTPStatus: http.StatusForbidden,
		Message:    "account is disabled",
		kind:       ErrAccountDisabled,
	}
}

// NewAccessExpired es distinto de AccountDisabled para que el frontend
// pueda decir "contacta con el admin para ampliar el acceso" en vez del
// genérico "cuenta desactivada".
func NewAccessExpired() *AppError {
	return &AppError{
		Code:       "ACCESS_EXPIRED",
		HTTPStatus: http.StatusForbidden,
		Message:    "temporary access window has expired",
		kind:       ErrAccessExpired,
	}
}

func NewValidation(fields map[string]string) *AppError {
	details := map[string]any{"fields": fields}
	return &AppError{
		Code:       "VALIDATION_ERROR",
		HTTPStatus: http.StatusBadRequest,
		Message:    "validation failed",
		Details:    details,
		kind:       ErrValidation,
	}
}

// NewTranscodeBusy: no hay slots de transcodificación libres. active/max
// van en Details para que el cliente pueda mostrarlos.
func NewTranscodeBusy(active, max int) *AppError {
	return &AppError{
		Code:       "STREAM_TRANSCODE_BUSY",
		HTTPStatus: http.StatusServiceUnavailable,
		Message:    "no transcode slots available",
		Hint:       "try again in a few seconds",
		Details:    map[string]any{"active": active, "max": max},
		RetryAfter: 5 * time.Second,
		kind:       ErrTranscodeBusy,
	}
}

// NewTranscodePending: la transcodificación aún no ha generado el manifiesto.
func NewTranscodePending() *AppError {
	return &AppError{
		Code:       "STREAM_TRANSCODE_PENDING",
		HTTPStatus: http.StatusServiceUnavailable,
		Message:    "transcoding is starting, try again shortly",
		RetryAfter: 2 * time.Second,
	}
}

func NewUnsupportedCodec(codec string) *AppError {
	return &AppError{
		Code:       "STREAM_UNSUPPORTED_CODEC",
		HTTPStatus: http.StatusUnsupportedMediaType,
		Message:    "codec not supported for playback",
		Details:    map[string]any{"codec": codec},
		kind:       ErrUnsupportedCodec,
	}
}

func NewFileNotAvailable(itemID string) *AppError {
	return &AppError{
		Code:       "FILE_NOT_FOUND",
		HTTPStatus: http.StatusNotFound,
		Message:    "media file is not available",
		Details:    map[string]any{"item_id": itemID},
		kind:       ErrNotFound,
	}
}

// NewInvalidInput se usa cuando lo que mandó el cliente no es válido (JSON
// malformado, parámetro inválido, etc.).
func NewInvalidInput(code, message string) *AppError {
	if code == "" {
		code = "INVALID_INPUT"
	}
	return &AppError{
		Code:       code,
		HTTPStatus: http.StatusBadRequest,
		Message:    message,
	}
}

// NewInternal es el último recurso. Conviene usar un constructor más
// específico si existe.
func NewInternal(cause error) *AppError {
	return &AppError{
		Code:       "INTERNAL_ERROR",
		HTTPStatus: http.StatusInternalServerError,
		Message:    "internal server error",
		cause:      cause,
	}
}
