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

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/provider"
	"hubplay/internal/testutil"
)

// ─── Fake ProviderRepository ────────────────────────────────────────────────

type providersFakeRepo struct {
	listFn    func(ctx context.Context) ([]*db.ProviderConfig, error)
	getByName map[string]*db.ProviderConfig
	upserted  []*db.ProviderConfig
	upsertErr error
}

func (r *providersFakeRepo) ListAll(ctx context.Context) ([]*db.ProviderConfig, error) {
	if r.listFn != nil {
		return r.listFn(ctx)
	}
	return nil, nil
}

func (r *providersFakeRepo) GetByName(_ context.Context, name string) (*db.ProviderConfig, error) {
	if c, ok := r.getByName[name]; ok {
		return c, nil
	}
	return nil, nil // handler treats nil + nil error as "not found" (404)
}

func (r *providersFakeRepo) Upsert(_ context.Context, cfg *db.ProviderConfig) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	cp := *cfg
	r.upserted = append(r.upserted, &cp)
	return nil
}

var _ ProviderRepository = (*providersFakeRepo)(nil)

// Reuses fakeProviderManager from image_test.go for the ProviderManager dep.

// ─── Env ────────────────────────────────────────────────────────────────────

type providerTestEnv struct {
	t       *testing.T
	mgr     *fakeProviderManager
	repo    *providersFakeRepo
	handler *ProviderHandler
	router  chi.Router
}

func newProviderTestEnv(t *testing.T) *providerTestEnv {
	t.Helper()
	env := &providerTestEnv{
		t:    t,
		mgr:  &fakeProviderManager{},
		repo: &providersFakeRepo{getByName: map[string]*db.ProviderConfig{}},
	}
	env.handler = NewProviderHandler(env.mgr, env.repo, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/providers", func(r chi.Router) {
		r.Get("/", env.handler.List)
		r.Put("/{name}", env.handler.Update)
		r.Get("/search/metadata", env.handler.SearchMetadata)
		r.Get("/metadata/{externalId}", env.handler.GetMetadata)
		r.Get("/images", env.handler.GetImages)
		r.Get("/search/subtitles", env.handler.SearchSubtitles)
	})
	env.router = r
	return env
}

