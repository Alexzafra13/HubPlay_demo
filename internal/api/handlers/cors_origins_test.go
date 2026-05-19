package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/db"
)

// ─── Fakes ────────────────────────────────────────────────────────

type fakeCorsStore struct {
	mu      sync.Mutex
	rows    []db.CorsOriginRow
	insertErr error
}

func (f *fakeCorsStore) List(_ context.Context) ([]db.CorsOriginRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]db.CorsOriginRow(nil), f.rows...), nil
}

func (f *fakeCorsStore) Insert(_ context.Context, row db.CorsOriginRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return f.insertErr
	}
	for _, r := range f.rows {
		if r.Origin == row.Origin {
			return db.ErrCorsOriginExists
		}
	}
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeCorsStore) Delete(_ context.Context, origin string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.rows[:0]
	for _, r := range f.rows {
		if r.Origin != origin {
			out = append(out, r)
		}
	}
	f.rows = out
	return nil
}

func (f *fakeCorsStore) ListOrigins(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r.Origin)
	}
	return out, nil
}

type fakeRegistry struct {
	mu       sync.Mutex
	statics  []string
	dynamics []string
}

func (f *fakeRegistry) SetDynamics(o []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dynamics = append([]string(nil), o...)
}
func (f *fakeRegistry) Statics() []string {
	return append([]string(nil), f.statics...)
}

// ─── Test helpers ─────────────────────────────────────────────────

func mountCors(h *handlers.CorsOriginsHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/cors", h.List)
	r.Post("/cors", h.Add)
	r.Delete("/cors", h.Delete)
	return r
}

func corsRequest(handler http.Handler, method, path string, body string, userID string) *httptest.ResponseRecorder {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if userID != "" {
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: userID}))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func okValidator(s string) (string, error) {
	// Mismo contrato que api.ValidateCorsOrigin pero sin las reglas
	// (los tests del handler chequean la lógica DEL HANDLER, no la
	// del validator que ya tiene sus propios tests en internal/api).
	return strings.TrimSpace(s), nil
}

func rejectValidator(s string) (string, error) {
	return "", errors.New("rejected by test validator")
}

// ─── Tests ────────────────────────────────────────────────────────

func TestCorsOriginsHandler_List_EmptyDynamics(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{statics: []string{"https://static.example.com"}}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodGet, "/cors", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var payload struct {
		Data struct {
			Statics  []string         `json:"statics"`
			Dynamics []map[string]any `json:"dynamics"`
		} `json:"data"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&payload)
	if len(payload.Data.Statics) != 1 || payload.Data.Statics[0] != "https://static.example.com" {
		t.Errorf("statics = %v", payload.Data.Statics)
	}
	if len(payload.Data.Dynamics) != 0 {
		t.Errorf("dynamics should be empty: %v", payload.Data.Dynamics)
	}
}

func TestCorsOriginsHandler_Add_HappyPath(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodPost, "/cors",
		`{"origin":"https://new.example.com","note":"hello"}`, "u-owner")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(store.rows) != 1 || store.rows[0].Origin != "https://new.example.com" {
		t.Errorf("not inserted: %+v", store.rows)
	}
	if store.rows[0].CreatedBy != "u-owner" {
		t.Errorf("created_by = %q", store.rows[0].CreatedBy)
	}
	if store.rows[0].Note != "hello" {
		t.Errorf("note lost: %q", store.rows[0].Note)
	}
	// Reload se llamó (registry conoce el nuevo dynamic).
	if len(reg.dynamics) != 1 || reg.dynamics[0] != "https://new.example.com" {
		t.Errorf("registry not reloaded: %v", reg.dynamics)
	}
}

func TestCorsOriginsHandler_Add_RejectsBadValidator(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{}
	h := handlers.NewCorsOriginsHandler(store, reg, rejectValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodPost, "/cors", `{"origin":"anything"}`, "u-1")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
	if len(store.rows) != 0 {
		t.Error("insert happened despite validator rejection")
	}
}

func TestCorsOriginsHandler_Add_RejectsDuplicateStatic(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{statics: []string{"https://yaml.example.com"}}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodPost, "/cors",
		`{"origin":"https://yaml.example.com"}`, "u-owner")
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d (want 409)", rr.Code)
	}
	if len(store.rows) != 0 {
		t.Error("insert happened despite static duplicate")
	}
}

func TestCorsOriginsHandler_Add_RejectsDuplicateDynamic(t *testing.T) {
	store := &fakeCorsStore{
		rows: []db.CorsOriginRow{
			{Origin: "https://existing.example.com", CreatedBy: "u-prev"},
		},
	}
	reg := &fakeRegistry{}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodPost, "/cors",
		`{"origin":"https://existing.example.com"}`, "u-owner")
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d (want 409)", rr.Code)
	}
}

func TestCorsOriginsHandler_Delete_HappyPath(t *testing.T) {
	store := &fakeCorsStore{
		rows: []db.CorsOriginRow{
			{Origin: "https://to-remove.example.com"},
		},
	}
	reg := &fakeRegistry{}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodDelete,
		"/cors?origin=https%3A%2F%2Fto-remove.example.com", "", "u-owner")
	if rr.Code != http.StatusNoContent {
		t.Errorf("status %d (want 204) body %s", rr.Code, rr.Body.String())
	}
	if len(store.rows) != 0 {
		t.Errorf("not deleted: %+v", store.rows)
	}
}

func TestCorsOriginsHandler_Delete_RejectsStatic(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{statics: []string{"https://yaml.example.com"}}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodDelete,
		"/cors?origin=https%3A%2F%2Fyaml.example.com", "", "u-owner")
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d (want 409) body %s", rr.Code, rr.Body.String())
	}
}

func TestCorsOriginsHandler_Delete_MissingOriginParam(t *testing.T) {
	store := &fakeCorsStore{}
	reg := &fakeRegistry{}
	h := handlers.NewCorsOriginsHandler(store, reg, okValidator, slog.Default())

	rr := corsRequest(mountCors(h), http.MethodDelete, "/cors", "", "u-owner")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}
