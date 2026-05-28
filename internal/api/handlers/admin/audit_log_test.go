package admin_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers/admin"
	"hubplay/internal/db"
)

type fakeAuditStore struct {
	rows  []db.AuditLogRow
	total int64
	types []string
	// lastQuery captura los filtros recibidos en la última llamada
	// — los tests inspeccionan que el parseo del query string funcionó.
	lastQuery db.AuditQuery
}

func (f *fakeAuditStore) Query(_ context.Context, q db.AuditQuery) ([]db.AuditLogRow, int64, error) {
	f.lastQuery = q
	return f.rows, f.total, nil
}
func (f *fakeAuditStore) DistinctEventTypes(_ context.Context) ([]string, error) {
	return f.types, nil
}

func mountAudit(h *admin.AuditLogHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/audit", h.Query)
	r.Get("/audit/types", h.EventTypes)
	return r
}

func TestAuditLogHandler_Query_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store := &fakeAuditStore{
		rows: []db.AuditLogRow{
			{ID: "e1", EventType: "auth.login.ok", ActorUserID: "u-1", CreatedAt: now},
		},
		total: 1,
	}
	h := admin.NewAuditLogHandler(store, slog.Default())

	req := httptest.NewRequest("GET", "/audit", nil)
	rr := httptest.NewRecorder()
	mountAudit(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Data struct {
			Rows  []map[string]any `json:"rows"`
			Total int64            `json:"total"`
			Limit int              `json:"limit"`
		} `json:"data"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&payload)
	if payload.Data.Total != 1 || len(payload.Data.Rows) != 1 {
		t.Errorf("payload = %+v", payload)
	}
	if payload.Data.Rows[0]["event_type"] != "auth.login.ok" {
		t.Errorf("row shape: %v", payload.Data.Rows[0])
	}
}

func TestAuditLogHandler_Query_ParsesAllFilters(t *testing.T) {
	store := &fakeAuditStore{}
	h := admin.NewAuditLogHandler(store, slog.Default())

	req := httptest.NewRequest("GET",
		"/audit?type=auth.&actor=u-alex&from=2026-05-01T00:00:00Z&to=2026-05-31T00:00:00Z&q=ip&limit=25&offset=50",
		nil)
	rr := httptest.NewRecorder()
	mountAudit(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	got := store.lastQuery
	if got.EventTypePrefix != "auth." {
		t.Errorf("event_type_prefix = %q", got.EventTypePrefix)
	}
	if got.ActorUserID != "u-alex" {
		t.Errorf("actor = %q", got.ActorUserID)
	}
	if got.From.IsZero() || got.To.IsZero() {
		t.Errorf("time window empty: from=%v to=%v", got.From, got.To)
	}
	if got.SearchText != "ip" {
		t.Errorf("search = %q", got.SearchText)
	}
	if got.Limit != 25 || got.Offset != 50 {
		t.Errorf("pagination wrong: limit=%d offset=%d", got.Limit, got.Offset)
	}
}

func TestAuditLogHandler_Query_RejectsMalformedFrom(t *testing.T) {
	h := admin.NewAuditLogHandler(&fakeAuditStore{}, slog.Default())
	req := httptest.NewRequest("GET", "/audit?from=not-a-date", nil)
	rr := httptest.NewRecorder()
	mountAudit(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}

func TestAuditLogHandler_EventTypes(t *testing.T) {
	store := &fakeAuditStore{
		types: []string{"auth.login.ok", "permission.changed", "system.restart"},
	}
	h := admin.NewAuditLogHandler(store, slog.Default())

	req := httptest.NewRequest("GET", "/audit/types", nil)
	rr := httptest.NewRecorder()
	mountAudit(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var payload struct {
		Data []string `json:"data"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&payload)
	if len(payload.Data) != 3 {
		t.Errorf("types = %v", payload.Data)
	}
}
