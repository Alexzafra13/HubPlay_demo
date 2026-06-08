package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPrivateOrLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":        true,
		"127.0.0.1:5555":   true,
		"10.0.0.5":         true,
		"192.168.1.20":     true,
		"172.16.5.5":       true,
		"169.254.1.1":      true, // link-local
		"::1":              true,
		"[::1]:8096":       true,
		"fd00::1":          true, // ULA
		"203.0.113.9":      false,
		"8.8.8.8":          false,
		"1.1.1.1:443":      false,
		"no-es-una-ip":     false,
	}
	for in, want := range cases {
		if got := isPrivateOrLoopback(in); got != want {
			t.Errorf("isPrivateOrLoopback(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestRequirePrivateClient comprueba que el middleware deja pasar a la LAN
// y corta a una IP pública. Usa RemoteAddr (sin proxy declarado, ClientIP
// cae a RemoteAddr).
func TestRequirePrivateClient(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := RequirePrivateClient(next)

	t.Run("LAN pasa", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/setup", nil)
		req.RemoteAddr = "192.168.1.50:40000"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("LAN: got %d want 200", rr.Code)
		}
	})

	t.Run("IP pública bloqueada 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth/setup", nil)
		req.RemoteAddr = "203.0.113.9:40000"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("WAN: got %d want 403", rr.Code)
		}
	})
}