func (e *providerTestEnv) do(method, path, body string) *httptest.ResponseRecorder {
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

func provDecodeData(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

// ─── List ───────────────────────────────────────────────────────────────────

func TestProviderHandler_List_Empty(t *testing.T) {
	env := newProviderTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/providers/", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := provDecodeData(t, rr).([]any)
	if len(data) != 0 {
		t.Errorf("empty: %d", len(data))
	}
}

func TestProviderHandler_List_MasksAPIKey(t *testing.T) {
	env := newProviderTestEnv(t)
	env.repo.listFn = func(_ context.Context) ([]*db.ProviderConfig, error) {
		return []*db.ProviderConfig{
			{Name: "tmdb", Type: "metadata", Status: "active", Priority: 1, APIKey: "abcd1234efgh5678"},
			{Name: "fanart", Type: "image", Status: "active", Priority: 2, APIKey: ""},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/providers/", "")
	data, _ := provDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("list: %d", len(data))
	}
	tmdb, _ := data[0].(map[string]any)
	if tmdb["has_api_key"] != true {
		t.Errorf("has_api_key: %v", tmdb["has_api_key"])
	}
	masked, _ := tmdb["api_key_masked"].(string)
	if masked != "abcd****5678" {
		t.Errorf("mask: %q", masked)
	}
	fanart, _ := data[1].(map[string]any)
	if fanart["has_api_key"] != false {
		t.Errorf("fanart has_api_key: %v", fanart["has_api_key"])
	}
	if _, ok := fanart["api_key_masked"]; ok {
		t.Errorf("fanart should not expose api_key_masked when empty")
	}
}

// ─── Update ─────────────────────────────────────────────────────────────────

func TestProviderHandler_Update_InvalidJSON_400(t *testing.T) {
	env := newProviderTestEnv(t)
	rr := env.do(http.MethodPut, "/api/v1/providers/tmdb", `{not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_Update_NotFound_404(t *testing.T) {
	env := newProviderTestEnv(t)
	// repo.getByName returns nil,nil for unknown → handler maps to 404.
	rr := env.do(http.MethodPut, "/api/v1/providers/ghost", `{"status":"disabled"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_Update_AppliesPartialPatch(t *testing.T) {
	env := newProviderTestEnv(t)
	env.repo.getByName["tmdb"] = &db.ProviderConfig{
		Name: "tmdb", Status: "active", Priority: 1,
		APIKey: "old-key", ConfigJSON: `{"lang":"en"}`,
	}
	body := `{"status":"disabled","priority":5,"api_key":"new-key","config":{"region":"us"}}`
	rr := env.do(http.MethodPut, "/api/v1/providers/tmdb", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rr.Code, rr.Body.String())
	}
	if len(env.repo.upserted) != 1 {
		t.Fatalf("Upsert not called: %d", len(env.repo.upserted))
	}
	got := env.repo.upserted[0]
	if got.Status != "disabled" || got.Priority != 5 || got.APIKey != "new-key" {
		t.Errorf("patched: %+v", got)
	}
	// Existing config field preserved, new one merged.
	if !strings.Contains(got.ConfigJSON, `"lang":"en"`) || !strings.Contains(got.ConfigJSON, `"region":"us"`) {
		t.Errorf("config merge: %s", got.ConfigJSON)
	}
}

func TestProviderHandler_Update_UpsertError_500(t *testing.T) {
	env := newProviderTestEnv(t)
	env.repo.getByName["tmdb"] = &db.ProviderConfig{Name: "tmdb"}
	env.repo.upsertErr = errors.New("locked")
	rr := env.do(http.MethodPut, "/api/v1/providers/tmdb", `{"status":"active"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── SearchMetadata ─────────────────────────────────────────────────────────

func TestProviderHandler_SearchMetadata_MissingTitle_400(t *testing.T) {
	env := newProviderTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/providers/search/metadata", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_SearchMetadata_Happy(t *testing.T) {
	env := newProviderTestEnv(t)
	// fakeProviderManager has no direct hook for SearchMetadata — extend via
	// a closure captured in a bespoke struct? Instead, use the one seam it
	// exposes: nothing. So we bypass by constructing a new one inline here.
	//
	// The simplest path: override via a wrapper struct satisfying the
	// ProviderManager interface with ad-hoc behaviour.
	mgr := &searchMetadataMgr{
		fn: func(_ context.Context, q provider.SearchQuery) ([]provider.SearchResult, error) {
			if q.Title != "Dune" {
				t.Errorf("title: %q", q.Title)
			}
			return []provider.SearchResult{{ExternalID: "42", Title: "Dune", Year: 2021, Score: 0.9}}, nil
		},
	}
	env.handler = NewProviderHandler(mgr, env.repo, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/providers/search/metadata", env.handler.SearchMetadata)
	env.router = r

	rr := env.do(http.MethodGet, "/api/v1/providers/search/metadata?title=Dune&type=movie", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := provDecodeData(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("results: %d", len(data))
	}
}

// searchMetadataMgr wraps a custom SearchMetadata func. Embeds fakeProviderManager
// for no-op defaults on the other methods.
type searchMetadataMgr struct {
	fakeProviderManager
	fn func(ctx context.Context, q provider.SearchQuery) ([]provider.SearchResult, error)
}

func (m *searchMetadataMgr) SearchMetadata(ctx context.Context, q provider.SearchQuery) ([]provider.SearchResult, error) {
	return m.fn(ctx, q)
}

// ─── GetMetadata ────────────────────────────────────────────────────────────

func TestProviderHandler_GetMetadata_Error_404(t *testing.T) {
	env := newProviderTestEnv(t)
	mgr := &getMetadataMgr{fn: func(_ context.Context, _ string, _ provider.ItemType) (*provider.MetadataResult, error) {
		return nil, errors.New("provider down")
	}}
	env.handler = NewProviderHandler(mgr, env.repo, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/providers/metadata/{externalId}", env.handler.GetMetadata)
	env.router = r

	rr := env.do(http.MethodGet, "/api/v1/providers/metadata/42", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_GetMetadata_Happy(t *testing.T) {
	env := newProviderTestEnv(t)
	mgr := &getMetadataMgr{fn: func(_ context.Context, id string, tp provider.ItemType) (*provider.MetadataResult, error) {
		if tp != provider.ItemSeries {
			t.Errorf("itemType: %q", tp)
		}
		return &provider.MetadataResult{Title: "X", Year: 2020}, nil
	}}
	env.handler = NewProviderHandler(mgr, env.repo, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/providers/metadata/{externalId}", env.handler.GetMetadata)
	env.router = r

	rr := env.do(http.MethodGet, "/api/v1/providers/metadata/42?type=series", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
}

type getMetadataMgr struct {
	fakeProviderManager
	fn func(ctx context.Context, id string, tp provider.ItemType) (*provider.MetadataResult, error)
}

func (m *getMetadataMgr) FetchMetadata(ctx context.Context, id string, tp provider.ItemType) (*provider.MetadataResult, error) {
	return m.fn(ctx, id, tp)
}

// ─── GetImages ──────────────────────────────────────────────────────────────

func TestProviderHandler_GetImages_MissingIDs_400(t *testing.T) {
	env := newProviderTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/providers/images", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_GetImages_HappyPath(t *testing.T) {
	env := newProviderTestEnv(t)
	env.mgr.fetchImagesFn = func(_ context.Context, ids map[string]string, tp provider.ItemType) ([]provider.ImageResult, error) {
		if ids["tmdb"] != "42" {
			t.Errorf("ids: %v", ids)
		}
		if tp != provider.ItemMovie {
			t.Errorf("default type should be movie, got %q", tp)
		}
		return []provider.ImageResult{{URL: "a", Type: "primary"}, {URL: "b", Type: "backdrop"}}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/providers/images?tmdb=42", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := provDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Errorf("images: %d", len(data))
	}
}

// ─── SearchSubtitles ────────────────────────────────────────────────────────

func TestProviderHandler_SearchSubtitles_MissingTitle_400(t *testing.T) {
	env := newProviderTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/providers/search/subtitles", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestProviderHandler_SearchSubtitles_Happy(t *testing.T) {
	env := newProviderTestEnv(t)
	mgr := &searchSubsMgr{fn: func(_ context.Context, q provider.SubtitleQuery) ([]provider.SubtitleResult, error) {
		if q.Title != "Dune" {
			t.Errorf("title: %q", q.Title)
		}
		if len(q.Languages) != 2 || q.Languages[0] != "en" || q.Languages[1] != "es" {
			t.Errorf("languages parsed: %v", q.Languages)
		}
		if q.ExternalIDs["imdb"] != "tt123" {
			t.Errorf("imdb: %q", q.ExternalIDs["imdb"])
		}
		return []provider.SubtitleResult{{Language: "en", Format: "srt", URL: "u", Source: "open"}}, nil
	}}
	env.handler = NewProviderHandler(mgr, env.repo, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/providers/search/subtitles", env.handler.SearchSubtitles)
	env.router = r

	rr := env.do(http.MethodGet, "/api/v1/providers/search/subtitles?title=Dune&languages=en,es&imdb=tt123", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
}

type searchSubsMgr struct {
	fakeProviderManager
	fn func(ctx context.Context, q provider.SubtitleQuery) ([]provider.SubtitleResult, error)
}

func (m *searchSubsMgr) SearchSubtitles(ctx context.Context, q provider.SubtitleQuery) ([]provider.SubtitleResult, error) {
	return m.fn(ctx, q)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func TestMaskAPIKey(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"short":            "****",
		"abcd1234efgh":     "abcd****efgh",
		"abcd1234efgh5678": "abcd****5678",
	}
	for in, want := range cases {
		if got := maskAPIKey(in); got != want {
			t.Errorf("maskAPIKey(%q) = %q want %q", in, got, want)
		}
	}
}

func TestSplitComma(t *testing.T) {
	got := splitComma(" en , es,fr ")
	want := []string{"en", "es", "fr"}
	if len(got) != len(want) {
		t.Fatalf("len: %d", len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q want %q", i, got[i], want[i])
		}
	}
	if len(splitComma("")) != 0 {
		t.Errorf("empty should yield empty slice")
	}
	// Suppress unused-import warning for domain in edge cases.
	_ = domain.ErrNotFound
}
