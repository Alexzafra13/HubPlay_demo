package handlers

import (
	"net/http"

	"hubplay/internal/clock"
	"hubplay/internal/federation"
)

// IPRateLimitMiddleware aplica un token-bucket por IP cliente al
// endpoint envuelto. Pensado para endpoints PUBLICOS sin auth donde
// un atacante en internet puede martillear (e.g. el inbox de pairing
// requests recibe POSTs de servidores remotos sin auth previa).
//
// Reutiliza federation.RateLimiter usando la IP como bucket key —
// el patrón es idéntico al per-peer limit que ya teníamos.
//
//   - 429 + Retry-After: 60 cuando se agotan los tokens.
//   - El IP se obtiene de `ClientIP(r)` que lee del ctx (set por el
//     middleware ClientIPFromXFF / ClientIPFromRemoteAddr cableado en
//     `applyGlobalMiddleware`). Operador con proxy declarado en
//     `server.trusted_proxies` ⇒ XFF honrado de forma segura; sin
//     proxy declarado ⇒ RemoteAddr de la conexión TCP.
func IPRateLimitMiddleware(limiter *federation.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
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

// NewPairingRequestRateLimiter es un constructor de conveniencia con
// limites razonables para el endpoint publico de pairing requests:
// 5 requests/min por IP, burst 3. Suficiente para flow legitimo
// (admin pulsa "enviar" un par de veces); duro para un bot.
func NewPairingRequestRateLimiter() *federation.RateLimiter {
	return federation.NewRateLimiter(clock.New(), 5, 3)
}

// NewAuthRateLimiter limita los endpoints publicos de auth (login,
// refresh, setup, device start/poll) por IP. 30 req/min con burst 10:
// holgado para un humano que reintenta o un cliente headless polleando
// el device-code cada ~5s (RFC 8628), pero corta en seco un flood de
// fuerza-bruta (que necesitaria miles/min). Defensa en profundidad sobre
// el rate-limit per-cuenta de internal/auth, que NO debe depender de que
// el operador configure el reverse proxy (cierra A1 del audit prod).
func NewAuthRateLimiter() *federation.RateLimiter {
	return federation.NewRateLimiter(clock.New(), 30, 10)
}
