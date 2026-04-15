package domain

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestValidationError_Unwrap(t *testing.T) {
	err := NewValidationError(map[string]string{
		"username": "too short",
		"password": "required",
	})

	if !errors.Is(err, ErrValidation) {
		t.Error("ValidationError should unwrap to ErrValidation")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatal("should be able to extract ValidationError with errors.As")
	}

	if valErr.Fields["username"] != "too short" {
		t.Errorf("expected 'too short', got %q", valErr.Fields["username"])
	}
	if valErr.Fields["password"] != "required" {
		t.Errorf("expected 'required', got %q", valErr.Fields["password"])
	}
}

func TestValidationError_ErrorMessage(t *testing.T) {
	err := NewValidationError(map[string]string{"field": "invalid"})
	msg := err.Error()
	if msg == "" {
		t.Error("error message should not be empty")
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	errs := []error{
		ErrNotFound, ErrAlreadyExists, ErrConflict,
		ErrUnauthorized, ErrForbidden, ErrInvalidToken,
		ErrTokenExpired, ErrInvalidPassword, ErrAccountDisabled,
		ErrValidation, ErrTranscodeBusy, ErrUnsupportedCodec,
	}

	for i, a := range errs {
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors should be distinct: %v == %v", a, b)
			}
		}
	}
}

func TestWrappedError_PreservesSentinel(t *testing.T) {
	wrapped := fmt.Errorf("item 123: %w", ErrNotFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("wrapped error should still match ErrNotFound")
	}

	doubleWrapped := fmt.Errorf("scanning library: %w", wrapped)
	if !errors.Is(doubleWrapped, ErrNotFound) {
		t.Error("double-wrapped error should still match ErrNotFound")
	}
}

// ----------------------- AppError tests -----------------------

func TestAppError_MatchesSentinelViaIs(t *testing.T) {
	// Each constructor is bound to a sentinel so legacy errors.Is checks
	// keep working. This guards the behavioural contract (a single broken
	// kind mapping would ripple through every handler).
	cases := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"NotFound", NewNotFound("item"), ErrNotFound},
		{"FileNotAvailable", NewFileNotAvailable("abc"), ErrNotFound},
		{"AlreadyExists", NewAlreadyExists("user"), ErrAlreadyExists},
		{"Conflict", NewConflict("state"), ErrConflict},
		{"Unauthorized", NewUnauthorized(""), ErrUnauthorized},
		{"InvalidCredentials", NewInvalidCredentials(), ErrInvalidPassword},
		{"TokenExpired", NewTokenExpired(), ErrTokenExpired},
		{"Forbidden", NewForbidden(""), ErrForbidden},
		{"AccountDisabled", NewAccountDisabled(), ErrAccountDisabled},
		{"Validation", NewValidation(map[string]string{"a": "b"}), ErrValidation},
		{"TranscodeBusy", NewTranscodeBusy(2, 2), ErrTranscodeBusy},
		{"UnsupportedCodec", NewUnsupportedCodec("hevc"), ErrUnsupportedCodec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.sentinel) {
				t.Errorf("errors.Is should match sentinel %v", tc.sentinel)
			}
		})
	}
}

