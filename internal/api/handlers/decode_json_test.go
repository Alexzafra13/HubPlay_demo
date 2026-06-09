package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hubplay/internal/domain"
)

func TestDecodeJSON_OK(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"hubplay"}`))
	w := httptest.NewRecorder()

	var v struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(w, r, &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "hubplay" {
		t.Fatalf("got %q, want hubplay", v.Name)
	}
}

func TestDecodeJSON_Malformed(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()

	var v map[string]any
	err := DecodeJSON(w, r, &v)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("malformed body: got %v, want 400 AppError", err)
	}
}

func TestDecodeJSON_TooLarge(t *testing.T) {
	// A body comfortably over MaxJSONBody must be rejected with 413 and
	// not buffered whole — the memory-DoS guard.
	big := `{"v":"` + strings.Repeat("a", MaxJSONBody+1024) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
	w := httptest.NewRecorder()

	var v map[string]any
	err := DecodeJSON(w, r, &v)
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: got %v, want 413 AppError", err)
	}
}
