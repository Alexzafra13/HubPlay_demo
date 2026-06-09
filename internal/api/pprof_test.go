package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func pprofGet(r chi.Router, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestMountPprof_DisabledByDefault(t *testing.T) {
	r := chi.NewRouter()
	mountPprofEndpoint(r, Dependencies{}) // PprofEnabled == false

	if rr := pprofGet(r, "/debug/pprof/", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("pprof disabled: got %d, want 404", rr.Code)
	}
}

func TestMountPprof_EnabledWithoutTokenFailsClosed(t *testing.T) {
	r := chi.NewRouter()
	var deps Dependencies
	deps.Server.PprofEnabled = true // but no MetricsToken
	mountPprofEndpoint(r, deps)

	if rr := pprofGet(r, "/debug/pprof/", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("pprof enabled w/o token must not mount: got %d, want 404", rr.Code)
	}
}

func TestMountPprof_TokenGated(t *testing.T) {
	r := chi.NewRouter()
	var deps Dependencies
	deps.Server.PprofEnabled = true
	deps.Server.MetricsToken = "s3cret"
	mountPprofEndpoint(r, deps)

	// No token → 401.
	if rr := pprofGet(r, "/debug/pprof/", ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("pprof without token: got %d, want 401", rr.Code)
	}
	// Wrong token → 401.
	if rr := pprofGet(r, "/debug/pprof/", "nope"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("pprof wrong token: got %d, want 401", rr.Code)
	}
	// Correct token → index served.
	if rr := pprofGet(r, "/debug/pprof/", "s3cret"); rr.Code != http.StatusOK {
		t.Fatalf("pprof with token: got %d, want 200", rr.Code)
	}
}
