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
