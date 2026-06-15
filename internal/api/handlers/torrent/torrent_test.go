package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hubplay/internal/provider"
	"hubplay/internal/torrentstream"
)

// fakeMeta stands in for the TMDb-backed metadata searcher.
type fakeMeta struct {
	results []provider.SearchResult
	err     error
}

func (f fakeMeta) SearchMetadata(context.Context, provider.SearchQuery) ([]provider.SearchResult, error) {
	return f.results, f.err
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
	h := NewHandler(&fakeManager{}, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/search", nil)
	h.Search(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_MissingSource(t *testing.T) {
	h := NewHandler(&fakeManager{}, nil, adminFalse, nil)
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
	h := NewHandler(&fakeManager{}, nil, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=ftp://example.com/x.torrent", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400 for disallowed scheme", rr.Code)
	}
}

func TestStream_DisabledWhenNoManager(t *testing.T) {
	h := NewHandler(nil, nil, adminFalse, nil)
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
	h := NewHandler(fm, nil, adminFalse, nil)
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
	h := NewHandler(fm, nil, adminTrue, nil)
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
	h := NewHandler(&fakeManager{}, fakeMeta{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/discover", nil)
	h.Discover(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestDiscover_NoProvider(t *testing.T) {
	// meta nil → TMDb not configured → 503.
	h := NewHandler(&fakeManager{}, nil, adminFalse, nil)
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
	h := NewHandler(&fakeManager{}, meta, adminFalse, nil)
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
