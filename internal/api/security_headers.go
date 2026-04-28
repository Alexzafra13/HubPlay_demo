package api

import (
	"net/http"
	"strings"
)

// SecurityHeaders returns a middleware that sets a baseline of HTTP
// security response headers on every API and SPA response.
//
// The CSP is shaped for the bundled SPA (script + CSS served from the
// same origin). The directives mirror the only third-party content the
// app actually pulls in:
//
//   - img-src       TMDb posters / Fanart artwork (admin image picker
//                   renders provider URLs directly), plus data: and blob:
//                   for inline thumbnails and HLS subtitle conversions.
//   - frame-src     YouTube nocookie + Vimeo for the hero trailer embed.
//   - style-src     Google Fonts CSS (loaded from index.html), plus
//                   'unsafe-inline' for React's `style={…}` prop and
//                   Tailwind v4's runtime style injection.
//   - font-src      Google Fonts woff2.
//   - media-src     blob: for HLS MediaSource buffers; 'self' for direct
//                   playback over the API.
//   - connect-src   API + SSE on the same origin only.
//
// Adding a new third-party host (a different image CDN, a new embed
// platform) means adding it here; otherwise the browser will block it
// silently in production. There is no report-uri configured — the SPA
// is single-tenant and we'd rather find regressions in dev than collect
// reports.
//
// frame-ancestors 'none' is the modern equivalent of X-Frame-Options
// DENY. We send both so old enterprise browsers still get clickjacking
// protection.
//
// HSTS is gated on actually being served over HTTPS (direct TLS or via
// a reverse proxy that sets X-Forwarded-Proto). Sending it over plain
// HTTP would be ignored by browsers anyway, but emitting it conditionally
// keeps audit logs clean and avoids confusing operators inspecting the
// LAN endpoint.
func SecurityHeaders() func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
		"img-src 'self' data: blob: https://image.tmdb.org https://assets.fanart.tv",
		"media-src 'self' blob:",
		"frame-src https://www.youtube-nocookie.com https://player.vimeo.com",
		"connect-src 'self'",
		"font-src 'self' data: https://fonts.gstatic.com",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", csp)
			// Cross-origin isolation: deny embedding our docs as resources
			// from another origin. Conservative default — flip to
			// 'cross-origin' if you ever need to expose poster URLs to a
			// third-party page.
			h.Set("Cross-Origin-Resource-Policy", "same-origin")

			if isHTTPS(r) {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isHTTPS reports whether the request reached us over TLS, either
// directly or through a TLS-terminating reverse proxy that set
// X-Forwarded-Proto. We trust that header because in self-hosted
// deployments the proxy is part of the same trust boundary as the app.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	return false
}
