package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/domain"
)

// decodeErrorResponse extracts the standard error envelope written by
// handleServiceError / respondAppError, so each assertion stays focused on
// one field at a time.
func decodeErrorResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == nil {
		t.Fatalf("response has no error object: %s", rr.Body.String())
	}
	return body.Error
}

// newRequestWithID builds a request whose context already carries the chi
// request_id, mimicking what middleware.RequestID does in the real stack.
func newRequestWithID(id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil)
	ctx := context.WithValue(r.Context(), middleware.RequestIDKey, id)
	return r.WithContext(ctx)
}

func TestHandleServiceError_RendersAppErrorDirectly(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newRequestWithID("req-123")

	handleServiceError(rr, r, domain.NewTranscodeBusy(2, 2))

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got == "" {
		t.Error("Retry-After header should be set for TranscodeBusy")
	}

	body := decodeErrorResponse(t, rr)
	if body["code"] != "STREAM_TRANSCODE_BUSY" {
		t.Errorf("code: got %v", body["code"])
	}
	if body["request_id"] != "req-123" {
		t.Errorf("request_id missing: got %v", body["request_id"])
	}
	details, _ := body["details"].(map[string]any)
	if details == nil || details["max"] == nil {
		t.Errorf("details missing: %v", body)
	}
}

func TestHandleServiceError_RendersWrappedAppError(t *testing.T) {
	// A service may wrap an AppError with fmt.Errorf; errors.As must still
	// find it. Otherwise we'd lose the typed response on every `%w` in the
	// call chain.
	rr := httptest.NewRecorder()
	r := newRequestWithID("req-wrap")

	wrapped := fmt.Errorf("transcode: %w", domain.NewTranscodeBusy(1, 1))
	handleServiceError(rr, r, wrapped)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rr.Code)
	}
	body := decodeErrorResponse(t, rr)
	if body["code"] != "STREAM_TRANSCODE_BUSY" {
		t.Errorf("wrapped AppError not unwrapped: %v", body["code"])
	}
}

func TestHandleServiceError_SentinelFallback(t *testing.T) {
	// Legacy services still returning bare sentinels must keep working until
	// every call site migrates. This locks down the fallback switch so a
	// refactor cannot silently break them.
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantKey  string
	}{
		{"NotFound", domain.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"AlreadyExists", domain.ErrAlreadyExists, http.StatusConflict, "ALREADY_EXISTS"},
		{"InvalidPassword", domain.ErrInvalidPassword, http.StatusUnauthorized, "INVALID_CREDENTIALS"},
		{"Unauthorized", domain.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"TokenExpired", domain.ErrTokenExpired, http.StatusUnauthorized, "TOKEN_EXPIRED"},
		{"Forbidden", domain.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{"AccountDisabled", domain.ErrAccountDisabled, http.StatusForbidden, "ACCOUNT_DISABLED"},
		{"Conflict", domain.ErrConflict, http.StatusConflict, "CONFLICT"},
		{"Validation", domain.ErrValidation, http.StatusBadRequest, "VALIDATION_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r := newRequestWithID("req-" + tc.name)

			handleServiceError(rr, r, tc.err)

			if rr.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", rr.Code, tc.wantCode)
			}
			body := decodeErrorResponse(t, rr)
			if body["code"] != tc.wantKey {
				t.Errorf("code: got %v, want %q", body["code"], tc.wantKey)
			}
		})
	}
}

func TestHandleServiceError_ValidationErrorDetails(t *testing.T) {
	rr := httptest.NewRecorder()
	r := newRequestWithID("req-val")

	valErr := domain.NewValidationError(map[string]string{
		"username": "too short",
		"password": "required",
	})
	handleServiceError(rr, r, valErr)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", rr.Code)
	}
	body := decodeErrorResponse(t, rr)
	details, ok := body["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing or wrong type: %v", body)
	}
	fields, ok := details["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields missing: %v", details)
	}
	if fields["username"] != "too short" {
		t.Errorf("fields[username]: got %v", fields["username"])
	}
}

