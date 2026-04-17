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
	h := NewHealthHandler(database, sm, "test-1.2.3")

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

func TestHealthHandler_Health_ReportsDBError(t *testing.T) {
	// Open then close a DB so Ping() errors.
	database := testutil.NewTestDB(t)
	_ = database.Close() // t.Cleanup also closes, which is fine — double-close is ignored
	sm := newFakeStreamManager()
	h := NewHealthHandler(database, sm, "v")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("handler should still respond 200 even on DB error, got %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	dbStatus, _ := out["database"].(string)
	if dbStatus == "ok" {
		t.Errorf("database status should report the error, got %q", dbStatus)
	}
}

func TestHealthHandler_Health_NilStreamManager_ZeroStreams(t *testing.T) {
	database := testutil.NewTestDB(t)
	h := NewHealthHandler(database, nil, "v")

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
