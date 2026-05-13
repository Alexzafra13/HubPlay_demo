package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/config"
)

// timeoutAfter returns a channel that closes after a generous bound.
// Kept locally rather than on package scope because the rest of the
// test files in this directory use ad-hoc sleeps or signal channels.
func timeoutAfter(t *testing.T, what string) <-chan time.Time {
	t.Helper()
	return time.After(2 * time.Second)
}

// fakeDBSaver captures the persistence callback the admin handler
// drives. Used as both a happy-path stub and to assert that Test
// calls do NOT touch the saver (a defence-in-depth check against
// regression that would persist a candidate before /test confirms
// it).
type fakeDBSaver struct {
	mu    sync.Mutex
	calls []fakeDBSaveCall
	err   error
}

type fakeDBSaveCall struct {
	Driver string
	Path   string
	DSN    string
}

func (f *fakeDBSaver) Save(driver, path, dsn string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeDBSaveCall{driver, path, dsn})
	return f.err
}

func (f *fakeDBSaver) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestAdminDBHandler(t *testing.T) (*handlers.AdminDBHandler, *fakeDBSaver, *config.RestartRequester, chan struct{}) {
	t.Helper()
	saver := &fakeDBSaver{}
	cancelled := make(chan struct{}, 1)
	cancel := func() { cancelled <- struct{}{} }
	restart := config.NewRestartRequester(cancel, newSilentLogger())
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.Path = t.TempDir() + "/hubplay.db"
	h := handlers.NewAdminDBHandler(
		cfg, t.TempDir()+"/hubplay.yaml", nil, saver.Save, restart, newSilentLogger(),
	)
	return h, saver, restart, cancelled
}

