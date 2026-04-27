package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

// Reuses libFakeService + libFakeMetadataRepo from library_test.go and
// fakeImageRepo from image_test.go (all in the same test package).

type itemTestEnv struct {
	t        *testing.T
	svc      *libFakeService
	images   *fakeImageRepo
	meta     *libFakeMetadataRepo
	userData *progressFakeUserData
	chapters *fakeChapterRepo
	handler  *ItemHandler
	router   chi.Router
}

// fakeChapterRepo is a minimal in-memory ChapterRepository fake for
// the handler tests. The repo interface only needs ListByItem so the
// fake can stay this small.
type fakeChapterRepo struct {
	byItem map[string][]*db.Chapter
}

func newFakeChapterRepo() *fakeChapterRepo {
	return &fakeChapterRepo{byItem: map[string][]*db.Chapter{}}
}

func (r *fakeChapterRepo) ListByItem(_ context.Context, itemID string) ([]*db.Chapter, error) {
	return r.byItem[itemID], nil
}

func newItemTestEnv(t *testing.T) *itemTestEnv {
	t.Helper()
	env := &itemTestEnv{
		t:      t,
		svc:    &libFakeService{},
		images: newFakeImageRepo(),
		meta:   &libFakeMetadataRepo{byID: map[string]*db.Metadata{}},
	}
	env.userData = newProgressFakeUserData()
	env.chapters = newFakeChapterRepo()
	env.handler = NewItemHandler(env.svc, env.images, env.meta, env.userData, env.chapters, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/items", func(r chi.Router) {
		r.Get("/search", env.handler.Search)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", env.handler.Get)
			r.Get("/children", env.handler.Children)
		})
	})
	env.router = r
	return env
}

func (e *itemTestEnv) do(method, path string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *itemTestEnv) doWithClaims(method, path string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func itemDecodeData(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

// ─── Get ────────────────────────────────────────────────────────────────────

func TestItemHandler_Get_NotFound_404(t *testing.T) {
	env := newItemTestEnv(t)
	// Default getItemFn returns domain.NewNotFound via the fake.
	rr := env.do(http.MethodGet, "/api/v1/items/missing/")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

func TestItemHandler_Get_ServiceError_500(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, _ string) (*db.Item, error) {
		return nil, errors.New("db down")
	}
	rr := env.do(http.MethodGet, "/api/v1/items/x/")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", rr.Code)
	}
}

func TestItemHandler_Get_HappyPath_Minimal(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, LibraryID: "lib-1", Type: "movie", Title: "Foo", Year: 2020}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := itemDecodeData(t, rr).(map[string]any)
	if data["id"] != "it-1" || data["title"] != "Foo" || data["year"] != float64(2020) {
		t.Errorf("shape: %v", data)
	}
}

func TestItemHandler_Get_IncludesStreams(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Type: "movie", Title: "Foo"}, nil
	}
	env.svc.getStreamsFn = func(_ context.Context, _ string) ([]*db.MediaStream, error) {
		return []*db.MediaStream{
			{StreamIndex: 0, StreamType: "video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "audio", Codec: "aac", Channels: 6, SampleRate: 48000},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	data, _ := itemDecodeData(t, rr).(map[string]any)
	streams, _ := data["media_streams"].([]any)
	if len(streams) != 2 {
		t.Fatalf("streams: %d", len(streams))
	}
}

func TestItemHandler_Get_ExtractsPrimaryPosterFromImages(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Title: "Foo"}, nil
	}
	env.svc.getItemImagesFn = func(_ context.Context, _ string) ([]*db.Image, error) {
		return []*db.Image{
			{ID: "img-p", Type: "primary", Path: "/api/v1/images/file/img-p", IsPrimary: true},
			{ID: "img-b", Type: "backdrop", Path: "/api/v1/images/file/img-b", IsPrimary: true},
			{ID: "img-l", Type: "logo", Path: "/api/v1/images/file/img-l", IsPrimary: true},
			{ID: "img-sec", Type: "primary", Path: "/alt", IsPrimary: false},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	data, _ := itemDecodeData(t, rr).(map[string]any)
	if data["poster_url"] != "/api/v1/images/file/img-p" {
		t.Errorf("poster_url: %v", data["poster_url"])
	}
	if data["backdrop_url"] != "/api/v1/images/file/img-b" {
		t.Errorf("backdrop_url: %v", data["backdrop_url"])
	}
	if data["logo_url"] != "/api/v1/images/file/img-l" {
		t.Errorf("logo_url: %v", data["logo_url"])
	}
}

func TestItemHandler_Get_AttachesMetadata(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Title: "Foo"}, nil
	}
	env.meta.byID["it-1"] = &db.Metadata{
		ItemID: "it-1", Overview: "The plot", Tagline: "Catchy!",
		Studio: "Acme", GenresJSON: `["drama","thriller"]`,
	}
	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	data, _ := itemDecodeData(t, rr).(map[string]any)
	if data["overview"] != "The plot" || data["tagline"] != "Catchy!" || data["studio"] != "Acme" {
		t.Errorf("meta: %v", data)
	}
	genres, _ := data["genres"].([]any)
	if len(genres) != 2 {
		t.Errorf("genres: %v", data["genres"])
	}
}

