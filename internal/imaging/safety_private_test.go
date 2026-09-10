package imaging

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSafeGetWith_AllowPrivate_ReachesLoopback: con AllowPrivate el
// operador puede alcanzar servicios de su LAN/loopback (p.ej. un Prowlarr
// empaquetado) sin desactivar el resto del guard.
func TestSafeGetWith_AllowPrivate_ReachesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("d8:announce0:e"))
	}))
	defer srv.Close()

	// Default (sin opt-in): loopback bloqueado.
	if _, _, err := SafeGet(srv.URL, 1<<20, time.Second); !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("default SafeGet must block loopback, got %v", err)
	}
	// Opt-in: loopback permitido.
	data, _, err := SafeGetWith(srv.URL, 1<<20, time.Second, SafeGetOpts{AllowPrivate: true})
	if err != nil {
		t.Fatalf("AllowPrivate should reach loopback: %v", err)
	}
	if string(data) != "d8:announce0:e" {
		t.Errorf("body = %q", data)
	}
}

// TestSafeGetWith_AllowPrivate_StillBlocksLinkLocal: relajar el rango
// privado NO abre la metadata de cloud (169.254.169.254) ni unspecified
// ni multicast. No se hace red: el guard falla antes de conectar.
func TestSafeGetWith_AllowPrivate_StillBlocksLinkLocal(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/x",
		"http://224.0.0.1/x",
		"http://[fe80::1]/x",
		"ftp://127.0.0.1/x",
	} {
		_, _, err := SafeGetWith(u, 1<<20, time.Second, SafeGetOpts{AllowPrivate: true})
		if !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("%s: want ErrUnsafeURL even with AllowPrivate, got %v", u, err)
		}
	}
}

// TestSafeGetWith_AllowPrivate_RedirectToLinkLocalBlocked: los redirects
// se siguen re-validando con el guard relajado.
func TestSafeGetWith_AllowPrivate_RedirectToLinkLocalBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()
	_, _, err := SafeGetWith(srv.URL, 1<<20, time.Second, SafeGetOpts{AllowPrivate: true})
	if err == nil || !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("redirect to link-local must be blocked, got %v", err)
	}
}
