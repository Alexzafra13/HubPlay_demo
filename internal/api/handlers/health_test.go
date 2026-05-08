package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/testutil"
)

// Reuses fakeStreamManager from stream_test.go for the StreamManagerService
// dependency. The DB handle comes from testutil.NewTestDB (in-memory sqlite).

func TestHealthHandler_Health_ReturnsOK(t *testing.T) {
	database := testutil.NewTestDB(t)
	sm := newFakeStreamManager()
	h := NewHealthHandler(database, sm, "test-1.2.3", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("status field: %v", out["status"])
	}
	if out["version"] != "test-1.2.3" {
		t.Errorf("version: %v", out["version"])
	}
	if out["database"] != "ok" {
		t.Errorf("database should be 'ok' on a healthy test DB, got %v", out["database"])
	}
	// active_streams should be zero — nothing running.
	if out["active_streams"] != float64(0) {
		t.Errorf("active_streams: %v", out["active_streams"])
	}
	// Memory + goroutines exposed as numbers.
	for _, k := range []string{"uptime_seconds", "goroutines", "memory_alloc_mb", "memory_sys_mb"} {
		if _, ok := out[k].(float64); !ok {
			t.Errorf("field %q missing or wrong type: %T", k, out[k])
		}
	}
}

func TestHealthHandler_Health_ReportsDBError_With503(t *testing.T) {
	// Open then close a DB so Ping() errors. Probes must see a non-2xx
	// status so the load balancer drains the node instead of routing
	// requests into a broken backend.
	database := testutil.NewTestDB(t)
	_ = database.Close() // t.Cleanup also closes, which is fine — double-close is ignored
	sm := newFakeStreamManager()
	h := NewHealthHandler(database, sm, "v", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("DB-down /health must return 503, got %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	dbStatus, _ := out["database"].(string)
	if dbStatus == "ok" {
		t.Errorf("database status should report the error, got %q", dbStatus)
	}
	if out["status"] != "unavailable" {
		t.Errorf("status field on DB error should be 'unavailable', got %v", out["status"])
	}
}

func TestHealthHandler_Health_NilStreamManager_ZeroStreams(t *testing.T) {
	database := testutil.NewTestDB(t)
	h := NewHealthHandler(database, nil, "v", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	if out["active_streams"] != float64(0) {
		t.Errorf("nil stream manager should yield 0 active_streams, got %v", out["active_streams"])
	}
}

func TestHealthHandler_Live_AlwaysOK_EvenWithoutDB(t *testing.T) {
	// Liveness must NOT depend on the DB — it answers "is the process
	// alive enough to receive traffic?" Killing the DB should not flip
	// /health/live or Kubernetes will restart healthy pods on every
	// transient SQLite hiccup.
	database := testutil.NewTestDB(t)
	_ = database.Close()
	h := NewHealthHandler(database, nil, "v", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr := httptest.NewRecorder()
	h.Live(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Live must return 200 even with broken DB, got %d", rr.Code)
	}
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("status: %v", out["status"])
	}
}

func TestHealthHandler_Ready_DBPingsOK(t *testing.T) {
	database := testutil.NewTestDB(t)
	h := NewHealthHandler(database, nil, "v", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.Ready(rr, req)

	// We don't assert overall HTTP status here because Ready also
	// gates on ffmpeg-in-PATH and free disk space, both of which
	// are environment-dependent (CI runners may or may not have
	// ffmpeg, /tmp may be tight). The 503-on-DB-down test below
	// covers the "fail hard" case; this test only verifies that
	// DB ping itself reports as healthy.
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	if out["database"] != "ok" {
		t.Errorf("database: %v", out["database"])
	}
}

func TestHealthHandler_Ready_503OnDBDown(t *testing.T) {
	database := testutil.NewTestDB(t)
	_ = database.Close()
	h := NewHealthHandler(database, nil, "v", ":memory:")

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("Ready with broken DB must return 503, got %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	if out["status"] != "unavailable" {
		t.Errorf("status: %v", out["status"])
	}
	dbStatus, _ := out["database"].(string)
	if dbStatus == "ok" {
		t.Errorf("database status should report the error, got %q", dbStatus)
	}
}
