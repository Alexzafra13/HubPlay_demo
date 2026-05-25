package handlers

import (
	"net"
	"net/http"
	"strings"

	"hubplay/internal/clock"
	"hubplay/internal/federation"
)

// IPRateLimitMiddleware aplica un token-bucket por IP cliente al
// endpoint envuelto. Pensado para endpoints PUBLICOS sin auth donde
// un atacante en internet puede martillear (e.g. el inbox de pairing
// requests recibe POSTs de servidores remotos sin auth previa).
//
// Reutiliza federation.RateLimiter usando la IP como bucket key -
//     RealIP si quiere honrar X-Forwarded-For).
func IPRateLimitMiddleware(limiter *federation.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !limiter.Allow(ip) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMITED","message":"too many requests, retry in a minute"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extrae la IP del request. Honra X-Forwarded-For si esta
// presente (asume primer hop trusted - es responsabilidad del
// operador no exponer el server directo a internet sin proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Primera entrada = cliente original.
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewPairingRequestRateLimiter es un constructor de conveniencia con
// limites razonables para el endpoint publico de pairing requests:
// 5 requests/min por IP, burst 3. Suficiente para flow legitimo
// (admin pulsa "enviar" un par de veces); duro para un bot.
func NewPairingRequestRateLimiter() *federation.RateLimiter {
	return federation.NewRateLimiter(clock.New(), 5, 3)
}
