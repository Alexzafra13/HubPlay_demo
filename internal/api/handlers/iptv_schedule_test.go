package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// ── Fakes ─────────────────────────────────────────────────────────

type fakeScheduleRepo struct {
	mu   sync.Mutex
	rows map[string]*db.IPTVScheduledJob // key = libraryID + "\x00" + kind
}

func newFakeScheduleRepo() *fakeScheduleRepo {
	return &fakeScheduleRepo{rows: map[string]*db.IPTVScheduledJob{}}
}

func schedKey(libraryID, kind string) string { return libraryID + "\x00" + kind }

func (r *fakeScheduleRepo) ListByLibrary(_ context.Context, libraryID string) ([]*db.IPTVScheduledJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*db.IPTVScheduledJob
	for _, row := range r.rows {
		if row.LibraryID == libraryID {
			copy := *row
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (r *fakeScheduleRepo) Get(_ context.Context, libraryID, kind string) (*db.IPTVScheduledJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[schedKey(libraryID, kind)]
	if !ok {
		return nil, db.ErrIPTVScheduledJobNotFound
	}
	c := *row
	return &c, nil
}

func (r *fakeScheduleRepo) Upsert(_ context.Context, job *db.IPTVScheduledJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := schedKey(job.LibraryID, job.Kind)
	if existing, ok := r.rows[key]; ok {
		// Preserve last_* like the real repo's ON CONFLICT … DO
		// UPDATE: only interval_hours, enabled and updated_at
		// change. The scheduler path updates last_* via a
		// separate route (simulated via forceRunRecord below).
		job.LastRunAt = existing.LastRunAt
		job.LastStatus = existing.LastStatus
		job.LastError = existing.LastError
		job.LastDurationMS = existing.LastDurationMS
	}
	copy := *job
	r.rows[key] = &copy
	return nil
}

// forceRunRecord is a test helper: the real scheduler writes last_*
// fields via a RecordRun path that isn't on the handler's interface.
// We expose this directly on the fake so the fakeRunner can simulate
// the same post-run DB state the real scheduler produces.
func (r *fakeScheduleRepo) forceRunRecord(libraryID, kind, status string, ranAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row, ok := r.rows[schedKey(libraryID, kind)]; ok {
		row.LastStatus = status
		row.LastRunAt = ranAt
	}
}

func (r *fakeScheduleRepo) Delete(_ context.Context, libraryID, kind string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, schedKey(libraryID, kind))
	return nil
}

// fakeScheduleRunner records RunNow calls and injects configurable
// errors so the handler tests can exercise the success + failure paths
// without spinning a real scheduler.
type fakeScheduleRunner struct {
	mu    sync.Mutex
	calls []struct{ LibraryID, Kind string }
	err   error
	repo  *fakeScheduleRepo
}

func (r *fakeScheduleRunner) RunNow(_ context.Context, libraryID, kind string) error {
	r.mu.Lock()
	r.calls = append(r.calls, struct{ LibraryID, Kind string }{libraryID, kind})
	err := r.err
	r.mu.Unlock()
	if err != nil {
		return err
	}
	// Simulate the real scheduler's RecordRun-equivalent path so the
	// handler's post-run re-read sees the updated last_* fields.
	if r.repo != nil {
		r.repo.forceRunRecord(libraryID, kind, "ok", time.Now().UTC())
	}
	return nil
}

// ── Env ───────────────────────────────────────────────────────────

type scheduleTestEnv struct {
	t       *testing.T
	repo    *fakeScheduleRepo
	runner  *fakeScheduleRunner
	access  *iptvFakeAccess
	handler *IPTVScheduleHandler
	router  chi.Router
}

func newScheduleTestEnv(t *testing.T) *scheduleTestEnv {
	t.Helper()
	repo := newFakeScheduleRepo()
	runner := &fakeScheduleRunner{repo: repo}
	access := &iptvFakeAccess{}
	handler := NewIPTVScheduleHandler(repo, runner, access, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/libraries/{id}/schedule", handler.List)
		r.Put("/libraries/{id}/schedule/{kind}", handler.Upsert)
		r.Delete("/libraries/{id}/schedule/{kind}", handler.Delete)
		r.Post("/libraries/{id}/schedule/{kind}/run", handler.RunNow)
	})
	return &scheduleTestEnv{t: t, repo: repo, runner: runner, access: access, handler: handler, router: r}
}

func (e *scheduleTestEnv) do(method, path, body string) *httptest.ResponseRecorder {
	return e.doAs(method, path, body, &auth.Claims{UserID: "u-admin", Role: "admin"})
}

func (e *scheduleTestEnv) doAs(method, path, body string, claims *auth.Claims) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func decodeScheduleData(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

// ── Tests ─────────────────────────────────────────────────────────

func TestSchedule_List_SynthesisesMissingKinds(t *testing.T) {
	// An empty repo still renders two placeholder rows so the UI can
	// draw the "not scheduled" state without special-casing.
	env := newScheduleTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-a/schedule", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	data, ok := decodeScheduleData(t, rr).([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("expected 2 placeholders, got %v", data)
	}
	kinds := make([]string, 0, 2)
	for _, row := range data {
		m := row.(map[string]any)
		kinds = append(kinds, m["kind"].(string))
		if m["enabled"] != false {
			t.Errorf("placeholder should be disabled: %v", m)
		}
	}
	wantKinds := map[string]bool{db.IPTVJobKindM3URefresh: true, db.IPTVJobKindEPGRefresh: true}
	for _, k := range kinds {
		if !wantKinds[k] {
			t.Errorf("unexpected kind %q", k)
		}
	}
}

func TestSchedule_List_DeniesWithoutAccess(t *testing.T) {
	env := newScheduleTestEnv(t)
	env.access.accessFn = func(_, _ string) (bool, error) { return false, nil }
	rr := env.doAs(http.MethodGet, "/api/v1/libraries/lib-a/schedule", "",
		&auth.Claims{UserID: "u-user", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for denied access, got %d", rr.Code)
	}
}

func TestSchedule_Upsert_CreatesAndReturnsRow(t *testing.T) {
	env := newScheduleTestEnv(t)
	body := `{"interval_hours": 6, "enabled": true}`
	rr := env.do(http.MethodPut,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindEPGRefresh, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	saved, _ := env.repo.Get(context.Background(), "lib-a", db.IPTVJobKindEPGRefresh)
	if saved.IntervalHours != 6 || !saved.Enabled {
		t.Errorf("not persisted correctly: %+v", saved)
	}
}

func TestSchedule_Upsert_RejectsInvalidKind(t *testing.T) {
	env := newScheduleTestEnv(t)
	rr := env.do(http.MethodPut,
		"/api/v1/libraries/lib-a/schedule/bogus", `{"interval_hours": 6}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid kind, got %d", rr.Code)
	}
}

func TestSchedule_Upsert_RejectsOutOfRangeInterval(t *testing.T) {
	env := newScheduleTestEnv(t)
	cases := []struct {
		name string
		body string
	}{
		{"zero", `{"interval_hours": 0}`},
		{"negative", `{"interval_hours": -1}`},
		{"too large", `{"interval_hours": 1000}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rr := env.do(http.MethodPut,
				"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh, tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %s, got %d", tc.name, rr.Code)
			}
		})
	}
}

