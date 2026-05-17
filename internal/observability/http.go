package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler: sirve formato Prometheus. Safe en path público — el registry no
// tiene secretos y los collectors son read-only.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		// Mantiene compat con scrapers que negocian formato nuevo, con fallback
		// a text/plain para los viejos.
		EnableOpenMetrics: true,
	})
}

// MetricsMiddleware: cuenta requests + histogram de duración. Debe ir DESPUÉS
// de las Router.Route definitions para que RoutePattern esté poblado.
//
// La label `route` usa el RoutePattern de chi ("/libraries/{id}") en vez del
// URL crudo para acotar cardinality; requests sin match (404 directos) caen al
// sentinel "_other".
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

// routeLabel: RoutePattern de chi del request matched. Sin match → "_other"
// (evita serie de tiempo per-URL).
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

// statusOr200: si el handler no llamó WriteHeader, chi devuelve 0 y net/http
// lo trata como 200.
func statusOr200(code int) int {
	if code == 0 {
		return http.StatusOK
	}
	return code
}