func TestItemHandler_Get_IncludesUserDataWhenAuthenticated(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Type: "movie", Title: "Foo", DurationTicks: 1_000}, nil
	}
	env.userData.data["u-2:it-1"] = &db.UserData{
		UserID: "u-2", ItemID: "it-1", PositionTicks: 500, IsFavorite: true,
	}

	rr := env.doWithClaims(http.MethodGet, "/api/v1/items/it-1/", &auth.Claims{UserID: "u-2", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := itemDecodeData(t, rr).(map[string]any)
	ud, ok := data["user_data"].(map[string]any)
	if !ok {
		t.Fatalf("user_data missing: %v", data)
	}
	if ud["is_favorite"] != true {
		t.Errorf("is_favorite: %v", ud["is_favorite"])
	}
	prog, _ := ud["progress"].(map[string]any)
	if prog["percentage"] != 50.0 {
		t.Errorf("percentage: %v", prog["percentage"])
	}
}

func TestItemHandler_Get_OmitsUserDataWhenAnonymous(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Title: "Foo"}, nil
	}
	env.userData.data["u-2:it-1"] = &db.UserData{UserID: "u-2", ItemID: "it-1", IsFavorite: true}

	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	data, _ := itemDecodeData(t, rr).(map[string]any)
	if _, present := data["user_data"]; present {
		t.Errorf("anonymous response should not include user_data: %v", data["user_data"])
	}
}

func TestItemHandler_Get_IncludesChapters(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Type: "movie", Title: "Foo", DurationTicks: 60_000_000_000}, nil
	}
	env.chapters.byItem["it-1"] = []*db.Chapter{
		{ItemID: "it-1", StartTicks: 0, EndTicks: 30_000_000_000, Title: "Cold Open"},
		{ItemID: "it-1", StartTicks: 30_000_000_000, EndTicks: 60_000_000_000, Title: ""},
	}

	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := itemDecodeData(t, rr).(map[string]any)
	chapters, ok := data["chapters"].([]any)
	if !ok {
		t.Fatalf("chapters missing or wrong type: %v", data["chapters"])
	}
	if len(chapters) != 2 {
		t.Fatalf("chapters: got %d want 2", len(chapters))
	}
	first, _ := chapters[0].(map[string]any)
	if first["title"] != "Cold Open" || first["start_ticks"] != float64(0) {
		t.Errorf("first chapter shape: %v", first)
	}
	// Second chapter has empty title — must still be emitted as empty
	// string so clients don't have to guard `undefined` vs "".
	second, _ := chapters[1].(map[string]any)
	if second["title"] != "" {
		t.Errorf("second chapter title: %v", second["title"])
	}
}

func TestItemHandler_Get_OmitsChaptersWhenAbsent(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getItemFn = func(_ context.Context, id string) (*db.Item, error) {
		return &db.Item{ID: id, Title: "Foo"}, nil
	}
	// No chapters seeded; handler should omit the key entirely so
	// the JSON stays compact rather than `"chapters": []`.
	rr := env.do(http.MethodGet, "/api/v1/items/it-1/")
	data, _ := itemDecodeData(t, rr).(map[string]any)
	if _, present := data["chapters"]; present {
		t.Errorf("expected chapters key to be absent, got: %v", data["chapters"])
	}
}

// ─── Children ───────────────────────────────────────────────────────────────

func TestItemHandler_Children_Empty(t *testing.T) {
	env := newItemTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/items/p-1/children")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := itemDecodeData(t, rr).([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty, got %d", len(data))
	}
}

func TestItemHandler_Children_HappyPath(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getChildrenFn = func(_ context.Context, _ string) ([]*db.Item, error) {
		return []*db.Item{
			{ID: "c-1", Type: "episode", Title: "E1"},
			{ID: "c-2", Type: "episode", Title: "E2"},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items/p-1/children")
	data, _ := itemDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("children: %d", len(data))
	}
}

func TestItemHandler_Children_ServiceError(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.getChildrenFn = func(_ context.Context, _ string) ([]*db.Item, error) {
		return nil, domain.NewNotFound("item")
	}
	rr := env.do(http.MethodGet, "/api/v1/items/missing/children")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── Search ─────────────────────────────────────────────────────────────────

func TestItemHandler_Search_MissingQuery_400(t *testing.T) {
	env := newItemTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/items/search")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestItemHandler_Search_PassesFilterAndReturnsTotal(t *testing.T) {
	env := newItemTestEnv(t)
	var gotFilter db.ItemFilter
	env.svc.listItemsFn = func(_ context.Context, f db.ItemFilter) ([]*db.Item, int, error) {
		gotFilter = f
		return []*db.Item{{ID: "a", Title: "X"}, {ID: "b", Title: "Y"}}, 42, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/items/search?q=foo&limit=5&library_id=lib-1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotFilter.Query != "foo" || gotFilter.Limit != 5 || gotFilter.LibraryID != "lib-1" {
		t.Errorf("filter: %+v", gotFilter)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	if out["total"] != float64(42) {
		t.Errorf("total: %v", out["total"])
	}
	data, _ := out["data"].([]any)
	if len(data) != 2 {
		t.Errorf("data: %d", len(data))
	}
}

func TestItemHandler_Search_ServiceError(t *testing.T) {
	env := newItemTestEnv(t)
	env.svc.listItemsFn = func(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
		return nil, 0, errors.New("fts broken")
	}
	rr := env.do(http.MethodGet, "/api/v1/items/search?q=x")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", rr.Code)
	}
}
