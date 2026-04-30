package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"hubplay/internal/api/handlers"
)

func TestOpenAPIHandler_ServesYAML(t *testing.T) {
	h := handlers.NewOpenAPIHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeYAML(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type: got %q want application/yaml", ct)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("ETag header missing")
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("Cache-Control header missing")
	}
	if rec.Body.Len() == 0 {
		t.Error("body is empty")
	}
}

func TestOpenAPIHandler_HonoursIfNoneMatch(t *testing.T) {
	h := handlers.NewOpenAPIHandler()

	// Prime: first GET → 200 + ETag.
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	rec1 := httptest.NewRecorder()
	h.ServeYAML(rec1, req1)
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response missing ETag")
	}

	// Second request with If-None-Match: same → 304, no body.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeYAML(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("expected 304 with matching If-None-Match, got %d", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 must not include a body; got %d bytes", rec2.Body.Len())
	}
}

func TestOpenAPIHandler_HEADSendsHeadersOnly(t *testing.T) {
	h := handlers.NewOpenAPIHandler()

	req := httptest.NewRequest(http.MethodHead, "/api/v1/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeYAML(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status: got %d want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD must not include body; got %d bytes", rec.Body.Len())
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Error("Content-Length should be set on HEAD")
	}
}

// TestOpenAPISpec_ParsesAsValidYAML is the regression test that catches
// a broken spec at compile-time-of-tests rather than at the first
// client to actually fetch it. If someone edits openapi.yaml and breaks
// the YAML syntax, this fails before the binary ships.
func TestOpenAPISpec_ParsesAsValidYAML(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(handlers.OpenAPIBytes(), &doc); err != nil {
		t.Fatalf("openapi.yaml does not parse as valid YAML: %v", err)
	}

	// Sanity: it should be a recognisable OpenAPI document.
	if v, ok := doc["openapi"].(string); !ok || !strings.HasPrefix(v, "3.") {
		t.Errorf("expected `openapi: 3.x.x` at top level, got %v", doc["openapi"])
	}
	if _, ok := doc["paths"]; !ok {
		t.Error("expected `paths` section at top level")
	}
	if _, ok := doc["components"]; !ok {
		t.Error("expected `components` section at top level")
	}
}

// TestOpenAPISpec_CoversCriticalPaths sanity-checks that the most
// important endpoints made it into the spec. If someone deletes a
// critical operation by accident (during a refactor), this test
// surfaces the omission immediately rather than a Kotlin client
// silently losing the ability to log in.
func TestOpenAPISpec_CoversCriticalPaths(t *testing.T) {
	var doc struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(handlers.OpenAPIBytes(), &doc); err != nil {
		t.Fatal(err)
	}
	required := []string{
		"/auth/login",
		"/auth/refresh",
		"/auth/device/start",
		"/auth/device/poll",
		"/me",
		"/libraries",
		"/items/{id}",
		"/stream/{itemId}/master.m3u8",
		"/me/progress/{itemId}",
		"/me/peers",
	}
	for _, path := range required {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("spec is missing critical path %q", path)
		}
	}
}
