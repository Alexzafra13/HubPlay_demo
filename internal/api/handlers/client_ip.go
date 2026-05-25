package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// ClientIP devuelve la dirección IP del cliente para audit logs, rate
// limit, y enriquecimiento del access log. El middleware global
// (`ClientIPFromXFF` / `ClientIPFromRemoteAddr` configurado vía
// `Dependencies.TrustedProxies` en `applyGlobalMiddleware`) deja el IP
// en el ctx del request; este helper lo lee y cae a `r.RemoteAddr` si
// el middleware no se cableó (caso tests que llaman handlers sin
// router completo).
//
// Reemplaza el patrón histórico de leer `r.RemoteAddr` directo. El
// pre-refactor wireba `middleware.RealIP` que mutaba RemoteAddr leyendo
// XFF sin validar — vulnerable a 3 CVE de IP spoofing (incluida
// GHSA-3fxj-6jh8-hvhx Critical 9.3), deprecado en chi v5.3.0. La nueva
// API NO muta RemoteAddr; los handlers deben leer el ctx con
// `GetClientIP` (o este helper).
func ClientIP(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
