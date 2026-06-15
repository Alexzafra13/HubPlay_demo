package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/provider"
	"hubplay/internal/torrentstream"
)

// fakeMeta stands in for the TMDb-backed metadata searcher.
type fakeMeta struct {
	results []provider.SearchResult
	err     error
	meta    *provider.MetadataResult
}

func (f fakeMeta) SearchMetadata(context.Context, provider.SearchQuery) ([]provider.SearchResult, error) {
	return f.results, f.err
}

func (f fakeMeta) FetchMetadata(context.Context, string, provider.ItemType) (*provider.MetadataResult, error) {
	return f.meta, f.err
}

// fakeManager stands in for the live engine. The serve path needs a real
// torrent reader (network), so these tests only cover the decision logic
// that returns before serving.
type fakeManager struct {
	active      bool
	startErr    error
	startCalled bool
}

func (f *fakeManager) Search(context.Context, string, int) ([]torrentstream.SearchResult, error) {
	return nil, nil
}
func (f *fakeManager) GetActive(string) (*torrentstream.Session, bool) {
	return nil, f.active
}
func (f *fakeManager) GetOrStart(context.Context, string) (*torrentstream.Session, error) {
	f.startCalled = true
	return nil, f.startErr // tests use a non-nil err so we never reach the reader
}

func adminTrue(*http.Request) bool  { return true }
func adminFalse(*http.Request) bool { return false }

func TestIsAllowedSource(t *testing.T) {
	// isAllowedSource gates the scheme only: magnet or http(s). It is NOT
	// catalogue-restricted — any public source is accepted. SSRF safety
	// for http(s) URLs (blocking 169.254.x / LAN / loopback) is enforced
	// downstream by imaging.SafeGet in the engine, not here.
	cases := map[string]bool{
		"magnet:?xt=urn:btih:abc":                                    true,
		"https://archive.org/download/sintel/sintel_archive.torrent": true,
		"https://example.com/any.torrent":                            true,
		"http://my-tracker.example/x.torrent":                        true,
		"":                                                           false,
		"ftp://example.com/x":                                        false,
		"file:///etc/passwd":                                         false,
		"notaurl":                                                    false,
	}
	for src, want := range cases {
		if got := isAllowedSource(src); got != want {
			t.Errorf("isAllowedSource(%q): got %v want %v", src, got, want)
		}
	}
}

