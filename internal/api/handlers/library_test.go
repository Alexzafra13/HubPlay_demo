package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/library"
	"hubplay/internal/scanner"
	"hubplay/internal/testutil"
)

// ─── Fake LibraryService ────────────────────────────────────────────────────
//
// The interface has ~16 methods; the handler only exercises a subset. The fake
// fields below are set only for methods a given test actually uses — the rest
// return zero values.

type libFakeService struct {
	createFn        func(ctx context.Context, req library.CreateRequest) (*db.Library, error)
	getByIDFn       func(ctx context.Context, id string) (*db.Library, error)
	listFn          func(ctx context.Context) ([]*db.Library, error)
	listForUserFn   func(ctx context.Context, userID string) ([]*db.Library, error)
	updateFn        func(ctx context.Context, id string, req library.UpdateRequest) (*db.Library, error)
	deleteFn        func(ctx context.Context, id string) error
	scanFn          func(ctx context.Context, id string, refresh ...bool) error
	listItemsFn     func(ctx context.Context, f db.ItemFilter) ([]*db.Item, int, error)
	latestFn        func(ctx context.Context, libraryID, itemType string, limit int) ([]*db.Item, error)
	itemCountFn     func(ctx context.Context, libraryID string) (int, error)
	getItemFn       func(ctx context.Context, id string) (*db.Item, error)
	getChildrenFn   func(ctx context.Context, id string) ([]*db.Item, error)
	getStreamsFn    func(ctx context.Context, id string) ([]*db.MediaStream, error)
	getItemImagesFn func(ctx context.Context, id string) ([]*db.Image, error)
}

func (s *libFakeService) Create(ctx context.Context, req library.CreateRequest) (*db.Library, error) {
	if s.createFn != nil {
		return s.createFn(ctx, req)
	}
	return nil, errors.New("create: not configured")
}

func (s *libFakeService) GetByID(ctx context.Context, id string) (*db.Library, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, id)
	}
	return nil, domain.NewNotFound("library")
}

func (s *libFakeService) List(ctx context.Context) ([]*db.Library, error) {
	if s.listFn != nil {
		return s.listFn(ctx)
	}
	return nil, nil
}

func (s *libFakeService) ListForUser(ctx context.Context, userID string) ([]*db.Library, error) {
	if s.listForUserFn != nil {
		return s.listForUserFn(ctx, userID)
	}
	return nil, nil
}

func (s *libFakeService) Update(ctx context.Context, id string, req library.UpdateRequest) (*db.Library, error) {
	if s.updateFn != nil {
		return s.updateFn(ctx, id, req)
	}
	return nil, errors.New("update: not configured")
}

func (s *libFakeService) Delete(ctx context.Context, id string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, id)
	}
	return nil
}

func (s *libFakeService) Scan(ctx context.Context, id string, refresh ...bool) error {
	if s.scanFn != nil {
		return s.scanFn(ctx, id, refresh...)
	}
	return nil
}

func (s *libFakeService) ScanSync(_ context.Context, _ string) (*scanner.ScanResult, error) {
	return nil, nil
}

func (s *libFakeService) IsScanning(_ string) bool { return false }

func (s *libFakeService) ListItems(ctx context.Context, f db.ItemFilter) ([]*db.Item, int, error) {
	if s.listItemsFn != nil {
		return s.listItemsFn(ctx, f)
	}
	return nil, 0, nil
}

func (s *libFakeService) GetItem(ctx context.Context, id string) (*db.Item, error) {
	if s.getItemFn != nil {
		return s.getItemFn(ctx, id)
	}
	return nil, domain.NewNotFound("item")
}
func (s *libFakeService) GetItemChildren(ctx context.Context, id string) ([]*db.Item, error) {
	if s.getChildrenFn != nil {
		return s.getChildrenFn(ctx, id)
	}
	return nil, nil
}
func (s *libFakeService) GetItemStreams(ctx context.Context, id string) ([]*db.MediaStream, error) {
	if s.getStreamsFn != nil {
		return s.getStreamsFn(ctx, id)
	}
	return nil, nil
}
func (s *libFakeService) GetItemImages(ctx context.Context, id string) ([]*db.Image, error) {
	if s.getItemImagesFn != nil {
		return s.getItemImagesFn(ctx, id)
	}
	return nil, nil
}

