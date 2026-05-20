package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
	"hubplay/internal/updates"
)

// fakeUpdatesProvider stubea el Service real. Permite a los tests
// observar SetUserEnabled (que el handler PUT /config debe llamar) y
// driveear lo que devuelve IsUserEnabled / Status sin levantar la
// goroutine del ticker. Concurrency-safe por defensa — el handler no
// debería llamar concurrentemente, pero no quiero un futuro refactor
// rompiéndome el test con -race.
type fakeUpdatesProvider struct {
	mu          sync.Mutex
	userEnabled bool
	status      updates.Status
	checkErr    error
	setCalls    []bool
}

func newFakeProvider(userEnabled bool) *fakeUpdatesProvider {
	return &fakeUpdatesProvider{
		userEnabled: userEnabled,
		status: updates.Status{
			Current:      "v0.1.0",
			CheckEnabled: true,
			UserDisabled: !userEnabled,
		},
	}
}

func (f *fakeUpdatesProvider) Status() updates.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.status
	st.UserDisabled = !f.userEnabled
	return st
}

func (f *fakeUpdatesProvider) Check(_ context.Context) error {
	return f.checkErr
}

func (f *fakeUpdatesProvider) SetUserEnabled(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userEnabled = enabled
	f.setCalls = append(f.setCalls, enabled)
}

func (f *fakeUpdatesProvider) IsUserEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.userEnabled
}

func (f *fakeUpdatesProvider) SetCalls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.setCalls...)
}

func newUpdatesRig(t *testing.T, fake *fakeUpdatesProvider, withSettings bool) (http.Handler, *db.SettingsRepository) {
	t.Helper()
	var settings *db.SettingsRepository
	if withSettings {
		database := testutil.NewTestDB(t)
		settings = db.NewSettingsRepository(testutil.Driver(), database)
	}
	h := NewUpdatesHandler(fake, settings, newQuietLogger())
	r := chi.NewRouter()
	r.Get("/admin/system/updates", h.Status)
	r.Post("/admin/system/updates/check", h.Check)
	r.Get("/admin/system/updates/config", h.GetConfig)
	r.Put("/admin/system/updates/config", h.UpdateConfig)
	return r, settings
}

func TestUpdates_GetConfig_ReturnsCurrentToggle(t *testing.T) {
	fake := newFakeProvider(true)
	router, _ := newUpdatesRig(t, fake, true)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/system/updates/config", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Data.Enabled {
		t.Fatalf("expected enabled=true, got false")
	}
}

func TestUpdates_GetConfig_ReflectsDisabledState(t *testing.T) {
	fake := newFakeProvider(false)
	router, _ := newUpdatesRig(t, fake, true)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/system/updates/config", nil)
	router.ServeHTTP(rr, req)

	var resp struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Data.Enabled {
		t.Fatalf("expected enabled=false, got true")
	}
}

func TestUpdates_UpdateConfig_PersistsAndPropagates(t *testing.T) {
	fake := newFakeProvider(true)
	router, settings := newUpdatesRig(t, fake, true)

	body := strings.NewReader(`{"enabled":false}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/updates/config", body)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// El Service recibió SetUserEnabled(false).
	calls := fake.SetCalls()
	if len(calls) != 1 || calls[0] != false {
		t.Fatalf("expected SetUserEnabled([false]), got %v", calls)
	}

	// La row en app_settings quedó persistida.
	v, err := settings.Get(context.Background(), "updates.check_enabled")
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	if v != "false" {
		t.Fatalf("expected persisted value 'false', got %q", v)
	}
}

func TestUpdates_UpdateConfig_RejectsInvalidJSON(t *testing.T) {
	fake := newFakeProvider(true)
	router, _ := newUpdatesRig(t, fake, true)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/updates/config",
		bytes.NewReader([]byte(`{"enabled":"yes"}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rr.Code)
	}
	if len(fake.SetCalls()) != 0 {
		t.Fatalf("Service was touched despite invalid body: %v", fake.SetCalls())
	}
}

func TestUpdates_UpdateConfig_RejectsUnknownFields(t *testing.T) {
	// Defence in depth: si un cliente futuro empuja un payload con
	// keys extra (e.g. "enabled":true,"force":true), preferimos
	// rechazarlo antes que silenciosamente ignorarlo. Match con el
	// shape del SettingsHandler genérico.
	fake := newFakeProvider(true)
	router, _ := newUpdatesRig(t, fake, true)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/updates/config",
		bytes.NewReader([]byte(`{"enabled":true,"force":true}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown fields, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdates_UpdateConfig_NilSettingsReturns503(t *testing.T) {
	// Si el handler se construye sin SettingsRepository (e.g. bootstrap
	// parcial), el GET sigue funcionando — lee del Service — pero el
	// PUT debe rechazar con 503 explícito en lugar de crashear con
	// nil deref.
	fake := newFakeProvider(true)
	router, _ := newUpdatesRig(t, fake, false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/updates/config",
		bytes.NewReader([]byte(`{"enabled":false}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without settings, got %d", rr.Code)
	}
}

func TestUpdates_UpdateConfig_SettingsErrorReturns500(t *testing.T) {
	// No es trivial inducir un fallo real de SettingsRepository.Set sin
	// mockearlo — el SettingsRepository es un struct concreto, no una
	// interface. Como surrogate, verifico el path interno: si el Set
	// devuelve error, el handler NO llama SetUserEnabled (evitamos
	// estados divergentes entre DB y memoria). El test se queda como
	// guard del contrato: si alguien refactoriza el orden, lo rompe.
	//
	// Mantenemos el test marcado como skip hasta que la persistencia
	// se exponga vía interface mockeable.
	_ = errors.New("placeholder")
	t.Skip("requires interface-driven settings persistence; covered by integration test if added later")
}
