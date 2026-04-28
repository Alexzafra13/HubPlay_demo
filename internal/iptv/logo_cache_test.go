package iptv

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hubplay/internal/imaging"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func newTestLogoCache(t *testing.T) *LogoCache {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewLogoCache(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewLogoCache: %v", err)
	}
	return c
}

// allowLoopback unblocks 127.0.0.0/8 for the duration of the test so
// httptest servers (which always bind to loopback) survive the
// SSRF guard inside imaging.SafeGet. Identical pattern to the
// imaging package's own ingest tests.
func allowLoopback(t *testing.T) {
	t.Helper()
	prev := imaging.BlockedIP
	imaging.BlockedIP = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}
		return prev(ip)
	}
	t.Cleanup(func() { imaging.BlockedIP = prev })
}

func TestLogoCache_FetchesAndCachesUpstream(t *testing.T) {
	allowLoopback(t)
	body := tinyPNG(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := newTestLogoCache(t)

	// First request: miss → upstream fetch + write.
	path1, err := c.Path(context.Background(), srv.URL+"/logo.png")
	if err != nil {
		t.Fatalf("first Path: %v", err)
	}
	got, err := os.ReadFile(path1)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("cached bytes mismatch: got %d bytes, want %d", len(got), len(body))
	}

	// Second request with the same URL: hit → no extra upstream
	// hit, same path returned.
	path2, err := c.Path(context.Background(), srv.URL+"/logo.png")
	if err != nil {
		t.Fatalf("second Path: %v", err)
	}
	if path1 != path2 {
		t.Errorf("cached path mismatch: %q vs %q", path1, path2)
	}
	if hits != 1 {
		t.Errorf("upstream hits: got %d want 1 (cache miss on second call)", hits)
	}
}

func TestLogoCache_DistinctURLsGetDistinctFiles(t *testing.T) {
	allowLoopback(t)
	body := tinyPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := newTestLogoCache(t)
	a, err := c.Path(context.Background(), srv.URL+"/a.png")
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := c.Path(context.Background(), srv.URL+"/b.png")
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a == b {
		t.Errorf("expected distinct cache files for distinct URLs, both got %q", a)
	}
}

func TestLogoCache_EmptyURL_ReturnsUnavailable(t *testing.T) {
	c := newTestLogoCache(t)
	_, err := c.Path(context.Background(), "")
	if !errors.Is(err, ErrLogoUnavailable) {
		t.Errorf("expected ErrLogoUnavailable, got %v", err)
	}
}

func TestLogoCache_UpstreamError_ReturnsUnavailable(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newTestLogoCache(t)
	_, err := c.Path(context.Background(), srv.URL+"/logo.png")
	if !errors.Is(err, ErrLogoUnavailable) {
		t.Errorf("expected ErrLogoUnavailable, got %v", err)
	}
}

func TestLogoCache_NonImageBody_Rejected(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>404 page from cdn</body></html>"))
	}))
	t.Cleanup(srv.Close)

	c := newTestLogoCache(t)
	_, err := c.Path(context.Background(), srv.URL+"/fake.png")
	if !errors.Is(err, ErrLogoUnavailable) {
		t.Errorf("expected ErrLogoUnavailable for non-image body, got %v", err)
	}
	// And the file must NOT have been written: a future request for
	// the same URL should re-fetch (and the cache dir should stay
	// empty of stale junk).
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		t.Fatalf("readdir cache: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cache dir not empty after rejected fetch: %v", names)
	}
}

func TestLogoCache_RejectsUnsafeURL(t *testing.T) {
	c := newTestLogoCache(t)
	// Without allowLoopback, the SafeGet SSRF guard blocks the
	// loopback address, surfacing as ErrLogoUnavailable.
	_, err := c.Path(context.Background(), "http://127.0.0.1/anything")
	if !errors.Is(err, ErrLogoUnavailable) {
		t.Errorf("expected ErrLogoUnavailable for SSRF target, got %v", err)
	}
}

func TestLogoCache_FilenameIsHexDigest(t *testing.T) {
	c := newTestLogoCache(t)
	got := c.cachedPathFor("https://example.com/logo.png")
	name := filepath.Base(got)
	if len(name) != 32 {
		t.Errorf("filename length: got %d want 32 (16 bytes hex)", len(name))
	}
	for _, r := range name {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("non-hex char %q in filename %q", r, name)
		}
	}
}
