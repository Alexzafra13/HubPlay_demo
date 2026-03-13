package domain

import (
	"errors"
	"fmt"
	"testing"
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
