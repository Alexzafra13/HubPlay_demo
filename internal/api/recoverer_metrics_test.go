package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"hubplay/internal/observability"
)

// TestRecoverer_CountsPanicInMetrics (M20): cada panic recuperado
// incrementa hubplay_http_errors_total{code="panic"}.
func TestRecoverer_CountsPanicInMetrics(t *testing.T) {
	m, err := observability.NewMetrics("test")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Recoverer(logger, m)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	for i := 0; i < 3; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	}
	if got := testutil.ToFloat64(m.HTTPErrors.WithLabelValues("panic")); got != 3 {
		t.Errorf("panic counter = %v, want 3", got)
	}
}
