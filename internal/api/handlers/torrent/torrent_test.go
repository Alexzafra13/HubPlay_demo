package torrenthandler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	h := NewHandler(nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/search", nil)
	h.Search(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_MissingSource(t *testing.T) {
	h := NewHandler(nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestStream_InvalidSource(t *testing.T) {
	h := NewHandler(nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=https://example.com/x.torrent", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400 for disallowed host", rr.Code)
	}
}

func TestStream_DisabledWhenNoManager(t *testing.T) {
	// Valid magnet but no manager wired → 503 (feature off), not a panic.
	h := NewHandler(nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/torrent/stream?src=magnet:?xt=urn:btih:abc", nil)
	h.Stream(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}
