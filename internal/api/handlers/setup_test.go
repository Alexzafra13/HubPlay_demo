package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/config"
	providermodel "hubplay/internal/provider/model"
	"hubplay/internal/domain"
	"hubplay/internal/library"
	"hubplay/internal/setup"
	"hubplay/internal/testutil"
)

// ─── Fakes ──────────────────────────────────────────────────────────────────
//
// SetupService fake with field overrides. Reuses mockAuthService (auth_test.go),
// userFakeService (users_test.go), libFakeService (library_test.go),
// providersFakeRepo (providers_test.go).

type setupFakeService struct {
	needsSetupFn  func(ctx context.Context) bool
	browseFn      func(path string) (*setup.BrowseResult, error)
	capsFn        func() *setup.SystemCapabilities
	completeFn    func(startScan bool) error
}

func (s *setupFakeService) NeedsSetup(ctx context.Context) bool {
	if s.needsSetupFn != nil {
		return s.needsSetupFn(ctx)
	}
	return true
}

func (s *setupFakeService) BrowseDirectories(path string) (*setup.BrowseResult, error) {
	if s.browseFn != nil {
		return s.browseFn(path)
	}
	return &setup.BrowseResult{Current: path}, nil
}

func (s *setupFakeService) DetectCapabilities() *setup.SystemCapabilities {
	if s.capsFn != nil {
		return s.capsFn()
	}
	return &setup.SystemCapabilities{FFmpegFound: false}
}

func (s *setupFakeService) CompleteSetup(startScan bool) error {
	if s.completeFn != nil {
		return s.completeFn(startScan)
	}
	return nil
}

var _ SetupService = (*setupFakeService)(nil)

// ─── Env ────────────────────────────────────────────────────────────────────

type setupTestEnv struct {
	t         *testing.T
	setup     *setupFakeService
	auth      *mockAuthService // from auth_test.go
	libs      *libFakeService
	users     *userFakeService
	providers *providersFakeRepo
	handler   *SetupHandler
	router    chi.Router
}

func newSetupTestEnv(t *testing.T) *setupTestEnv {
	t.Helper()
	env := &setupTestEnv{
		t:         t,
		setup:     &setupFakeService{},
		auth:      &mockAuthService{},
		libs:      &libFakeService{},
		users:     &userFakeService{},
		providers: &providersFakeRepo{getByName: map[string]*providermodel.ProviderConfig{}},
	}
	env.handler = NewSetupHandler(SetupHandlerConfig{
		Setup:     env.setup,
		Auth:      env.auth,
		Libraries: env.libs,
		Users:     env.users,
		Providers: env.providers,
		Config:    &config.Config{},
		Logger:    testutil.NopLogger(),
	})

	r := chi.NewRouter()
	r.Route("/api/v1/setup", func(r chi.Router) {
		r.Get("/status", env.handler.Status)
		r.Get("/browse", env.handler.Browse)
		r.Post("/libraries", env.handler.CreateLibraries)
		r.Put("/settings", env.handler.UpdateSettings)
		r.Get("/capabilities", env.handler.Capabilities)
		r.Post("/complete", env.handler.Complete)
	})
	env.router = r
	return env
}