func TestHandleServiceError_InternalErrorDoesNotLeakCause(t *testing.T) {
	// Arbitrary errors from the service layer must never reach the client
	// verbatim — they could contain DB DSNs, internal paths, etc.
	rr := httptest.NewRecorder()
	r := newRequestWithID("req-int")

	secret := errors.New("SECRET_DB_DSN=postgres://user:pass@host/db")
	handleServiceError(rr, r, secret)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "SECRET_DB_DSN") {
		t.Errorf("500 response leaked cause: %s", rr.Body.String())
	}
	body := decodeErrorResponse(t, rr)
	if body["code"] != "INTERNAL_ERROR" {
		t.Errorf("code: got %v", body["code"])
	}
	if body["request_id"] != "req-int" {
		t.Errorf("request_id missing for 500: %v", body["request_id"])
	}
}

func TestHandleServiceError_OmitsRequestIDWhenAbsent(t *testing.T) {
	// If the upstream middleware is not installed, we should still produce a
	// valid error body rather than an empty request_id slot. The JSON tag is
	// `omitempty`, so the field must simply not appear.
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil) // no request_id in ctx

	handleServiceError(rr, r, domain.NewNotFound("thing"))

	body := decodeErrorResponse(t, rr)
	if _, present := body["request_id"]; present {
		t.Errorf("request_id should be omitted when empty: %v", body)
	}
}

func TestSetErrorRecorder_InvokedOnEveryAppError(t *testing.T) {
	// The recorder hook is the single seam through which observability sees
	// API errors. If respondAppError ever forgets to call it we lose the
	// error-rate metric without any build failure. Lock that contract here.
	var gotCodes []string
	SetErrorRecorder(func(code string) { gotCodes = append(gotCodes, code) })
	// Restore the no-op recorder for any later test in the package.
	t.Cleanup(func() { SetErrorRecorder(nil) })

	rr := httptest.NewRecorder()
	r := newRequestWithID("req-metric")

	handleServiceError(rr, r, domain.NewTranscodeBusy(1, 1))
	handleServiceError(rr, r, domain.ErrNotFound)        // via sentinel fallback
	handleServiceError(rr, r, errors.New("unexpected")) // 500 → INTERNAL_ERROR

	want := []string{"STREAM_TRANSCODE_BUSY", "NOT_FOUND", "INTERNAL_ERROR"}
	if len(gotCodes) != len(want) {
		t.Fatalf("got %d codes, want %d: %v", len(gotCodes), len(want), gotCodes)
	}
	for i, c := range want {
		if gotCodes[i] != c {
			t.Errorf("codes[%d]: got %q, want %q", i, gotCodes[i], c)
		}
	}
}

func TestSetErrorRecorder_NilRestoresNoop(t *testing.T) {
	// SetErrorRecorder(nil) must not crash subsequent responses; it should
	// fall back to the no-op so partial deregistration is safe.
	SetErrorRecorder(func(string) { t.Fatal("should have been replaced by noop") })
	SetErrorRecorder(nil)

	rr := httptest.NewRecorder()
	r := newRequestWithID("req-nil")
	handleServiceError(rr, r, domain.NewNotFound("thing"))
}

func TestRespondError_WritesAppErrorEnvelope(t *testing.T) {
	// respondError is the ad-hoc helper used by handlers for input-shape
	// errors; it must produce the same envelope as the AppError path so
	// clients can parse a single response shape.
	rr := httptest.NewRecorder()
	r := newRequestWithID("req-resp")

	respondError(rr, r, http.StatusBadRequest, "INVALID_JSON", "bad body")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", rr.Code)
	}
	body := decodeErrorResponse(t, rr)
	if body["code"] != "INVALID_JSON" {
		t.Errorf("code: got %v", body["code"])
	}
	if body["message"] != "bad body" {
		t.Errorf("message: got %v", body["message"])
	}
	if body["request_id"] != "req-resp" {
		t.Errorf("request_id missing: %v", body["request_id"])
	}
}
