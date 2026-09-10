package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/api/handlers"
)

// RequestLogger logs each HTTP request with structured fields. It relies on
// chi's middleware.RequestID being installed upstream so each log entry
// carries a request_id that can be correlated with error responses (which
// embed the same id in the JSON payload) and application logs.
//
// La IP registrada es la del CLIENTE resuelta por el middleware de
// trusted-proxy (handlers.ClientIP), no r.RemoteAddr crudo — detrás de un
// reverse proxy cada línea llevaría la IP del proxy (M18). logIPs=false
// (`logging.log_ips`) omite el campo por completo (privacidad/GDPR, M19).
func RequestLogger(logger *slog.Logger, logIPs bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if logIPs {
				attrs = append(attrs, "ip", handlers.ClientIP(r))
			}
			attrs = append(attrs, "request_id", middleware.GetReqID(r.Context()))
			logger.Info("request", attrs...)
		})
	}
}
