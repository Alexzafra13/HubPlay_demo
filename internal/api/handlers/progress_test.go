package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/testutil"
)

// ─── Fake UserDataRepository ────────────────────────────────────────────────

type progressFakeUserData struct {
	mu   sync.Mutex
	data map[string]*db.UserData // key: "userID:itemID"

	// Optional overrides for the list-returning methods; nil = use default
	// behaviour (return an empty slice).
	continueFn func(ctx context.Context, userID string, limit int) ([]*db.ContinueWatchingItem, error)
	favsFn     func(ctx context.Context, userID string, limit, offset int) ([]*db.FavoriteItem, error)
	nextUpFn   func(ctx context.Context, userID string, limit int) ([]*db.NextUpItem, error)

	// Optional failure injection for write methods.
	failGet      bool
	failUpdate   bool
	failMark     bool
	failFavorite bool
	failDelete   bool
}

func newProgressFakeUserData() *progressFakeUserData {
	return &progressFakeUserData{data: map[string]*db.UserData{}}
}

func keyUD(userID, itemID string) string { return userID + ":" + itemID }

func (r *progressFakeUserData) Get(_ context.Context, userID, itemID string) (*db.UserData, error) {
	if r.failGet {
		return nil, errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ud, ok := r.data[keyUD(userID, itemID)]
	if !ok {
		return nil, nil // intentional: handler treats nil as "no record yet"
	}
	cp := *ud
	return &cp, nil
}

func (r *progressFakeUserData) GetBatch(_ context.Context, userID string, itemIDs []string) (map[string]*db.UserData, error) {
	if r.failGet {
		return nil, errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]*db.UserData, len(itemIDs))
	for _, id := range itemIDs {
		if ud, ok := r.data[keyUD(userID, id)]; ok {
			cp := *ud
			out[id] = &cp
		}
	}
	return out, nil
}

