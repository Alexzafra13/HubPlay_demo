package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns an http.Handler that serves the Prometheus exposition
// format. It is safe to mount at a public path: the registry contains no
// secrets and the collectors are read-only.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		// EnableOpenMetrics keeps compatibility with scrapers that negotiate
		// the newer format while falling back to text/plain for older ones.
		EnableOpenMetrics: true,
	})
}

// MetricsMiddleware observes every request with counters + a duration
// histogram. It is safe to chain before or after logging, but should come
// after chi's Router.Route definitions so RoutePattern is populated for
// matched requests.
//
// The route label uses chi's RoutePattern ("/libraries/{id}") rather than the
// raw URL path to keep cardinality bounded; unmatched requests (404s that
// never hit a handler) fall back to a stable sentinel "_other".
func (m *Metrics) MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := routeLabel(r)
		status := strconv.Itoa(statusOr200(ww.Status()))
		m.HTTPRequests.WithLabelValues(r.Method, route, status).Inc()
		m.HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// routeLabel extracts the chi route pattern for the matched request. Requests
// that did not match any route (chi RouteContext empty) are bucketed under
// "_other" so scraping shows them without creating per-URL time series.
func routeLabel(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "_other"
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return "_other"
}

// statusOr200 normalises the status code recorded by chi's wrapped
// ResponseWriter: if the handler never called WriteHeader explicitly, chi
// reports 0 and net/http treats it as 200.
func statusOr200(code int) int {
	if code == 0 {
		return http.StatusOK
	}
	return code
}
