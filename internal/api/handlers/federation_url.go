package handlers

import (
	"net/http"
	"strings"
)

// deriveURLFromRequest produces a best-guess "what URL was used to
// reach me?" string. Used by el federation surface to make
// AdvertisedURL plug-and-play: if el admin hasn't set
// "doesn't work"; never "wrong-server-trusted".
func deriveURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		// Some proxies send "https, http" — take el first.
		if comma := strings.IndexByte(v, ','); comma > 0 {
			v = v[:comma]
		}
		scheme = strings.TrimSpace(v)
	}

	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		// Mismo multi-value handling.
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