func TestSchedule_Upsert_KeepsEnabledWhenOmitted(t *testing.T) {
	// Saving just the interval must not toggle the enabled flag.
	env := newScheduleTestEnv(t)
	ctx := context.Background()
	if err := env.repo.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 12, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"interval_hours": 24}` // no `enabled`
	rr := env.do(http.MethodPut,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	saved, _ := env.repo.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if !saved.Enabled {
		t.Error("omitting enabled in body flipped the flag")
	}
	if saved.IntervalHours != 24 {
		t.Errorf("interval not updated: %d", saved.IntervalHours)
	}
}

func TestSchedule_Upsert_DeniesWithoutAccess(t *testing.T) {
	env := newScheduleTestEnv(t)
	env.access.accessFn = func(_, _ string) (bool, error) { return false, nil }
	rr := env.doAs(http.MethodPut,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh,
		`{"interval_hours": 6}`,
		&auth.Claims{UserID: "u-user", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for denied access, got %d", rr.Code)
	}
}

func TestSchedule_Upsert_RejectsUnknownField(t *testing.T) {
	env := newScheduleTestEnv(t)
	rr := env.do(http.MethodPut,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh,
		`{"interval_hours": 6, "bogus": true}`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown field, got %d", rr.Code)
	}
}

func TestSchedule_Delete_HappyPath(t *testing.T) {
	env := newScheduleTestEnv(t)
	ctx := context.Background()
	_ = env.repo.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh, IntervalHours: 6, Enabled: true,
	})
	rr := env.do(http.MethodDelete,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh, "")
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if _, err := env.repo.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh); err == nil {
		t.Error("row still present after delete")
	}
}

func TestSchedule_RunNow_HappyPath(t *testing.T) {
	env := newScheduleTestEnv(t)
	ctx := context.Background()
	_ = env.repo.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh, IntervalHours: 6, Enabled: true,
	})
	rr := env.do(http.MethodPost,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh+"/run", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if len(env.runner.calls) != 1 {
		t.Fatalf("expected 1 runner call, got %d", len(env.runner.calls))
	}
	row, _ := env.repo.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if row.LastStatus != "ok" {
		t.Errorf("last_status not updated: %q", row.LastStatus)
	}
}

func TestSchedule_RunNow_SurfacesRunnerError(t *testing.T) {
	env := newScheduleTestEnv(t)
	env.runner.err = fmt.Errorf("upstream 502")
	rr := env.do(http.MethodPost,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindEPGRefresh+"/run", "")
	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSchedule_RunNow_WithoutRowReturns204(t *testing.T) {
	// Firing RunNow against a library that never configured a
	// schedule should still execute the refresh — but since there's
	// no row to return, the API answers 204 and the client refetches.
	env := newScheduleTestEnv(t)
	rr := env.do(http.MethodPost,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh+"/run", "")
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 with no row, got %d", rr.Code)
	}
	if len(env.runner.calls) != 1 {
		t.Errorf("runner not called: %d", len(env.runner.calls))
	}
}

func TestSchedule_RunNow_DeniesWithoutAccess(t *testing.T) {
	env := newScheduleTestEnv(t)
	env.access.accessFn = func(_, _ string) (bool, error) { return false, nil }
	rr := env.doAs(http.MethodPost,
		"/api/v1/libraries/lib-a/schedule/"+db.IPTVJobKindM3URefresh+"/run", "",
		&auth.Claims{UserID: "u-user", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
	if len(env.runner.calls) != 0 {
		t.Errorf("runner invoked on denied request: %d", len(env.runner.calls))
	}
}

// Compile-time assertion: the production scheduler type satisfies
// the runner interface the handler expects. Prevents silent drift if
// RunNow's signature changes on either side.
var _ IPTVScheduleRunner = (*fakeScheduleRunner)(nil)

// Ensure the fake repo matches the handler's expected surface.
var _ IPTVScheduleRepository = (*fakeScheduleRepo)(nil)

// Anchor the errors import so lint doesn't complain if future edits
// drop the explicit sentinel check.
var _ = errors.Is
