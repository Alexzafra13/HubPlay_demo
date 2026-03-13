package domain

import (
	"errors"
	"fmt"
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
	ErrPeerOffline      = errors.New("peer offline")
	ErrPeerUnauthorized = errors.New("peer not authorized")

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
