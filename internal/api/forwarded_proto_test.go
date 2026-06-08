package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTrustForwardedProto verifica el fix M4: X-Forwarded-Proto solo
// sobrevive si el peer directo está en trusted_proxies; en otro caso se
// borra para que el downstream no lo crea.
func TestTrustForwardedProto(t *testing.T) {
	cases := []struct {
		name       string
		trusted    []string
		remoteAddr string
		wantKept   bool
	}{
		{"peer de confianza conserva XFP", []string{"127.0.0.1/32"}, "127.0.0.1:5555", true},
		{"peer en CIDR de confianza", []string{"10.0.0.0/8"}, "10.1.2.3:40000", true},
		{"peer no confiable se limpia", []string{"127.0.0.1/32"}, "203.0.113.9:40000", false},
		{"sin proxies declarados nunca se confía", nil, "127.0.0.1:5555", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			h := trustForwardedProto(normalizeCIDRs(tc.trusted))(
				http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					seen = r.Header.Get("X-Forwarded-Proto")
				}),
			)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Header.Set("X-Forwarded-Proto", "https")
			h.ServeHTTP(httptest.NewRecorder(), req)

			if tc.wantKept && seen != "https" {
				t.Errorf("XFP debería conservarse, got %q", seen)
			}
			if !tc.wantKept && seen != "" {
				t.Errorf("XFP debería borrarse, got %q", seen)
			}
		})
	}
}
