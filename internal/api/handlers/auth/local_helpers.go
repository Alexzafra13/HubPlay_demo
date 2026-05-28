package authhandler

import (
	"net/http"
	"strings"
	"time"
)

// avatarPublicURL returns the public URL for a user avatar given the
// userID + relName. Embeds relName as a query param so the client can
// use it as a cache-buster without the server dispatching by path.
func avatarPublicURL(userID, relName string) string {
	if relName == "" {
		return ""
	}
	return "/api/v1/users/" + userID + "/avatar?v=" + relName
}

// sseKeepaliveInterval keeps an idle SSE stream below the typical
// reverse-proxy idle cutoff (nginx default = 60s). Comment lines are
// invisible to EventSource consumers but reset the proxy idle timer.
// 25s leaves comfortable margin against jittery 30s upstream caps too.
const sseKeepaliveInterval = 25 * time.Second

// deriveURLFromRequest infers the external base URL from the inbound
// request's Host + proxy headers. Intentionally permissive about
// untrusted headers — a spoofed X-Forwarded-Host just means we'd
// advertise the wrong URL, which would then fail to reach us.
func deriveURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		if comma := strings.IndexByte(v, ','); comma > 0 {
			v = v[:comma]
		}
		scheme = strings.TrimSpace(v)
	}

	host := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
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