func TestAppError_HTTPStatusAndCode(t *testing.T) {
	cases := []struct {
		name string
		err  *AppError
		want int
		code string
	}{
		{"NotFound", NewNotFound("item"), http.StatusNotFound, "NOT_FOUND"},
		{"AlreadyExists", NewAlreadyExists("user"), http.StatusConflict, "ALREADY_EXISTS"},
		{"Conflict", NewConflict("state"), http.StatusConflict, "CONFLICT"},
		{"Unauthorized", NewUnauthorized(""), http.StatusUnauthorized, "UNAUTHORIZED"},
		{"InvalidCredentials", NewInvalidCredentials(), http.StatusUnauthorized, "INVALID_CREDENTIALS"},
		{"TokenExpired", NewTokenExpired(), http.StatusUnauthorized, "TOKEN_EXPIRED"},
		{"Forbidden", NewForbidden(""), http.StatusForbidden, "FORBIDDEN"},
		{"AccountDisabled", NewAccountDisabled(), http.StatusForbidden, "ACCOUNT_DISABLED"},
		{"Validation", NewValidation(nil), http.StatusBadRequest, "VALIDATION_ERROR"},
		{"TranscodeBusy", NewTranscodeBusy(2, 2), http.StatusServiceUnavailable, "STREAM_TRANSCODE_BUSY"},
		{"TranscodePending", NewTranscodePending(), http.StatusServiceUnavailable, "STREAM_TRANSCODE_PENDING"},
		{"UnsupportedCodec", NewUnsupportedCodec("hevc"), http.StatusUnsupportedMediaType, "STREAM_UNSUPPORTED_CODEC"},
		{"FileNotAvailable", NewFileNotAvailable("abc"), http.StatusNotFound, "FILE_NOT_FOUND"},
		{"InvalidInput", NewInvalidInput("", "bad"), http.StatusBadRequest, "INVALID_INPUT"},
		{"Internal", NewInternal(errors.New("boom")), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.HTTPStatus != tc.want {
				t.Errorf("status: got %d, want %d", tc.err.HTTPStatus, tc.want)
			}
			if tc.err.Code != tc.code {
				t.Errorf("code: got %q, want %q", tc.err.Code, tc.code)
			}
		})
	}
}

func TestAppError_WithCause_UnwrapsToCause(t *testing.T) {
	cause := errors.New("upstream boom")
	appErr := NewInternal(cause)

	if !errors.Is(appErr, cause) {
		t.Error("errors.Is should find the wrapped cause via Unwrap")
	}
	if errors.Unwrap(appErr) != cause {
		t.Error("Unwrap should return the stored cause")
	}
}

func TestAppError_WithCause_DoesNotLeakInMessage(t *testing.T) {
	// The user-facing Message must not include the cause (that's logs-only).
	// Error() may append the cause for operators; Message must stay clean.
	cause := errors.New("SECRET_DB_DSN=foo")
	appErr := NewInternal(cause)

	if appErr.Message == "" || containsSubstring(appErr.Message, "SECRET_DB_DSN") {
		t.Errorf("Message must not contain cause details: %q", appErr.Message)
	}
}

func TestAppError_TranscodeBusy_CarriesRetryAfterAndDetails(t *testing.T) {
	appErr := NewTranscodeBusy(3, 4)

	if appErr.RetryAfter <= 0 {
		t.Error("TranscodeBusy should set a positive RetryAfter")
	}
	if appErr.RetryAfter > time.Minute {
		t.Errorf("RetryAfter too large: %v", appErr.RetryAfter)
	}
	if got := appErr.Details["active"]; got != 3 {
		t.Errorf("details.active: got %v, want 3", got)
	}
	if got := appErr.Details["max"]; got != 4 {
		t.Errorf("details.max: got %v, want 4", got)
	}
}

func TestAppError_Validation_CarriesFields(t *testing.T) {
	appErr := NewValidation(map[string]string{"name": "required"})
	details, ok := appErr.Details["fields"].(map[string]string)
	if !ok {
		t.Fatalf("details.fields wrong type: %T", appErr.Details["fields"])
	}
	if details["name"] != "required" {
		t.Errorf("lost field payload: %v", details)
	}
}

func TestAppError_Builders(t *testing.T) {
	appErr := NewNotFound("movie").
		WithCause(errors.New("db closed")).
		WithHint("refresh cache").
		WithDetails(map[string]any{"id": "42"})

	if appErr.Hint != "refresh cache" {
		t.Errorf("hint not set: %q", appErr.Hint)
	}
	if appErr.Details["id"] != "42" {
		t.Errorf("details not set: %v", appErr.Details)
	}
	if errors.Unwrap(appErr) == nil {
		t.Error("cause not set")
	}
}

// containsSubstring is a local helper to avoid pulling in strings for a
// single assertion in the leak test.
func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