func (e *setupTestEnv) do(method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func setupDecodeData(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d, _ := out["data"].(map[string]any)
	return d
}

// ─── Status ─────────────────────────────────────────────────────────────────

func TestSetupHandler_Status_AlreadyComplete(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(_ context.Context) bool { return false }
	rr := env.do(http.MethodGet, "/api/v1/setup/status", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	d := setupDecodeData(t, rr)
	if d["needs_setup"] != false || d["current_step"] != "" {
		t.Errorf("shape: %v", d)
	}
}

func TestSetupHandler_Status_NoUsers_AccountStep(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.users.countFn = func(_ context.Context) (int, error) { return 0, nil }
	rr := env.do(http.MethodGet, "/api/v1/setup/status", "")
	d := setupDecodeData(t, rr)
	if d["current_step"] != "account" {
		t.Errorf("step: %v", d["current_step"])
	}
}

func TestSetupHandler_Status_UserButNoLibs_LibrariesStep(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.users.countFn = func(_ context.Context) (int, error) { return 1, nil }
	env.libs.listFn = func(_ context.Context) ([]*librarymodel.Library, error) { return nil, nil }
	rr := env.do(http.MethodGet, "/api/v1/setup/status", "")
	d := setupDecodeData(t, rr)
	if d["current_step"] != "libraries" {
		t.Errorf("step: %v", d["current_step"])
	}
}

func TestSetupHandler_Status_LibsExist_SettingsStep(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.users.countFn = func(_ context.Context) (int, error) { return 1, nil }
	env.libs.listFn = func(_ context.Context) ([]*librarymodel.Library, error) {
		return []*librarymodel.Library{{ID: "lib-1"}}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/setup/status", "")
	d := setupDecodeData(t, rr)
	if d["current_step"] != "settings" {
		t.Errorf("step: %v", d["current_step"])
	}
}

// ─── Browse ─────────────────────────────────────────────────────────────────

func TestSetupHandler_Browse_ServiceError_400(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.browseFn = func(_ string) (*setup.BrowseResult, error) {
		return nil, errors.New("permission denied")
	}
	rr := env.do(http.MethodGet, "/api/v1/setup/browse?path=/secret", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_Browse_DefaultsRootPath(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	var gotPath string
	env.setup.browseFn = func(p string) (*setup.BrowseResult, error) {
		gotPath = p
		return &setup.BrowseResult{Current: p}, nil
	}
	_ = env.do(http.MethodGet, "/api/v1/setup/browse", "")
	if gotPath != "/" {
		t.Errorf("empty path should default to '/', got %q", gotPath)
	}
}

func TestSetupHandler_Browse_SetsCacheControl(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.browseFn = func(p string) (*setup.BrowseResult, error) {
		return &setup.BrowseResult{Current: p}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/setup/browse?path=/", "")
	if got := rr.Header().Get("Cache-Control"); got != "private, max-age=30" {
		t.Errorf("Cache-Control = %q", got)
	}
}

// ─── CreateLibraries ────────────────────────────────────────────────────────

func TestSetupHandler_CreateLibraries_Empty_400(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/setup/libraries", `{"libraries":[]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_CreateLibraries_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/setup/libraries", `{bogus`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_CreateLibraries_Happy_CreatesEach(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	var created []library.CreateRequest
	env.libs.createFn = func(_ context.Context, req library.CreateRequest) (*librarymodel.Library, error) {
		created = append(created, req)
		return &librarymodel.Library{ID: "new-" + req.Name}, nil
	}
	body := `{"libraries":[
		{"name":"Movies","content_type":"movies","paths":["/m"]},
		{"name":"Shows","content_type":"shows","paths":["/s"]}
	]}`
	rr := env.do(http.MethodPost, "/api/v1/setup/libraries", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d, body: %s", rr.Code, rr.Body.String())
	}
	if len(created) != 2 {
		t.Errorf("libraries created: %d", len(created))
	}
}

func TestSetupHandler_CreateLibraries_ServiceError_StopsAtFailure(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	calls := 0
	env.libs.createFn = func(_ context.Context, _ library.CreateRequest) (*librarymodel.Library, error) {
		calls++
		if calls == 2 {
			return nil, domain.NewValidation(nil)
		}
		return &librarymodel.Library{ID: "ok"}, nil
	}
	body := `{"libraries":[
		{"name":"A"},{"name":"B"},{"name":"C"}
	]}`
	rr := env.do(http.MethodPost, "/api/v1/setup/libraries", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
	if calls != 2 {
		t.Errorf("expected to stop at 2 calls, made %d", calls)
	}
}

// ─── UpdateSettings ─────────────────────────────────────────────────────────

func TestSetupHandler_UpdateSettings_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/setup/settings", `{`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_UpdateSettings_NoAPIKey_OK(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	body := `{"transcoding_enabled":true,"hw_accel":"none"}`
	rr := env.do(http.MethodPut, "/api/v1/setup/settings", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	// No provider upsert should have happened.
	if len(env.providers.upserted) != 0 {
		t.Errorf("should not upsert provider without API key, got %d calls", len(env.providers.upserted))
	}
}

func TestSetupHandler_UpdateSettings_PersistsTMDBKey(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	body := `{"tmdb_api_key":"my-key","transcoding_enabled":false}`
	rr := env.do(http.MethodPut, "/api/v1/setup/settings", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.providers.upserted) != 1 {
		t.Fatalf("upsert calls: %d", len(env.providers.upserted))
	}
	cfg := env.providers.upserted[0]
	if cfg.Name != "tmdb" || cfg.APIKey != "my-key" {
		t.Errorf("upserted: %+v", cfg)
	}
}

func TestSetupHandler_UpdateSettings_UpsertError_500(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.providers.upsertErr = errors.New("write fail")
	rr := env.do(http.MethodPut, "/api/v1/setup/settings", `{"tmdb_api_key":"k"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Capabilities ───────────────────────────────────────────────────────────

func TestSetupHandler_Capabilities_ReturnsWhatServiceReports(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.capsFn = func() *setup.SystemCapabilities {
		return &setup.SystemCapabilities{
			FFmpegPath: "/usr/bin/ffmpeg", FFmpegFound: true,
			HWAccels: []string{"vaapi", "nvenc"},
		}
	}
	rr := env.do(http.MethodGet, "/api/v1/setup/capabilities", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	d := setupDecodeData(t, rr)
	if d["ffmpeg_found"] != true || d["ffmpeg_path"] != "/usr/bin/ffmpeg" {
		t.Errorf("ffmpeg: %v / %v", d["ffmpeg_found"], d["ffmpeg_path"])
	}
	accels, _ := d["hw_accels"].([]any)
	if len(accels) != 2 {
		t.Errorf("hw_accels: %v", d["hw_accels"])
	}
}

// ─── Complete ───────────────────────────────────────────────────────────────

func TestSetupHandler_Complete_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/setup/complete", `{`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_Complete_ServiceError_500(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.completeFn = func(_ bool) error { return errors.New("boom") }
	rr := env.do(http.MethodPost, "/api/v1/setup/complete", `{"start_scan":false}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestSetupHandler_Complete_PropagatesStartScan(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	var got bool
	env.setup.completeFn = func(scan bool) error { got = scan; return nil }
	rr := env.do(http.MethodPost, "/api/v1/setup/complete", `{"start_scan":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if !got {
		t.Errorf("start_scan not propagated")
	}
}

// ─── Post-completion guard ─────────────────────────────────────────────────
//
// Once the wizard has been completed, the mutating + filesystem-snooping
// endpoints must refuse traffic. Before the guard, /setup/browse stayed
// open as an unauthenticated filesystem listing forever (root-level
// blocklist only — /home, /srv, /var/lib, mounted media were all
// browsable). /setup/status keeps responding because the SPA hits it
// on boot to know whether to redirect to the wizard, and it doesn't
// leak anything.

func TestSetupHandler_PostCompletion_Browse_403(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodGet, "/api/v1/setup/browse?path=/", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "SETUP_COMPLETE") {
		t.Errorf("expected SETUP_COMPLETE error code, body=%s", rr.Body.String())
	}
}

func TestSetupHandler_PostCompletion_Capabilities_403(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodGet, "/api/v1/setup/capabilities", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", rr.Code)
	}
}

func TestSetupHandler_PostCompletion_CreateLibraries_403(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodPost, "/api/v1/setup/libraries",
		`{"libraries":[{"name":"x","content_type":"movie","paths":["/m"]}]}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", rr.Code)
	}
}

func TestSetupHandler_PostCompletion_UpdateSettings_403(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodPut, "/api/v1/setup/settings", `{}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", rr.Code)
	}
}

func TestSetupHandler_PostCompletion_Complete_403(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodPost, "/api/v1/setup/complete", `{"start_scan":false}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", rr.Code)
	}
}

// Status remains open because the SPA polls it on boot. Pin the
// carve-out so a future "lock everything when needs_setup=false"
// refactor doesn't accidentally break the redirect logic.
func TestSetupHandler_PostCompletion_Status_StillOpen(t *testing.T) {
	t.Parallel()
	env := newSetupTestEnv(t)
	env.setup.needsSetupFn = func(context.Context) bool { return false }
	rr := env.do(http.MethodGet, "/api/v1/setup/status", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
}
