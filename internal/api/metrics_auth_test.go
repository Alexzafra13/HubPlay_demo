package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequireMetricsToken fija el gate opt-in de /metrics (olor A5): con
// token configurado, solo pasa quien presenta el Bearer (o ?token=)
// correcto; sin credencial o con una errónea → 401.
func TestRequireMetricsToken(t *testing.T) {
	const token = "s3cr3t-metrics-token"
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# HELP hubplay_up\n"))
	})
	h := requireMetricsToken(token, inner)

	do := func(mut func(*http.Request)) int {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		mut(req)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	cases := []struct {
		name string
		mut  func(*http.Request)
		want int
	}{
		{"Bearer correcto", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, http.StatusOK},
		{"query token correcto", func(r *http.Request) {
			q := r.URL.Query()
			q.Set("token", token)
			r.URL.RawQuery = q.Encode()
		}, http.StatusOK},
		{"Bearer incorrecto", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, http.StatusUnauthorized},
		{"query incorrecto", func(r *http.Request) {
			q := r.URL.Query()
			q.Set("token", "nope")
			r.URL.RawQuery = q.Encode()
		}, http.StatusUnauthorized},
		{"sin credencial", func(*http.Request) {}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(tc.mut); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
}
