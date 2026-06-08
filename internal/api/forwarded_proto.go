package api

import (
	"net"
	"net/http"
	"net/netip"
)

// trustForwardedProto borra la cabecera X-Forwarded-Proto de las
// peticiones cuyo peer directo NO es un proxy declarado en
// `trusted_proxies`. El código downstream (isHTTPS en security_headers,
// el flag Secure de la cookie CSRF, y la cookie de auth) lee XFP para
// decidir si la conexión es HTTPS; sin este filtro, un atacante que
// conecte directo podría falsear `X-Forwarded-Proto: https` para forzar
// cookies Secure / HSTS y desincronizar la frontera de confianza
// respecto a XFF (que chi sí valida contra trusted_proxies).
//
// Sin proxies declarados, XFP nunca se honra (se borra siempre): solo
// cuenta el TLS real de la conexión. Cierra el olor M4.
func trustForwardedProto(trustedCIDRs []string) func(http.Handler) http.Handler {
	prefixes := parsePrefixes(trustedCIDRs)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !peerInPrefixes(r.RemoteAddr, prefixes) {
				r.Header.Del("X-Forwarded-Proto")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parsePrefixes(cidrs []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// peerInPrefixes indica si la IP de `remoteAddr` (host:port) cae en
// alguno de los prefijos de confianza. Lista vacía ⇒ nadie es de
// confianza.
func peerInPrefixes(remoteAddr string, prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
