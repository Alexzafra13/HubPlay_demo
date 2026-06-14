package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/torrentstream"
)

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
	cases := map[string]bool{
		"magnet:?xt=urn:btih:abc":                                    true,
		"https://archive.org/download/sintel/sintel_archive.torrent": true,
		"https://ia800000.us.archive.org/x/sintel_archive.torrent":   true,
		"https://example.com/evil.torrent":                           false,
		"http://archive.org/x.torrent":                               false, // only https archive.org
		"http://169.254.169.254/latest/meta-data":                    false, // SSRF target
		"":                    false,
		"ftp://archive.org/x": false,
	}
	for src, want := range cases {
		if got := isAllowedSource(src); got != want {
			t.Errorf("isAllowedSource(%q): got %v want %v", src, got, want)
		}
	}
}

func TestSearch_MissingQuery(t *testing.T) {
	h := NewHandler(&fakeManager{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/search", nil)
	h.Search(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_MissingSource(t *testing.T) {
	h := NewHandler(&fakeManager{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_InvalidSource(t *testing.T) {
	h := NewHandler(&fakeManager{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=https://example.com/x.torrent", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400 for disallowed host", rr.Code)
	}
}

func TestStream_DisabledWhenNoManager(t *testing.T) {
	h := NewHandler(nil, adminFalse, nil)
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
	h := NewHandler(fm, adminFalse, nil)
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
	h := NewHandler(fm, adminTrue, nil)
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
