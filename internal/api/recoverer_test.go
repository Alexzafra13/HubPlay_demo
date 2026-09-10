package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// TestRecoverer_PanicBecomes500WithLogAndRequestID (M20): un panic en un
// handler responde 500 JSON, se loguea vía slog con request_id y stack, y
// no tumba el proceso.
func TestRecoverer_PanicBecomes500WithLogAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	h := middleware.RequestID(Recoverer(logger, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explode", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	line := buf.String()
	for _, want := range []string{`"panic recovered"`, `"panic":"boom"`, `"path":"/api/v1/explode"`, `"request_id"`, `"stack"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
}

// TestRecoverer_ErrAbortHandlerPropagates: http.ErrAbortHandler es la
// señal idiomática de net/http para abortar silenciosamente; no se
// convierte en 500 ni se loguea como panic.
func TestRecoverer_ErrAbortHandlerPropagates(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := Recoverer(logger, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("ErrAbortHandler should propagate, recovered %v", r)
		}
		if strings.Contains(buf.String(), "panic recovered") {
			t.Error("ErrAbortHandler must not be logged as a panic")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

// TestRequestLogger_LogIPs (M18/M19): con log_ips=false no se registra la
// IP; con true se registra la IP del cliente resuelta (no RemoteAddr crudo
// cuando hay middleware de client-IP delante).
func TestRequestLogger_LogIPs(t *testing.T) {
	run := func(logIPs bool) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		h := middleware.ClientIPFromXFF("10.0.0.0/8")(RequestLogger(logger, logIPs)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.RemoteAddr = "10.1.2.3:4444" // el proxy de confianza
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		h.ServeHTTP(httptest.NewRecorder(), req)
		return buf.String()
	}

	off := run(false)
	if strings.Contains(off, `"ip"`) {
		t.Errorf("log_ips=false must omit the ip field: %s", off)
	}
	on := run(true)
	if !strings.Contains(on, `"ip":"203.0.113.9"`) {
		t.Errorf("log_ips=true must log the resolved client IP, got: %s", on)
	}
	if strings.Contains(on, "10.1.2.3") {
		t.Errorf("proxy address must not be logged as client ip: %s", on)
	}
}