func TestAdminDB_Status_SQLite(t *testing.T) {
	h, _, _, _ := newTestAdminDBHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/system/db", nil)
	h.Status(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data struct {
			Driver string `json:"driver"`
			Path   string `json:"path"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.Driver != "sqlite" {
		t.Errorf("driver = %q, want sqlite", body.Data.Driver)
	}
	if !strings.HasSuffix(body.Data.Path, "hubplay.db") {
		t.Errorf("path = %q does not end in hubplay.db", body.Data.Path)
	}
}

func TestAdminDB_Test_RejectsUnknownDriver(t *testing.T) {
	h, saver, _, _ := newTestAdminDBHandler(t)
	body := bytes.NewBufferString(`{"driver":"mysql"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/system/db/test", body)
	h.Test(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp struct {
		Data struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.OK {
		t.Errorf("ok = true on unknown driver")
	}
	if !strings.Contains(resp.Data.Error, "driver") {
		t.Errorf("error = %q, want mention of driver", resp.Data.Error)
	}
	if saver.CallCount() != 0 {
		t.Errorf("Test should not invoke saver, got %d calls", saver.CallCount())
	}
}

func TestAdminDB_Test_PingsSQLite(t *testing.T) {
	// Open + ping :memory: as a smoke that the test path actually
	// drives the db.Open dispatcher end-to-end. Hits the same code
	// path the panel's "Test" button does.
	h, _, _, _ := newTestAdminDBHandler(t)
	body := bytes.NewBufferString(`{"driver":"sqlite","path":":memory:"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/system/db/test", body)
	h.Test(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			OK            bool   `json:"ok"`
			ServerVersion string `json:"server_version"`
			Error         string `json:"error"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Data.OK {
		t.Fatalf("ok = false, error=%q", resp.Data.Error)
	}
	if resp.Data.ServerVersion == "" {
		t.Errorf("expected a version string, got empty")
	}
}

func TestAdminDB_Save_PersistsAndDefersRestart(t *testing.T) {
	h, saver, _, cancelled := newTestAdminDBHandler(t)

	body := bytes.NewBufferString(`{"driver":"postgres","dsn":"postgres://u:p@host:5432/db?sslmode=disable","restart":false}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/db", body)
	h.Save(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if saver.CallCount() != 1 {
		t.Fatalf("saver called %d times, want 1", saver.CallCount())
	}
	if saver.calls[0].Driver != "postgres" {
		t.Errorf("driver = %q", saver.calls[0].Driver)
	}
	select {
	case <-cancelled:
		t.Errorf("Save with restart=false should not cancel")
	default:
	}
}

func TestAdminDB_Save_TriggersRestartWhenRequested(t *testing.T) {
	h, _, _, cancelled := newTestAdminDBHandler(t)
	body := bytes.NewBufferString(`{"driver":"postgres","dsn":"postgres://u:p@h/d","restart":true}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/admin/system/db", body)
	h.Save(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	// RestartRequester's delay is ~100 ms by default; wait a touch.
	select {
	case <-cancelled:
	case <-timeoutAfter(t, "restart cancel"):
		t.Errorf("restart was not scheduled")
	}
}

func TestAdminDB_Save_RejectsMissingFields(t *testing.T) {
	h, saver, _, _ := newTestAdminDBHandler(t)
	cases := []struct{ name, body string }{
		{"missing driver", `{}`},
		{"sqlite without path", `{"driver":"sqlite"}`},
		{"postgres without dsn", `{"driver":"postgres"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/admin/system/db", bytes.NewBufferString(c.body))
			h.Save(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rr.Code)
			}
		})
	}
	if saver.CallCount() != 0 {
		t.Errorf("saver should never be called on rejection, got %d", saver.CallCount())
	}
}

func TestAdminDB_Profiles_NoBundledByDefault(t *testing.T) {
	t.Setenv("HUBPLAY_POSTGRES_BUNDLED_DSN", "")
	h, _, _, _ := newTestAdminDBHandler(t)

	rr := httptest.NewRecorder()
	h.Profiles(rr, httptest.NewRequest(http.MethodGet, "/admin/system/db/profiles", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body struct {
		Data struct {
			BundledPostgres bool   `json:"bundled_postgres"`
			BundledLabel    string `json:"bundled_label"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.BundledPostgres {
		t.Errorf("bundled_postgres should be false when env is unset")
	}
}

func TestAdminDB_Profiles_BundledWhenEnvSet(t *testing.T) {
	t.Setenv("HUBPLAY_POSTGRES_BUNDLED_DSN", "postgres://hubplay:hubplay@db:5432/hubplay?sslmode=disable")
	h, _, _, _ := newTestAdminDBHandler(t)

	rr := httptest.NewRecorder()
	h.Profiles(rr, httptest.NewRequest(http.MethodGet, "/admin/system/db/profiles", nil))

	var body struct {
		Data struct {
			BundledPostgres bool   `json:"bundled_postgres"`
			BundledLabel    string `json:"bundled_label"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Data.BundledPostgres {
		t.Errorf("bundled_postgres should be true with env set")
	}
	if body.Data.BundledLabel == "" {
		t.Errorf("expected a non-empty bundled_label")
	}
	// Defence-in-depth: the response must NOT leak the DSN (it
	// carries the password, even though it lives on an internal
	// network). The body should only signal availability.
	if strings.Contains(rr.Body.String(), "hubplay@db") {
		t.Errorf("response leaks the bundled DSN userinfo: %s", rr.Body.String())
	}
}

func TestAdminDB_Save_UseBundledFallsBackToEnv(t *testing.T) {
	t.Setenv("HUBPLAY_POSTGRES_BUNDLED_DSN", "postgres://hubplay:hubplay@db:5432/hubplay?sslmode=disable")
	h, saver, _, _ := newTestAdminDBHandler(t)

	body := bytes.NewBufferString(`{"driver":"postgres","use_bundled":true,"restart":false}`)
	rr := httptest.NewRecorder()
	h.Save(rr, httptest.NewRequest(http.MethodPut, "/admin/system/db", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	if saver.CallCount() != 1 {
		t.Fatalf("saver called %d times, want 1", saver.CallCount())
	}
	if saver.calls[0].DSN == "" {
		t.Errorf("DSN should have been substituted from env, got empty")
	}
	if !strings.Contains(saver.calls[0].DSN, "hubplay@db") {
		t.Errorf("DSN does not match env: %q", saver.calls[0].DSN)
	}
}

func TestAdminDB_Save_UseBundledWithoutEnvRejects(t *testing.T) {
	t.Setenv("HUBPLAY_POSTGRES_BUNDLED_DSN", "")
	h, saver, _, _ := newTestAdminDBHandler(t)

	body := bytes.NewBufferString(`{"driver":"postgres","use_bundled":true}`)
	rr := httptest.NewRecorder()
	h.Save(rr, httptest.NewRequest(http.MethodPut, "/admin/system/db", body))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if saver.CallCount() != 0 {
		t.Errorf("saver should not be invoked, got %d calls", saver.CallCount())
	}
}

func TestAdminDB_Restart_OneShot(t *testing.T) {
	h, _, _, cancelled := newTestAdminDBHandler(t)
	rr := httptest.NewRecorder()
	h.Restart(rr, httptest.NewRequest(http.MethodPost, "/admin/system/restart", nil))
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rr.Code)
	}
	// Second click is a no-op (one-shot).
	rr2 := httptest.NewRecorder()
	h.Restart(rr2, httptest.NewRequest(http.MethodPost, "/admin/system/restart", nil))
	if rr2.Code != http.StatusAccepted {
		t.Errorf("second click status = %d, want 202", rr2.Code)
	}

	// Exactly one cancel.
	got := 0
	for {
		select {
		case <-cancelled:
			got++
		case <-timeoutAfter(t, "drain cancel"):
			if got != 1 {
				t.Errorf("cancel invoked %d times, want 1", got)
			}
			return
		}
	}
}
