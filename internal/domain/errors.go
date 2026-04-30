package domain

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Sentinel errors (for errors.Is)
var (
	// Resource errors
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("conflict")

	// Auth errors
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrInvalidToken    = errors.New("invalid token")
	ErrTokenExpired    = errors.New("token expired")
	ErrInvalidPassword = errors.New("invalid password")
	ErrAccountDisabled = errors.New("account disabled")

	// Validation
	ErrValidation = errors.New("validation error")

	// Streaming
	ErrTranscodeBusy    = errors.New("transcode slots full")
	ErrUnsupportedCodec = errors.New("unsupported codec")

	// Federation
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

	// Plugin
	ErrPluginCrashed = errors.New("plugin crashed")
	ErrPluginTimeout = errors.New("plugin timeout")
)

// ValidationError contains details about which fields failed validation.
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

// AppError is a rich, typed error meant to be rendered as an HTTP response.
//
// It carries everything the API layer needs to produce a consistent JSON error
// without leaking internal messages: a stable machine-readable Code, the HTTP
// status, a user-facing Message, and optional Hint/Details/RetryAfter. The
// unexported kind links the AppError to a sentinel so existing errors.Is
// checks keep working (backward compatibility). The unexported cause carries
// the wrapped internal error for logs and tests, never for the client.
//
// Usage:
//
//	return nil, domain.NewTranscodeBusy(active, max)
//
// At the API layer, handleServiceError renders *AppError directly; any other
// error falls back to the sentinel switch.
type AppError struct {
	Code       string
	HTTPStatus int
	Message    string
	Hint       string
	Details    map[string]any
	RetryAfter time.Duration

	kind  error // sentinel for errors.Is (may be nil for ad-hoc errors)
	cause error // wrapped internal cause for logs/tests
}

// Error implements the error interface. It returns the user-facing message
// (suitable for logs); the cause, if any, is appended for operator context.
func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the internal cause so errors.Is/As can traverse the chain.
func (e *AppError) Unwrap() error {
	return e.cause
}

// Is reports whether target matches the AppError's sentinel kind. This keeps
// legacy checks like errors.Is(err, domain.ErrNotFound) working after a
// service switches to returning *AppError.
func (e *AppError) Is(target error) bool {
	if e.kind == nil {
		return false
	}
	return errors.Is(e.kind, target)
}

// WithCause attaches an internal cause. Callers pass the upstream error
// (DB, FFmpeg, provider, ...) so it shows up in logs without leaking to the
// client. Returns the same *AppError for chaining.
func (e *AppError) WithCause(cause error) *AppError {
	e.cause = cause
	return e
}

// WithDetails attaches structured details. Prefer small, bounded maps.
func (e *AppError) WithDetails(details map[string]any) *AppError {
	e.Details = details
	return e
}

// WithHint attaches an actionable hint for the client.
func (e *AppError) WithHint(hint string) *AppError {
	e.Hint = hint
	return e
}

// --- Constructors ----------------------------------------------------------
//
// Each constructor binds a sentinel (kind) + HTTP status + stable code, so
// adopting them at a call site is a one-liner and the API error catalog stays
// consistent across handlers.

// NewNotFound returns a 404 AppError for a missing resource.
func NewNotFound(resource string) *AppError {
	return &AppError{
		Code:       "NOT_FOUND",
		HTTPStatus: http.StatusNotFound,
		Message:    resource + " not found",
		kind:       ErrNotFound,
	}
}

// NewAlreadyExists returns a 409 AppError when a resource already exists.
func NewAlreadyExists(resource string) *AppError {
	return &AppError{
		Code:       "ALREADY_EXISTS",
		HTTPStatus: http.StatusConflict,
		Message:    resource + " already exists",
		kind:       ErrAlreadyExists,
	}
}

// NewConflict returns a 409 AppError for a state conflict.
func NewConflict(message string) *AppError {
	return &AppError{
		Code:       "CONFLICT",
		HTTPStatus: http.StatusConflict,
		Message:    message,
		kind:       ErrConflict,
	}
}

// NewUnauthorized returns a 401 AppError for an unauthenticated request.
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

// NewInvalidCredentials returns a 401 AppError for a failed login.
// Message is deliberately vague to avoid leaking which field was wrong.
func NewInvalidCredentials() *AppError {
	return &AppError{
		Code:       "INVALID_CREDENTIALS",
		HTTPStatus: http.StatusUnauthorized,
		Message:    "invalid username or password",
		kind:       ErrInvalidPassword,
	}
}

// NewTokenExpired returns a 401 AppError signalling the access token expired.
func NewTokenExpired() *AppError {
	return &AppError{
		Code:       "TOKEN_EXPIRED",
		HTTPStatus: http.StatusUnauthorized,
		Message:    "access token has expired",
		Hint:       "refresh the token or log in again",
		kind:       ErrTokenExpired,
	}
}

// NewForbidden returns a 403 AppError for a rejected authorization.
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

// NewAccountDisabled returns a 403 AppError for a disabled account.
func NewAccountDisabled() *AppError {
	return &AppError{
		Code:       "ACCOUNT_DISABLED",
		HTTPStatus: http.StatusForbidden,
		Message:    "account is disabled",
		kind:       ErrAccountDisabled,
	}
}

// NewValidation returns a 400 AppError from per-field validation errors.
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

// NewTranscodeBusy returns a 503 AppError when no transcode slots are free.
// active and max are reported as details so clients can surface them.
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

// NewTranscodePending returns a 503 AppError when the manifest is not ready yet.
func NewTranscodePending() *AppError {
	return &AppError{
		Code:       "STREAM_TRANSCODE_PENDING",
		HTTPStatus: http.StatusServiceUnavailable,
		Message:    "transcoding is starting, try again shortly",
		RetryAfter: 2 * time.Second,
	}
}

// NewUnsupportedCodec returns a 415 AppError for a codec the server cannot play.
func NewUnsupportedCodec(codec string) *AppError {
	return &AppError{
		Code:       "STREAM_UNSUPPORTED_CODEC",
		HTTPStatus: http.StatusUnsupportedMediaType,
		Message:    "codec not supported for playback",
		Details:    map[string]any{"codec": codec},
		kind:       ErrUnsupportedCodec,
	}
}

// NewFileNotAvailable returns a 404 AppError when the media file is missing on disk.
func NewFileNotAvailable(itemID string) *AppError {
	return &AppError{
		Code:       "FILE_NOT_FOUND",
		HTTPStatus: http.StatusNotFound,
		Message:    "media file is not available",
		Details:    map[string]any{"item_id": itemID},
		kind:       ErrNotFound,
	}
}

// NewInvalidInput returns a 400 AppError for a malformed client input
// (bad JSON body, bad path parameter, bad filename, ...).
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

// NewInternal returns a 500 AppError. The cause is stored for logs but never
// rendered to the client. Use this at the last line of defense — prefer a
// more specific constructor when you can.
func NewInternal(cause error) *AppError {
	return &AppError{
		Code:       "INTERNAL_ERROR",
		HTTPStatus: http.StatusInternalServerError,
		Message:    "internal server error",
		cause:      cause,
	}
}