func TestSearch_MissingQuery(t *testing.T) {
	h := NewHandler(&fakeManager{}, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/search", nil)
	h.Search(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_MissingSource(t *testing.T) {
	h := NewHandler(&fakeManager{}, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_InvalidSource(t *testing.T) {
	// A non-magnet, non-http(s) scheme is rejected at the handler (400);
	// SSRF for http(s) is handled later by the engine, not here.
	h := NewHandler(&fakeManager{}, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=ftp://example.com/x.torrent", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400 for disallowed scheme", rr.Code)
	}
}

func TestStream_DisabledWhenNoManager(t *testing.T) {
	h := NewHandler(nil, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=magnet:?xt=urn:btih:abc", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

// A non-admin asking for content that isn't active must be refused (403)
// and must NOT trigger a download.
func TestStream_NonAdmin_NotActive_Forbidden(t *testing.T) {
	fm := &fakeManager{active: false}
	h := NewHandler(fm, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=magnet:?xt=urn:btih:abc", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", rr.Code)
	}
	if fm.startCalled {
		t.Error("non-admin must not trigger a download (GetOrStart)")
	}
}

// An admin reaches GetOrStart; we feed it ErrTooManySessions to assert the
// busy mapping without needing a live torrent reader.
func TestStream_Admin_StartsAndMapsBusy(t *testing.T) {
	fm := &fakeManager{startErr: torrentstream.ErrTooManySessions}
	h := NewHandler(fm, nil, nil, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=magnet:?xt=urn:btih:abc", nil)
	h.Stream(rr, req)
	if !fm.startCalled {
		t.Error("admin should reach GetOrStart")
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503 (busy)", rr.Code)
	}
}

func TestDiscover_MissingQuery(t *testing.T) {
	h := NewHandler(&fakeManager{}, nil, fakeMeta{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover", nil)
	h.Discover(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestDiscover_NoProvider(t *testing.T) {
	// meta nil → TMDb not configured → 503.
	h := NewHandler(&fakeManager{}, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover?q=nosferatu", nil)
	h.Discover(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

func TestDiscover_ReturnsMappedResults(t *testing.T) {
	meta := fakeMeta{results: []provider.SearchResult{
		{ExternalID: "653", Title: "Nosferatu", Year: 1922, Overview: "vamp", PosterURL: "https://image.tmdb.org/p.jpg"},
	}}
	h := NewHandler(&fakeManager{}, nil, meta, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover?q=nosferatu", nil)
	h.Discover(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"tmdb_id":"653"`) ||
		!strings.Contains(rr.Body.String(), `"poster_url":"https://image.tmdb.org/p.jpg"`) {
		t.Errorf("body missing mapped fields: %s", rr.Body.String())
	}
}

// fakeSources stands in for the Torznab aggregator.
type fakeSources struct {
	res    []torrentstream.SearchResult
	err    error
	gotMT  torrentstream.MediaType
	gotID  string
	called bool
}

func (f *fakeSources) Sources(_ context.Context, mt torrentstream.MediaType, imdbID string) ([]torrentstream.SearchResult, error) {
	f.called = true
	f.gotMT = mt
	f.gotID = imdbID
	return f.res, f.err
}

func (f *fakeSources) SearchText(_ context.Context, mt torrentstream.MediaType, query string) ([]torrentstream.SearchResult, error) {
	f.called = true
	f.gotMT = mt
	f.gotID = query
	return f.res, f.err
}

func TestSourcesSearch(t *testing.T) {
	fs := &fakeSources{res: []torrentstream.SearchResult{{Identifier: "x", Title: "Movie 1080p", Seeders: 5}}}
	h := NewHandler(nil, fs, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/sources/search?q=matrix&type=series", nil)
	h.SourcesSearch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if fs.gotMT != torrentstream.MediaTypeSeries || fs.gotID != "matrix" {
		t.Errorf("forwarded args: mt=%q q=%q", fs.gotMT, fs.gotID)
	}
}

func TestSourcesSearch_RateLimited(t *testing.T) {
	fs := &fakeSources{err: torrentstream.ErrRateLimited}
	h := NewHandler(nil, fs, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/sources/search?q=matrix", nil)
	h.SourcesSearch(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503 (%s)", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on rate-limited response")
	}
	if !strings.Contains(rr.Body.String(), "RATE_LIMITED") {
		t.Errorf("body: %s", rr.Body.String())
	}
}

func TestSourcesSearch_MissingQuery(t *testing.T) {
	h := NewHandler(nil, &fakeSources{}, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/sources/search", nil)
	h.SourcesSearch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

// withIMDb attaches a chi route param so the handler can read {imdbId}.
func withIMDb(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("imdbId", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestSources_InvalidIMDb(t *testing.T) {
	h := NewHandler(nil, &fakeSources{}, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withIMDb(httptest.NewRequest(http.MethodGet, "/torrent/sources/movie/bad", nil), "bad")
	h.SourcesMovie(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestSources_DisabledWhenNoIndexer(t *testing.T) {
	h := NewHandler(nil, nil, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withIMDb(httptest.NewRequest(http.MethodGet, "/torrent/sources/movie/tt0133093", nil), "tt0133093")
	h.SourcesMovie(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

func TestSources_MovieReturnsData(t *testing.T) {
	fs := &fakeSources{res: []torrentstream.SearchResult{
		{Identifier: "abc", Title: "The Matrix 1080p", Seeders: 42, Quality: "1080p", MagnetURI: "magnet:?xt=urn:btih:abc"},
	}}
	h := NewHandler(nil, fs, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withIMDb(httptest.NewRequest(http.MethodGet, "/torrent/sources/movie/tt0133093", nil), "tt0133093")
	h.SourcesMovie(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if fs.gotMT != torrentstream.MediaTypeMovie || fs.gotID != "tt0133093" {
		t.Errorf("forwarded wrong args: mt=%q id=%q", fs.gotMT, fs.gotID)
	}
	if !strings.Contains(rr.Body.String(), `"quality":"1080p"`) ||
		!strings.Contains(rr.Body.String(), `"seeders":42`) {
		t.Errorf("body missing fields: %s", rr.Body.String())
	}
}

func TestSources_SeriesForwardsType(t *testing.T) {
	fs := &fakeSources{}
	h := NewHandler(nil, fs, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withIMDb(httptest.NewRequest(http.MethodGet, "/torrent/sources/series/tt0903747", nil), "tt0903747")
	h.SourcesSeries(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if fs.gotMT != torrentstream.MediaTypeSeries {
		t.Errorf("media type: got %q want series", fs.gotMT)
	}
}

func TestDiscoverSources_ResolvesImdbAndSearches(t *testing.T) {
	fm := fakeMeta{meta: &provider.MetadataResult{
		Title:       "The Matrix",
		ExternalIDs: map[string]string{"imdb": "tt0133093"},
	}}
	fs := &fakeSources{res: []torrentstream.SearchResult{{Identifier: "x", Title: "The Matrix 1080p"}}}
	h := NewHandler(nil, fs, fm, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover/sources?type=movie&tmdb_id=603", nil)
	h.DiscoverSources(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if fs.gotID != "tt0133093" {
		t.Errorf("expected sources resolved by imdbid, got %q", fs.gotID)
	}
}

func TestDiscoverSources_MissingTmdbID(t *testing.T) {
	h := NewHandler(nil, &fakeSources{}, fakeMeta{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover/sources?type=movie", nil)
	h.DiscoverSources(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestSources_IndexerError(t *testing.T) {
	fs := &fakeSources{err: context.DeadlineExceeded}
	h := NewHandler(nil, fs, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withIMDb(httptest.NewRequest(http.MethodGet, "/torrent/sources/movie/tt0133093", nil), "tt0133093")
	h.SourcesMovie(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d want 502", rr.Code)
	}
}
