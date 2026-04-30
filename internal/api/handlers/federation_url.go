package handlers

import (
	"net/http"
	"strings"
)

// deriveURLFromRequest produces a best-guess "what URL was used to
// reach me?" string. Used by the federation surface to make
// AdvertisedURL plug-and-play: if the admin hasn't set
// HUBPLAY_SERVER_BASE_URL or `server.base_url` in settings, we just
// echo back the URL the caller used.
//
// Order of precedence (most-trusted to least):
//
//  1. X-Forwarded-Proto + X-Forwarded-Host  — set by reverse proxies
//     (Caddy, nginx, Traefik, Cloudflare). The proxy is the
//     authoritative source for "what host the user typed".
//  2. r.TLS != nil ? "https" : "http"  + r.Host  — direct connection.
//
// The result is a URL with scheme + host + optional port (whatever
// arrived). No path; the federation protocol always tacks /api/v1/...
// on top, so a trailing slash here would make joinBaseURL produce
// double-slashes which some servers reject.
//
// This function is intentionally permissive about untrusted headers
// (X-Forwarded-*) because federation isn't an authentication path —
// the actual auth comes from Ed25519 signatures on every request.
// A spoofed X-Forwarded-Host just means we'd advertise the wrong
// URL to the peer, who would then fail to reach us. Worst case is
// "doesn't work"; never "wrong-server-trusted".
func deriveURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		// Some proxies send "https, http" — take the first.
		if comma := strings.IndexByte(v, ','); comma > 0 {
			v = v[:comma]
		}
		scheme = strings.TrimSpace(v)
	}

	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		// Same multi-value handling.
		if comma := strings.IndexByte(v, ','); comma > 0 {
			v = v[:comma]
		}
		host = strings.TrimSpace(v)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}
