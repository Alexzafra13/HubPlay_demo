package api_test

import (
	"sync"
	"testing"

	"hubplay/internal/api"
)

// ─── ValidateCorsOrigin ─────────────────────────────────────────────

func TestValidateCorsOrigin_Accepts(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://app.example.com", "https://app.example.com"},
		{"http://localhost:5173", "http://localhost:5173"},
		// Canonicaliza scheme + host a minúsculas.
		{"HTTPS://APP.EXAMPLE.COM", "https://app.example.com"},
		// Trailing slash se quita.
		{"https://app.example.com/", "https://app.example.com"},
		// Espacios en bordes se ignoran.
		{"  https://x.example.com  ", "https://x.example.com"},
	}
	for _, tc := range cases {
		got, err := api.ValidateCorsOrigin(tc.in)
		if err != nil {
			t.Errorf("Validate(%q) err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Validate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateCorsOrigin_RejectsObvious(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"*",
		"https://*.example.com",      // wildcard en subdomain
		"null",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,<script>",
		"ftp://example.com",
		"https://",                    // sin host
		"https://example.com/path",    // con path
		"https://example.com?q=1",     // con query
		"https://example.com#frag",    // con fragment
		"://example.com",              // sin scheme
		"not a url at all",
	}
	for _, in := range cases {
		_, err := api.ValidateCorsOrigin(in)
		if err == nil {
			t.Errorf("Validate(%q) passed, want error", in)
		}
	}
}

// ─── CorsRegistry ───────────────────────────────────────────────────

func TestCorsRegistry_StaticsMatchAlways(t *testing.T) {
	r := api.NewCorsRegistry([]string{"https://static.example.com"})
	if !r.IsAllowed("https://static.example.com") {
		t.Error("static not allowed")
	}
	if r.IsAllowed("https://other.example.com") {
		t.Error("non-listed origin allowed")
	}
}

func TestCorsRegistry_DynamicsHotReload(t *testing.T) {
	r := api.NewCorsRegistry(nil)

	// Inicialmente vacío — ningún dynamic match.
	if r.IsAllowed("https://added.example.com") {
		t.Error("empty registry allowed an origin")
	}

	r.SetDynamics([]string{"https://added.example.com"})
	if !r.IsAllowed("https://added.example.com") {
		t.Error("dynamic not allowed after SetDynamics")
	}

	// Reemplaza: el origen anterior ya no está.
	r.SetDynamics([]string{"https://replacement.example.com"})
	if r.IsAllowed("https://added.example.com") {
		t.Error("removed dynamic still allowed — atomic store didn't propagate")
	}
	if !r.IsAllowed("https://replacement.example.com") {
		t.Error("new dynamic not allowed")
	}
}

func TestCorsRegistry_StaticsImmutableViaPublicAccessor(t *testing.T) {
	r := api.NewCorsRegistry([]string{"https://a.example.com", "https://b.example.com"})
	statics := r.Statics()
	statics[0] = "https://EVIL.example.com"

	// La mutación del slice devuelto no debe afectar al registry.
	if r.IsAllowed("https://EVIL.example.com") {
		t.Error("Statics() returned a reference, not a clone — security regression")
	}
}

// TestCorsRegistry_ConcurrentReadDuringWrite es pin del invariante
// atómico — un Reload concurrente con N readers no causa data race
// ni respuestas inconsistentes (un origin "antes válido" se
// invalida o sigue válido, nunca queda en estado intermedio).
func TestCorsRegistry_ConcurrentReadDuringWrite(t *testing.T) {
	r := api.NewCorsRegistry([]string{"https://static.example.com"})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 16 readers en bucle apretado.
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					// El static siempre debe pasar; el dynamic puede
					// estar o no, ambos son válidos como respuesta.
					if !r.IsAllowed("https://static.example.com") {
						t.Error("static lost during concurrent Reload")
						return
					}
					_ = r.IsAllowed("https://x.example.com")
				}
			}
		}()
	}

	// Writer que alterna el set dynamic.
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			r.SetDynamics([]string{"https://x.example.com"})
		} else {
			r.SetDynamics([]string{})
		}
	}
	close(stop)
	wg.Wait()
}
