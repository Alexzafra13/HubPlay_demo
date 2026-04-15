package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics_RegistersCollectors(t *testing.T) {
	m, err := NewMetrics("test-1.2.3")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	if m.Registry == nil {
		t.Fatal("registry must not be nil")
	}
	// build_info should already be populated so scrapes work from t=0.
	if got := testutil.CollectAndCount(m.BuildInfo); got != 1 {
		t.Errorf("build_info samples: got %d, want 1", got)
	}
}

func TestNewMetrics_IsolatedRegistries(t *testing.T) {
	// Each call owns its own registry, so two parallel test sets never
	// collide on duplicate registration.
	a, err := NewMetrics("a")
	if err != nil {
		t.Fatalf("NewMetrics a: %v", err)
	}
	b, err := NewMetrics("b")
	if err != nil {
		t.Fatalf("NewMetrics b: %v", err)
	}
	if a.Registry == b.Registry {
		t.Error("registries must be independent instances")
	}
}

func TestHandler_ExposesPrometheusText(t *testing.T) {
	m, err := NewMetrics("v0")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	m.HTTPRequests.WithLabelValues("GET", "/health", "200").Inc()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	text := string(body)
	if !strings.Contains(text, "hubplay_build_info") {
		t.Errorf("exposition missing build_info:\n%s", text)
	}
	if !strings.Contains(text, `hubplay_http_requests_total{method="GET",route="/health",status="200"} 1`) {
		t.Errorf("exposition missing HTTP counter sample:\n%s", text)
	}
}

func TestMetricsMiddleware_UsesRoutePattern(t *testing.T) {
	// The critical guarantee: the route label is the chi pattern, not the
	// raw URL. Otherwise /libraries/{id} yields one series per library id
	// and swamps the TSDB.
	m, err := NewMetrics("v0")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	r := chi.NewRouter()
	r.Use(m.MetricsMiddleware)
	r.Get("/libraries/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, id := range []string{"a", "b", "c"} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/libraries/"+id, nil))
	}

	got := testutil.ToFloat64(m.HTTPRequests.WithLabelValues("GET", "/libraries/{id}", "200"))
	if got != 3 {
		t.Errorf("counter {route=/libraries/{id}}: got %v, want 3", got)
	}
	// The raw path label must not exist.
	raw := testutil.ToFloat64(m.HTTPRequests.WithLabelValues("GET", "/libraries/a", "200"))
	if raw != 0 {
		t.Errorf("raw path label leaked into metrics: got %v", raw)
	}
}

// NOTE: chi v5 does not run router-level middlewares when a request fails to
// match any route, so "unmatched → _other" cannot be exercised through a
// router-mounted middleware. The fallback in routeLabel still covers edge
// cases where RouteContext exists but RoutePattern is empty (e.g. requests
// served by handlers registered outside the chi tree), which is difficult to
// reproduce deterministically in a unit test. Integration tests cover the
// production stack; the unit test surface stops at matched routes.

func TestStreamSink_Counters(t *testing.T) {
	m, err := NewMetrics("v0")
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	sink := NewStreamSink(m)

	sink.TranscodeStarted()
	sink.TranscodeStarted()
	sink.TranscodeBusy()
	sink.TranscodeFailed()
	sink.SetActiveSessions(3)

	if got := testutil.ToFloat64(m.StreamTranscodeStarts.WithLabelValues("started")); got != 2 {
		t.Errorf("started: got %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.StreamTranscodeStarts.WithLabelValues("busy")); got != 1 {
		t.Errorf("busy: got %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.StreamTranscodeStarts.WithLabelValues("failed")); got != 1 {
		t.Errorf("failed: got %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.StreamActiveSessions); got != 3 {
		t.Errorf("active: got %v, want 3", got)
	}
}

func TestNewStreamSink_NilMetricsReturnsNil(t *testing.T) {
	if NewStreamSink(nil) != nil {
		t.Error("NewStreamSink(nil) should return nil for caller to short-circuit")
	}
}
