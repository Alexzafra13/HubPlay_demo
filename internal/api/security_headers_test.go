package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// noopHandler is what the middleware should still be able to wrap without
// any of the response-writing assumptions affecting our header checks.
var noopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestSecurityHeaders_StaticHeadersPresent(t *testing.T) {
	mw := SecurityHeaders()(noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	cases := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for header, want := range cases {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestSecurityHeaders_CSPCoversThirdPartyHosts(t *testing.T) {
	mw := SecurityHeaders()(noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header missing")
	}

	// Each of these is rendered by some piece of the SPA. Removing any
	// of them is a regression — the browser will silently block the
	// resource and the feature breaks in production only.
	wantSubstrings := []string{
		"https://image.tmdb.org",
		"https://assets.fanart.tv",
		"https://www.youtube-nocookie.com",
		"https://player.vimeo.com",
		"https://fonts.googleapis.com",
		"https://fonts.gstatic.com",
		"frame-ancestors 'none'",
		"object-src 'none'",
	}
	for _, sub := range wantSubstrings {
		if !strings.Contains(csp, sub) {
			t.Errorf("CSP missing %q\nfull header: %s", sub, csp)
		}
	}

	// connect-src must allow the YouTube + Vimeo oEmbed origins. The
	// HeroTrailer pre-flight hits these before mounting the iframe; if
	// the directive ever drops back to 'self' only, every trailer
	// silently fails-closed in production with no JS console signal
	// beyond a CSP violation. Pin it as a directive-scoped match so
	// listing the host under any other directive (e.g. img-src) won't
	// accidentally satisfy this assertion.
	if connect := directive(csp, "connect-src"); connect == "" {
		t.Fatal("connect-src directive missing entirely")
	} else {
		for _, host := range []string{"https://www.youtube.com", "https://vimeo.com"} {
			if !strings.Contains(connect, host) {
				t.Errorf("connect-src missing %q (oEmbed pre-flight will be blocked)\ndirective: %s", host, connect)
			}
		}
	}
}

// directive extracts a single CSP directive (e.g. "connect-src ...") from a
// full Content-Security-Policy header value. Returns "" if the directive is
// not present. Used by the test to assert host placement, not just header
// presence — putting youtube.com under img-src by mistake would silently
// satisfy a naive substring check.
func directive(csp, name string) string {
	for _, part := range strings.Split(csp, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+" ") || part == name {
			return part
		}
	}
	return ""
}

func TestSecurityHeaders_HSTSOnlyOverHTTPS(t *testing.T) {
	mw := SecurityHeaders()(noopHandler)

	t.Run("plain HTTP omits HSTS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS set over plain HTTP: %q", got)
		}
	})

	t.Run("direct TLS sets HSTS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.TLS = &tls.ConnectionState{}
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if got := rr.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=") {
			t.Errorf("HSTS missing or malformed under TLS: %q", got)
		}
	})

	t.Run("X-Forwarded-Proto=https sets HSTS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if got := rr.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=") {
			t.Errorf("HSTS missing behind reverse proxy: %q", got)
		}
	})

	t.Run("X-Forwarded-Proto=http omits HSTS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-Proto", "http")
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
		if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("HSTS set behind plain-HTTP proxy: %q", got)
		}
	})
}