func (s *libFakeService) LatestItems(ctx context.Context, libraryID, itemType string, limit int) ([]*db.Item, error) {
	if s.latestFn != nil {
		return s.latestFn(ctx, libraryID, itemType, limit)
	}
	return nil, nil
}

func (s *libFakeService) ItemCount(ctx context.Context, libraryID string) (int, error) {
	if s.itemCountFn != nil {
		return s.itemCountFn(ctx, libraryID)
	}
	return 0, nil
}

func (s *libFakeService) UserHasAccess(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// ─── Fake MetadataRepository ────────────────────────────────────────────────

type libFakeMetadataRepo struct {
	byID map[string]*db.Metadata
}

func (r *libFakeMetadataRepo) GetByItemID(_ context.Context, itemID string) (*db.Metadata, error) {
	if m, ok := r.byID[itemID]; ok {
		return m, nil
	}
	return nil, domain.NewNotFound("metadata")
}

func (r *libFakeMetadataRepo) GetMetadataBatch(_ context.Context, ids []string) (map[string]*db.Metadata, error) {
	out := map[string]*db.Metadata{}
	for _, id := range ids {
		if m, ok := r.byID[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}

// Compile-time checks.
var (
	_ LibraryService     = (*libFakeService)(nil)
	_ MetadataRepository = (*libFakeMetadataRepo)(nil)
)

// ─── Env ────────────────────────────────────────────────────────────────────

type libTestEnv struct {
	t        *testing.T
	svc      *libFakeService
	images   *fakeImageRepo // reused from image_test.go
	meta     *libFakeMetadataRepo
	userData *progressFakeUserData
	handler  *LibraryHandler
	router   chi.Router
}

func newLibTestEnv(t *testing.T) *libTestEnv {
	t.Helper()
	env := &libTestEnv{
		t:        t,
		svc:      &libFakeService{},
		images:   newFakeImageRepo(),
		meta:     &libFakeMetadataRepo{byID: map[string]*db.Metadata{}},
		userData: newProgressFakeUserData(),
	}
	env.handler = NewLibraryHandler(env.svc, env.images, env.meta, env.userData, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/libraries", env.handler.Create)
		r.Get("/libraries", env.handler.List)
		r.Get("/libraries/latest-items", env.handler.LatestItems)
		r.Get("/libraries/{id}", env.handler.Get)
		r.Put("/libraries/{id}", env.handler.Update)
		r.Delete("/libraries/{id}", env.handler.Delete)
		r.Post("/libraries/{id}/scan", env.handler.Scan)
		r.Post("/libraries/browse", env.handler.Browse)
		r.Get("/libraries/{id}/items", env.handler.Items)
	})
	env.router = r
	return env
}

func (e *libTestEnv) do(method, path, body string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
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

func libDecodeData(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

func adminClaims() *auth.Claims { return &auth.Claims{UserID: "u-1", Role: "admin"} }
func userClaims() *auth.Claims  { return &auth.Claims{UserID: "u-2", Role: "user"} }

// ─── Create ─────────────────────────────────────────────────────────────────

func TestLibraryHandler_Create_HappyPath(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.createFn = func(_ context.Context, req library.CreateRequest) (*db.Library, error) {
		return &db.Library{
			ID: "lib-new", Name: req.Name, ContentType: req.ContentType,
			ScanMode: req.ScanMode, Paths: req.Paths,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, nil
	}
	body := `{"name":"Movies","content_type":"movies","paths":["/m"],"scan_mode":"auto"}`
	rr := env.do(http.MethodPost, "/api/v1/libraries", body, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d want 201, body: %s", rr.Code, rr.Body.String())
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["id"] != "lib-new" || data["name"] != "Movies" {
		t.Errorf("shape: %v", data)
	}
}

func TestLibraryHandler_Create_InvalidJSON_400(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries", `{not json`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestLibraryHandler_Create_ServiceError_Mapped(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.createFn = func(_ context.Context, _ library.CreateRequest) (*db.Library, error) {
		return nil, domain.NewAlreadyExists("library")
	}
	rr := env.do(http.MethodPost, "/api/v1/libraries", `{"name":"X"}`, nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status: got %d want 409", rr.Code)
	}
}

// ─── List ───────────────────────────────────────────────────────────────────

func TestLibraryHandler_List_Unauthenticated_401(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/libraries", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestLibraryHandler_List_Admin_UsesListAll(t *testing.T) {
	env := newLibTestEnv(t)
	adminHit, userHit := false, false
	env.svc.listFn = func(_ context.Context) ([]*db.Library, error) {
		adminHit = true
		return []*db.Library{{ID: "lib-1", Name: "L1"}}, nil
	}
	env.svc.listForUserFn = func(_ context.Context, _ string) ([]*db.Library, error) {
		userHit = true
		return nil, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries", "", adminClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if !adminHit || userHit {
		t.Errorf("admin should use List, not ListForUser")
	}
}

func TestLibraryHandler_List_User_UsesListForUser(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listForUserFn = func(_ context.Context, userID string) ([]*db.Library, error) {
		if userID != "u-2" {
			t.Errorf("userID: %q", userID)
		}
		return []*db.Library{{ID: "lib-1"}}, nil
	}
	env.svc.itemCountFn = func(_ context.Context, _ string) (int, error) { return 42, nil }

	rr := env.do(http.MethodGet, "/api/v1/libraries", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := libDecodeData(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("want 1 entry, got %d", len(data))
	}
	entry, _ := data[0].(map[string]any)
	if entry["item_count"] != float64(42) {
		t.Errorf("item_count not attached: %v", entry["item_count"])
	}
}

// ─── Get ────────────────────────────────────────────────────────────────────

func TestLibraryHandler_Get_HappyPath(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.getByIDFn = func(_ context.Context, id string) (*db.Library, error) {
		return &db.Library{ID: id, Name: "Movies"}, nil
	}
	env.svc.itemCountFn = func(_ context.Context, _ string) (int, error) { return 7, nil }
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["id"] != "lib-1" || data["item_count"] != float64(7) {
		t.Errorf("shape: %v", data)
	}
}

func TestLibraryHandler_Get_NotFound_404(t *testing.T) {
	env := newLibTestEnv(t)
	// Default getByIDFn nil → returns NewNotFound("library") via the fake's fallback.
	rr := env.do(http.MethodGet, "/api/v1/libraries/missing", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── Update ─────────────────────────────────────────────────────────────────

func TestLibraryHandler_Update_HappyPath(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.updateFn = func(_ context.Context, id string, req library.UpdateRequest) (*db.Library, error) {
		return &db.Library{ID: id, Name: req.Name, ContentType: req.ContentType}, nil
	}
	body := `{"name":"Renamed","content_type":"movies","paths":["/x"],"scan_mode":"manual"}`
	rr := env.do(http.MethodPut, "/api/v1/libraries/lib-1", body, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["name"] != "Renamed" {
		t.Errorf("name: %v", data["name"])
	}
}

func TestLibraryHandler_Update_InvalidJSON_400(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/libraries/lib-1", `{not}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Delete ─────────────────────────────────────────────────────────────────

func TestLibraryHandler_Delete_HappyPath(t *testing.T) {
	env := newLibTestEnv(t)
	var got string
	env.svc.deleteFn = func(_ context.Context, id string) error { got = id; return nil }
	rr := env.do(http.MethodDelete, "/api/v1/libraries/lib-1", "", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if got != "lib-1" {
		t.Errorf("Delete called with: %q", got)
	}
}

func TestLibraryHandler_Delete_NotFound_404(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.deleteFn = func(_ context.Context, _ string) error { return domain.NewNotFound("library") }
	rr := env.do(http.MethodDelete, "/api/v1/libraries/missing", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Scan ───────────────────────────────────────────────────────────────────

func TestLibraryHandler_Scan_Accepted(t *testing.T) {
	env := newLibTestEnv(t)
	var gotRefresh bool
	env.svc.scanFn = func(_ context.Context, _ string, refresh ...bool) error {
		if len(refresh) > 0 {
			gotRefresh = refresh[0]
		}
		return nil
	}
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/scan?refresh_metadata=true", "", nil)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202", rr.Code)
	}
	if !gotRefresh {
		t.Errorf("refresh_metadata=true should propagate to service")
	}
}

func TestLibraryHandler_Scan_ServiceError_Mapped(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.scanFn = func(_ context.Context, _ string, _ ...bool) error {
		return domain.NewNotFound("library")
	}
	rr := env.do(http.MethodPost, "/api/v1/libraries/missing/scan", "", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Browse ─────────────────────────────────────────────────────────────────

func TestLibraryHandler_Browse_InvalidJSON_400(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/browse", `{bogus`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestLibraryHandler_Browse_SensitivePath_403(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/browse", `{"path":"/etc"}`, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", rr.Code)
	}
}

func TestLibraryHandler_Browse_NonexistentPath_400(t *testing.T) {
	env := newLibTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/browse",
		`{"path":"/definitely/does/not/exist/zzz"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestLibraryHandler_Browse_ListsSubdirectories(t *testing.T) {
	env := newLibTestEnv(t)
	root := t.TempDir()
	// Create two subdirs + one hidden dir (should be skipped) + one file.
	for _, name := range []string{"dir1", "dir2", ".hidden"} {
		if err := createDir(root, name); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := createFile(root, "readme.txt"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	body := `{"path":"` + strings.ReplaceAll(root, `\`, `\\`) + `"}`
	rr := env.do(http.MethodPost, "/api/v1/libraries/browse", body, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", rr.Code, rr.Body.String())
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	dirs, _ := data["directories"].([]any)
	if len(dirs) != 2 {
		t.Fatalf("dirs: got %d want 2 (hidden + file excluded)", len(dirs))
	}
}

// ─── Items ──────────────────────────────────────────────────────────────────

func TestLibraryHandler_Items_RespectsFilter(t *testing.T) {
	env := newLibTestEnv(t)
	var gotFilter db.ItemFilter
	env.svc.listItemsFn = func(_ context.Context, f db.ItemFilter) ([]*db.Item, int, error) {
		gotFilter = f
		return []*db.Item{{ID: "it-1", LibraryID: f.LibraryID, Title: "Movie A"}}, 100, nil
	}
	url := "/api/v1/libraries/lib-1/items?limit=25&offset=50&sort_by=title&sort_order=asc&type=movie&parent_id=p-1"
	rr := env.do(http.MethodGet, url, "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotFilter.LibraryID != "lib-1" || gotFilter.Limit != 25 || gotFilter.Offset != 50 ||
		gotFilter.SortBy != "title" || gotFilter.SortOrder != "asc" ||
		gotFilter.Type != "movie" || gotFilter.ParentID != "p-1" {
		t.Errorf("filter passed: %+v", gotFilter)
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["total"] != float64(100) || data["offset"] != float64(50) || data["limit"] != float64(25) {
		t.Errorf("pagination fields: %v", data)
	}
}

func TestLibraryHandler_Items_NextCursorOnFullPage(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return []*db.Item{
			{ID: "a"}, {ID: "b"}, {ID: "c"},
		}, 300, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items?limit=3", "", nil)
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["next_cursor"] != "c" {
		t.Errorf("next_cursor: %v want 'c'", data["next_cursor"])
	}
}

func TestLibraryHandler_Items_NoCursorWhenPartialPage(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return []*db.Item{{ID: "a"}}, 1, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items?limit=25", "", nil)
	data, _ := libDecodeData(t, rr).(map[string]any)
	if _, has := data["next_cursor"]; has {
		t.Errorf("next_cursor should be absent on partial page")
	}
}

func TestLibraryHandler_Items_EnrichesWithMetadata(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return []*db.Item{{ID: "it-1", Title: "Movie"}}, 1, nil
	}
	env.meta.byID["it-1"] = &db.Metadata{
		ItemID: "it-1", Overview: "A movie", Tagline: "Big", GenresJSON: `["action","drama"]`,
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items?limit=10", "", nil)
	data, _ := libDecodeData(t, rr).(map[string]any)
	items, _ := data["items"].([]any)
	entry, _ := items[0].(map[string]any)
	if entry["overview"] != "A movie" || entry["tagline"] != "Big" {
		t.Errorf("metadata not attached: %v", entry)
	}
	genres, _ := entry["genres"].([]any)
	if len(genres) != 2 {
		t.Errorf("genres: %v", entry["genres"])
	}
}

func TestLibraryHandler_Items_IncludesUserDataWhenAuthenticated(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return []*db.Item{
			{ID: "it-1", Title: "Watched", DurationTicks: 1_000},
			{ID: "it-2", Title: "InProgress", DurationTicks: 1_000},
			{ID: "it-3", Title: "Untouched", DurationTicks: 1_000},
		}, 3, nil
	}
	// Seed per-user state for u-2 (the userClaims fixture).
	env.userData.data["u-2:it-1"] = &db.UserData{
		UserID: "u-2", ItemID: "it-1", Completed: true, PlayCount: 1, PositionTicks: 1_000,
	}
	env.userData.data["u-2:it-2"] = &db.UserData{
		UserID: "u-2", ItemID: "it-2", PositionTicks: 250, IsFavorite: true,
	}

	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items?limit=10", "", userClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rr.Code, rr.Body.String())
	}

	data, _ := libDecodeData(t, rr).(map[string]any)
	items, _ := data["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items: got %d want 3", len(items))
	}

	// it-1: watched at 100%
	got1, _ := items[0].(map[string]any)
	ud1, ok := got1["user_data"].(map[string]any)
	if !ok {
		t.Fatalf("it-1 missing user_data: %v", got1)
	}
	if ud1["played"] != true {
		t.Errorf("it-1 played: %v", ud1["played"])
	}
	prog1, _ := ud1["progress"].(map[string]any)
	if prog1["percentage"] != 100.0 {
		t.Errorf("it-1 percentage: %v", prog1["percentage"])
	}

	// it-2: in progress at 25%, favorite
	got2, _ := items[1].(map[string]any)
	ud2, _ := got2["user_data"].(map[string]any)
	if ud2["is_favorite"] != true {
		t.Errorf("it-2 is_favorite: %v", ud2["is_favorite"])
	}
	prog2, _ := ud2["progress"].(map[string]any)
	if prog2["percentage"] != 25.0 {
		t.Errorf("it-2 percentage: %v", prog2["percentage"])
	}

	// it-3: no row → user_data must be absent (not null) so the JSON
	// payload stays compact.
	got3, _ := items[2].(map[string]any)
	if _, present := got3["user_data"]; present {
		t.Errorf("it-3 should not have user_data, got: %v", got3["user_data"])
	}
}

func TestLibraryHandler_Items_OmitsUserDataWhenAnonymous(t *testing.T) {
	env := newLibTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return []*db.Item{{ID: "it-1", DurationTicks: 1_000}}, 1, nil
	}
	env.userData.data["u-2:it-1"] = &db.UserData{UserID: "u-2", ItemID: "it-1", Completed: true}

	// No claims attached — this exercises the listing endpoint pre-auth
	// (chi route exists, real wiring requires middleware) and asserts we
	// don't leak another user's state to anonymous callers.
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/items?limit=10", "", nil)
	data, _ := libDecodeData(t, rr).(map[string]any)
	items, _ := data["items"].([]any)
	got, _ := items[0].(map[string]any)
	if _, present := got["user_data"]; present {
		t.Errorf("anonymous response should not include user_data: %v", got)
	}
}

// ─── LatestItems ────────────────────────────────────────────────────────────

func TestLibraryHandler_LatestItems_RespectsQueryParams(t *testing.T) {
	env := newLibTestEnv(t)
	var gotLib, gotType string
	var gotLimit int
	env.svc.latestFn = func(_ context.Context, libraryID, itemType string, limit int) ([]*db.Item, error) {
		gotLib, gotType, gotLimit = libraryID, itemType, limit
		return []*db.Item{{ID: "it-1"}, {ID: "it-2"}}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/latest-items?library_id=lib-1&type=movie&limit=5", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotLib != "lib-1" || gotType != "movie" || gotLimit != 5 {
		t.Errorf("params: lib=%q type=%q limit=%d", gotLib, gotType, gotLimit)
	}
	data, _ := libDecodeData(t, rr).(map[string]any)
	if data["total"] != float64(2) {
		t.Errorf("total: %v", data["total"])
	}
}

// ─── Small filesystem helpers for Browse tests ──────────────────────────────

func createDir(root, name string) error {
	return os.MkdirAll(filepath.Join(root, name), 0o755)
}

func createFile(root, name string) error {
	return os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644)
}
