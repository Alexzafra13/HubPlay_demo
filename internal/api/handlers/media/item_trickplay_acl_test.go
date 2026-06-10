package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/imaging"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"
)

// Stub mínimo del lookup de items para el handler de trickplay.
type trickplayLibStub struct {
	fn func(ctx context.Context, id string) (*librarymodel.Item, error)
}

func (s *trickplayLibStub) GetItem(ctx context.Context, id string) (*librarymodel.Item, error) {
	return s.fn(ctx, id)
}

// trickplayACLEnv monta un TrickplayHandler con gate ACL real y un
// item que vive en "lib-restricted".
func trickplayACLEnv(t *testing.T, access LibraryACL) (chi.Router, string) {
	t.Helper()
	dir := t.TempDir()
	lib := &trickplayLibStub{fn: func(_ context.Context, id string) (*librarymodel.Item, error) {
		return &librarymodel.Item{
			ID: id, LibraryID: "lib-restricted", Type: "movie",
			Title: "Secret", Path: "/dev/null",
		}, nil
	}}
	h := newTrickplayHandler(lib, access, dir, testutil.NopLogger())
	t.Cleanup(h.WaitTrickplayInflight)

	r := chi.NewRouter()
	r.Route("/api/v1/items/{id}", func(r chi.Router) {
		r.Get("/trickplay.json", h.TrickplayManifest)
		r.Get("/trickplay.png", h.TrickplaySprite)
	})
	return r, dir
}

func trickplayGet(t *testing.T, router chi.Router, path string, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// PB-12 (audit 2026-06-10): trickplay era el único surface de playback
// sin gate de biblioteca — cualquier usuario autenticado podía ver la
// timeline visual (el sprite son ~200 frames de la película) de
// bibliotecas restringidas. El gate corre ANTES de servir el cache.
func TestTrickplayACL_UserWithoutAccess_GetsNotFound(t *testing.T) {
	t.Parallel()
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{}}
	router, dir := trickplayACLEnv(t, access)
	// Cache fresco en disco: el deny debe ganar igualmente.
	trickplayWriteCache(t, filepath.Join(dir, "it-1"), imaging.TrickplayManifestVersion)
	bob := &auth.Claims{UserID: "bob", Role: "user"}

	for _, p := range []string{
		"/api/v1/items/it-1/trickplay.json",
		"/api/v1/items/it-1/trickplay.png",
	} {
		rr := trickplayGet(t, router, p, bob)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404 (ACL deny)", p, rr.Code)
		}
	}
}

func TestTrickplayACL_UserWithAccess_ServesFreshCache(t *testing.T) {
	t.Parallel()
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{
		"alice": {"lib-restricted": true},
	}}
	router, dir := trickplayACLEnv(t, access)
	trickplayWriteCache(t, filepath.Join(dir, "it-1"), imaging.TrickplayManifestVersion)

	rr := trickplayGet(t, router, "/api/v1/items/it-1/trickplay.json",
		&auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// PB-13 (audit 2026-06-10): un intento de generación fallido reciente
// responde 404 TRICKPLAY_UNAVAILABLE sin Retry-After — el frontend
// deja de poolear y no se relanza ffmpeg en bucle contra un fichero
// que no genera.
func TestTrickplay_FailedMarker_Returns404WithoutRetry(t *testing.T) {
	t.Parallel()
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{
		"alice": {"lib-restricted": true},
	}}
	router, dir := trickplayACLEnv(t, access)
	itemDir := filepath.Join(dir, "it-failed")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(itemDir, trickplayFailedMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	rr := trickplayGet(t, router, "/api/v1/items/it-failed/trickplay.json",
		&auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") != "" {
		t.Error("404 de fallo no debe llevar Retry-After (el cliente dejaría de poolear)")
	}
	if !strings.Contains(rr.Body.String(), "TRICKPLAY_UNAVAILABLE") {
		t.Errorf("expected TRICKPLAY_UNAVAILABLE code, body=%s", rr.Body.String())
	}
}

// Marcador caducado (>TTL): se permite el reintento — vuelve el flujo
// normal de 503 pending + regeneración (el fichero puede haber sido
// reemplazado por una versión sana).
func TestTrickplay_StaleFailedMarker_AllowsRetry(t *testing.T) {
	t.Parallel()
	access := &fakeLibraryAccess{allow: map[string]map[string]bool{
		"alice": {"lib-restricted": true},
	}}
	router, dir := trickplayACLEnv(t, access)
	itemDir := filepath.Join(dir, "it-retry")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(itemDir, trickplayFailedMarker)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-trickplayFailedTTL - time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}

	rr := trickplayGet(t, router, "/api/v1/items/it-retry/trickplay.json",
		&auth.Claims{UserID: "alice", Role: "user"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 pending (retry allowed); body=%s", rr.Code, rr.Body.String())
	}
}