func (r *progressFakeUserData) UpdateProgress(_ context.Context, userID, itemID string, pos int64, completed bool) error {
	if r.failUpdate {
		return errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := keyUD(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &db.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.PositionTicks = pos
	ud.Completed = completed
	now := time.Now()
	ud.LastPlayedAt = &now
	return nil
}

func (r *progressFakeUserData) MarkPlayed(_ context.Context, userID, itemID string) error {
	if r.failMark {
		return errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := keyUD(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &db.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.Completed = true
	ud.PlayCount++
	return nil
}

func (r *progressFakeUserData) SetFavorite(_ context.Context, userID, itemID string, fav bool) error {
	if r.failFavorite {
		return errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := keyUD(userID, itemID)
	ud, ok := r.data[k]
	if !ok {
		ud = &db.UserData{UserID: userID, ItemID: itemID}
		r.data[k] = ud
	}
	ud.IsFavorite = fav
	return nil
}

func (r *progressFakeUserData) ContinueWatching(ctx context.Context, userID string, limit int) ([]*db.ContinueWatchingItem, error) {
	if r.continueFn != nil {
		return r.continueFn(ctx, userID, limit)
	}
	return nil, nil
}

func (r *progressFakeUserData) Favorites(ctx context.Context, userID string, limit, offset int) ([]*db.FavoriteItem, error) {
	if r.favsFn != nil {
		return r.favsFn(ctx, userID, limit, offset)
	}
	return nil, nil
}

func (r *progressFakeUserData) NextUp(ctx context.Context, userID string, limit int) ([]*db.NextUpItem, error) {
	if r.nextUpFn != nil {
		return r.nextUpFn(ctx, userID, limit)
	}
	return nil, nil
}

func (r *progressFakeUserData) SeriesEpisodeProgress(_ context.Context, _, _ string) (int, int, error) {
	return 0, 0, nil
}

func (r *progressFakeUserData) Delete(_ context.Context, userID, itemID string) error {
	if r.failDelete {
		return errors.New("boom")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, keyUD(userID, itemID))
	return nil
}

var _ UserDataRepository = (*progressFakeUserData)(nil)

// ─── Test env ───────────────────────────────────────────────────────────────

type progressTestEnv struct {
	t        *testing.T
	userData *progressFakeUserData
	images   *fakeImageRepo // reused from image_test.go
	handler  *ProgressHandler
	router   chi.Router
}

func newProgressTestEnv(t *testing.T) *progressTestEnv {
	t.Helper()
	env := &progressTestEnv{
		t:        t,
		userData: newProgressFakeUserData(),
		images:   newFakeImageRepo(),
	}
	// nil bus — these tests don't assert event publishing; the
	// dedicated me_events_test.go covers that path.
	env.handler = NewProgressHandler(env.userData, env.images, nil, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/me", func(r chi.Router) {
		r.Get("/progress/{itemId}", env.handler.GetProgress)
		r.Put("/progress/{itemId}", env.handler.UpdateProgress)
		r.Post("/progress/{itemId}/played", env.handler.MarkPlayed)
		r.Delete("/progress/{itemId}/played", env.handler.MarkUnplayed)
		r.Post("/favorite/{itemId}", env.handler.ToggleFavorite)
		r.Get("/continue-watching", env.handler.ContinueWatching)
		r.Get("/favorites", env.handler.Favorites)
		r.Get("/next-up", env.handler.NextUp)
	})
	env.router = r
	return env
}

func (e *progressTestEnv) do(method, path string, body string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	var req *http.Request
	if reqBody != nil {
		req = httptest.NewRequest(method, path, reqBody)
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

func decodeProgressData(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d, _ := out["data"].(map[string]any)
	return d
}

func defaultClaims() *auth.Claims { return &auth.Claims{UserID: "user-1", Username: "alice"} }

// ─── GetProgress ────────────────────────────────────────────────────────────

func TestProgressHandler_GetProgress_Unauthenticated(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/me/progress/item-1", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
}

func TestProgressHandler_GetProgress_EmptyReturnsZeroPayload(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/me/progress/item-1", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	d := decodeProgressData(t, rr)
	if d["item_id"] != "item-1" {
		t.Errorf("item_id: %v", d["item_id"])
	}
	// JSON numbers decode as float64; compare against 0 directly.
	if d["position_ticks"] != float64(0) {
		t.Errorf("position_ticks: %v", d["position_ticks"])
	}
	if d["completed"] != false {
		t.Errorf("completed: %v", d["completed"])
	}
	if d["is_favorite"] != false {
		t.Errorf("is_favorite: %v", d["is_favorite"])
	}
}

func TestProgressHandler_GetProgress_PopulatedReturnsState(t *testing.T) {
	env := newProgressTestEnv(t)
	env.userData.data[keyUD("user-1", "item-1")] = &db.UserData{
		UserID: "user-1", ItemID: "item-1",
		PositionTicks: 123, PlayCount: 2, Completed: true, IsFavorite: true,
	}
	rr := env.do(http.MethodGet, "/api/v1/me/progress/item-1", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	d := decodeProgressData(t, rr)
	if d["position_ticks"] != float64(123) {
		t.Errorf("position_ticks: %v", d["position_ticks"])
	}
	if d["play_count"] != float64(2) {
		t.Errorf("play_count: %v", d["play_count"])
	}
	if d["completed"] != true {
		t.Errorf("completed: %v", d["completed"])
	}
	if d["is_favorite"] != true {
		t.Errorf("is_favorite: %v", d["is_favorite"])
	}
}

func TestProgressHandler_GetProgress_RepoError_500(t *testing.T) {
	env := newProgressTestEnv(t)
	env.userData.failGet = true
	rr := env.do(http.MethodGet, "/api/v1/me/progress/item-1", "", defaultClaims())
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", rr.Code)
	}
}

// ─── UpdateProgress ─────────────────────────────────────────────────────────

func TestProgressHandler_UpdateProgress_WritesState(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/me/progress/item-1",
		`{"position_ticks": 5000, "completed": false}`, defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rr.Code)
	}
	ud := env.userData.data[keyUD("user-1", "item-1")]
	if ud == nil {
		t.Fatal("no record written")
		return // unreachable; helps staticcheck see the nil-guard.
	}
	if ud.PositionTicks != 5000 {
		t.Errorf("position: %d", ud.PositionTicks)
	}
}

func TestProgressHandler_UpdateProgress_InvalidJSON_400(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/me/progress/item-1",
		`{not valid json`, defaultClaims())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestProgressHandler_UpdateProgress_Unauthenticated(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/me/progress/item-1",
		`{"position_ticks": 1}`, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProgressHandler_UpdateProgress_CompletedRespected(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/me/progress/item-1",
		`{"position_ticks": 100, "completed": true}`, defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	ud := env.userData.data[keyUD("user-1", "item-1")]
	if !ud.Completed {
		t.Error("completed flag not set")
	}
}

// ─── MarkPlayed / MarkUnplayed ──────────────────────────────────────────────

func TestProgressHandler_MarkPlayed_SetsCompleted(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/me/progress/item-1/played", "", defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	ud := env.userData.data[keyUD("user-1", "item-1")]
	if ud == nil || !ud.Completed || ud.PlayCount != 1 {
		t.Fatalf("state after MarkPlayed: %+v", ud)
	}
}

func TestProgressHandler_MarkUnplayed_DeletesRecord(t *testing.T) {
	env := newProgressTestEnv(t)
	env.userData.data[keyUD("user-1", "item-1")] = &db.UserData{Completed: true}
	rr := env.do(http.MethodDelete, "/api/v1/me/progress/item-1/played", "", defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if _, ok := env.userData.data[keyUD("user-1", "item-1")]; ok {
		t.Error("record not deleted")
	}
}

// ─── ToggleFavorite ─────────────────────────────────────────────────────────

func TestProgressHandler_ToggleFavorite_FirstTime_Enables(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/me/favorite/item-1", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	d := decodeProgressData(t, rr)
	if d["is_favorite"] != true {
		t.Fatalf("is_favorite: %v", d["is_favorite"])
	}
}

func TestProgressHandler_ToggleFavorite_Inverts(t *testing.T) {
	env := newProgressTestEnv(t)
	env.userData.data[keyUD("user-1", "item-1")] = &db.UserData{IsFavorite: true}
	rr := env.do(http.MethodPost, "/api/v1/me/favorite/item-1", "", defaultClaims())
	d := decodeProgressData(t, rr)
	if d["is_favorite"] != false {
		t.Fatalf("after toggle: %v", d["is_favorite"])
	}
}

// ─── ContinueWatching ───────────────────────────────────────────────────────

func TestProgressHandler_ContinueWatching_Empty(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/me/continue-watching", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	data, _ := out["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty, got %d", len(data))
	}
}

func TestProgressHandler_ContinueWatching_ShapesEntries(t *testing.T) {
	env := newProgressTestEnv(t)
	now := time.Now()
	env.userData.continueFn = func(_ context.Context, _ string, _ int) ([]*db.ContinueWatchingItem, error) {
		return []*db.ContinueWatchingItem{
			{ItemID: "it-1", PositionTicks: 100, LastPlayedAt: &now, Title: "Foo", Type: "episode", DurationTicks: 1000, ParentID: "series-1"},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/me/continue-watching?limit=5", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	data, _ := out["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("want 1 entry, got %d", len(data))
	}
	entry := data[0].(map[string]any)
	if entry["id"] != "it-1" || entry["title"] != "Foo" {
		t.Errorf("shape: %v", entry)
	}
	// Image URLs default to nil when no image exists.
	if entry["poster_url"] != nil {
		t.Errorf("poster_url should be nil, got %v", entry["poster_url"])
	}
}

func TestProgressHandler_ContinueWatching_LimitValidated(t *testing.T) {
	env := newProgressTestEnv(t)
	var gotLimit int
	env.userData.continueFn = func(_ context.Context, _ string, limit int) ([]*db.ContinueWatchingItem, error) {
		gotLimit = limit
		return nil, nil
	}
	// Valid limit.
	_ = env.do(http.MethodGet, "/api/v1/me/continue-watching?limit=50", "", defaultClaims())
	if gotLimit != 50 {
		t.Errorf("valid limit: got %d want 50", gotLimit)
	}
	// Invalid (>100) ignored, default 20 used.
	_ = env.do(http.MethodGet, "/api/v1/me/continue-watching?limit=999", "", defaultClaims())
	if gotLimit != 20 {
		t.Errorf("invalid limit: got %d want default 20", gotLimit)
	}
	// Non-numeric ignored.
	_ = env.do(http.MethodGet, "/api/v1/me/continue-watching?limit=abc", "", defaultClaims())
	if gotLimit != 20 {
		t.Errorf("non-numeric limit: got %d want default 20", gotLimit)
	}
}

// ─── Favorites ──────────────────────────────────────────────────────────────

func TestProgressHandler_Favorites_RespectsPagination(t *testing.T) {
	env := newProgressTestEnv(t)
	var gotLimit, gotOffset int
	env.userData.favsFn = func(_ context.Context, _ string, limit, offset int) ([]*db.FavoriteItem, error) {
		gotLimit, gotOffset = limit, offset
		return []*db.FavoriteItem{{ItemID: "fav-1", Title: "Favy", Type: "movie", Year: 2020}}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/me/favorites?limit=25&offset=100", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotLimit != 25 || gotOffset != 100 {
		t.Errorf("pagination: limit=%d offset=%d", gotLimit, gotOffset)
	}
}

func TestProgressHandler_Favorites_RejectsNegativeOffset(t *testing.T) {
	env := newProgressTestEnv(t)
	var gotOffset int
	env.userData.favsFn = func(_ context.Context, _ string, _, offset int) ([]*db.FavoriteItem, error) {
		gotOffset = offset
		return nil, nil
	}
	_ = env.do(http.MethodGet, "/api/v1/me/favorites?offset=-5", "", defaultClaims())
	if gotOffset != 0 {
		t.Errorf("negative offset: got %d want 0 (default)", gotOffset)
	}
}

// ─── NextUp ─────────────────────────────────────────────────────────────────

func TestProgressHandler_NextUp_ShapesEpisodes(t *testing.T) {
	env := newProgressTestEnv(t)
	s2, e3 := 2, 3
	env.userData.nextUpFn = func(_ context.Context, _ string, _ int) ([]*db.NextUpItem, error) {
		return []*db.NextUpItem{
			{EpisodeID: "ep-1", EpisodeTitle: "Pilot", SeasonNumber: &s2, EpisodeNumber: &e3,
				DurationTicks: 1200, SeriesTitle: "Show", SeriesID: "series-1"},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/me/next-up", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	data, _ := out["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("want 1 entry, got %d", len(data))
	}
	entry := data[0].(map[string]any)
	if entry["episode_id"] != "ep-1" || entry["series_title"] != "Show" {
		t.Errorf("shape: %v", entry)
	}
	if entry["season_number"] != float64(2) || entry["episode_number"] != float64(3) {
		t.Errorf("season/episode: %v / %v", entry["season_number"], entry["episode_number"])
	}
}

func TestProgressHandler_NextUp_Unauthenticated(t *testing.T) {
	env := newProgressTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/me/next-up", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Bus publication ────────────────────────────────────────────────────────
//
// Verifies the four mutating endpoints emit user-scoped events with
// the right type + Data payload. The cross-device sync depends on
// these — losing a publish call here would break "I started on the
// laptop, my phone catches up" without obvious failure.

type recordingBus struct {
	events []event.Event
}

func (b *recordingBus) Publish(e event.Event) {
	b.events = append(b.events, e)
}

func newProgressTestEnvWithBus(t *testing.T) (*progressTestEnv, *recordingBus) {
	t.Helper()
	bus := &recordingBus{}
	env := &progressTestEnv{
		t:        t,
		userData: newProgressFakeUserData(),
		images:   newFakeImageRepo(),
	}
	env.handler = NewProgressHandler(env.userData, env.images, bus, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/me", func(r chi.Router) {
		r.Put("/progress/{itemId}", env.handler.UpdateProgress)
		r.Post("/progress/{itemId}/played", env.handler.MarkPlayed)
		r.Delete("/progress/{itemId}/played", env.handler.MarkUnplayed)
		r.Post("/favorite/{itemId}", env.handler.ToggleFavorite)
	})
	env.router = r
	return env, bus
}

func TestProgressHandler_UpdateProgress_PublishesEvent(t *testing.T) {
	env, bus := newProgressTestEnvWithBus(t)
	rr := env.do(http.MethodPut, "/api/v1/me/progress/it-1",
		`{"position_ticks": 12345}`, defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	e := bus.events[0]
	if e.Type != event.ProgressUpdated {
		t.Errorf("type: %s", e.Type)
	}
	if e.Data["user_id"] != "user-1" || e.Data["item_id"] != "it-1" {
		t.Errorf("scope fields: %+v", e.Data)
	}
	if e.Data["position_ticks"] != int64(12345) {
		t.Errorf("position_ticks: %v", e.Data["position_ticks"])
	}
}

func TestProgressHandler_MarkPlayed_PublishesEvent(t *testing.T) {
	env, bus := newProgressTestEnvWithBus(t)
	rr := env.do(http.MethodPost, "/api/v1/me/progress/it-2/played", "", defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	e := bus.events[0]
	if e.Type != event.PlayedToggled {
		t.Errorf("type: %s", e.Type)
	}
	if e.Data["played"] != true {
		t.Errorf("played: %v", e.Data["played"])
	}
}

func TestProgressHandler_MarkUnplayed_PublishesPlayedFalse(t *testing.T) {
	env, bus := newProgressTestEnvWithBus(t)
	rr := env.do(http.MethodDelete, "/api/v1/me/progress/it-3/played", "", defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	if bus.events[0].Data["played"] != false {
		t.Errorf("expected played=false: %+v", bus.events[0].Data)
	}
}

func TestProgressHandler_ToggleFavorite_PublishesEvent(t *testing.T) {
	env, bus := newProgressTestEnvWithBus(t)
	rr := env.do(http.MethodPost, "/api/v1/me/favorite/it-4", "", defaultClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	e := bus.events[0]
	if e.Type != event.FavoriteToggled {
		t.Errorf("type: %s", e.Type)
	}
	if e.Data["is_favorite"] != true {
		t.Errorf("is_favorite: %v", e.Data["is_favorite"])
	}
}

// nil bus must not panic. Test rigs that don't care about publication
// pass nil; the publish() helper short-circuits.
func TestProgressHandler_NilBus_NoOpPublish(t *testing.T) {
	env := newProgressTestEnv(t) // nil bus
	rr := env.do(http.MethodPut, "/api/v1/me/progress/it-x",
		`{"position_ticks": 1}`, defaultClaims())
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
}
