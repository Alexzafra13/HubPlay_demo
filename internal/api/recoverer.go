package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"

	"hubplay/internal/api/handlers"
	"hubplay/internal/observability"
)

// Recoverer recupera panics de los handlers y responde 500 (JSON de error
// estándar con request_id) en vez de tumbar el proceso.
//
// Sustituye a chi's middleware.Recoverer (M20): aquel escribía el stack a
// stderr fuera del logger estructurado (invisible en el ring-buffer del
// panel admin y sin request_id) y no dejaba huella en métricas. Aquí el
// panic va a slog a nivel Error con request_id, método, ruta y stack, y se
// cuenta en hubplay_http_errors_total{code="panic"} (metrics puede ser nil
// en tests minimalistas).
//
// http.ErrAbortHandler se re-lanza tal cual: es el mecanismo idiomático de
// net/http para abortar una respuesta sin log (p.ej. cliente desconectado
// a mitad de un stream) y el servidor lo trata de forma silenciosa.
func Recoverer(logger *slog.Logger, metrics *observability.Metrics) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				logger.Error("panic recovered",
					"panic", fmt.Sprint(rec),
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", middleware.GetReqID(r.Context()),
					"stack", string(debug.Stack()),
				)
				if metrics != nil {
					metrics.HTTPErrors.WithLabelValues("panic").Inc()
				}
				handlers.RespondError(w, r, http.StatusInternalServerError,
					"INTERNAL_ERROR", "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
