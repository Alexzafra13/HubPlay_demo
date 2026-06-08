package api

import (
	"net"
	"net/http"
	"net/netip"

	"hubplay/internal/api/apperror"
	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
)

// RequirePrivateClient bloquea con 403 las peticiones cuyo cliente no
// esté en una red privada/local. Se usa para acotar la ventana de
// configuración inicial (M3): antes de existir el primer admin,
// `/auth/setup` (reclama admin) y `/setup/*` (incluye navegar el
// filesystem del host) quedaban abiertos a cualquiera que alcanzara el
// puerto — race-to-setup + fuga del filesystem. Restringirlo a la LAN
// mantiene el "enchufar y listo" en casa y cierra el secuestro desde
// internet, sin pasos extra.
//
// La IP se obtiene de handlers.ClientIP (respeta trusted_proxies). Detrás
// de un proxy SIN trusted_proxies declarado se ve la IP del proxy, así
// que el despliegue de referencia declara `127.0.0.1` (ver compose).
func RequirePrivateClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isPrivateOrLoopback(handlers.ClientIP(r)) {
			apperror.Write(w, r.Context(), &domain.AppError{
				Code:       "SETUP_LOCAL_ONLY",
				HTTPStatus: http.StatusForbidden,
				Message:    "la configuración inicial solo se permite desde la red local; usa un túnel o accede desde la LAN",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isPrivateOrLoopback indica si `rawIP` (puede traer puerto) es loopback,
// privada (RFC 1918 / ULA) o link-local. Una IP no parseable se trata
// como NO privada — fail-closed para la ventana de setup.
func isPrivateOrLoopback(rawIP string) bool {
	host := rawIP
	if h, _, err := net.SplitHostPort(rawIP); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
