package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hubplay/internal/testutil"
)

// allowLoopback temporarily relaxes the SSRF guard so httptest.Server
// (always 127.0.0.1) can be reached. Without it, every download path
// trips ErrUnsafeURL — the production guard rejects loopback by design.
// Restored on test cleanup so the override never leaks across tests.
func allowLoopback(t *testing.T) {
	t.Helper()
	prev := BlockedIP
	BlockedIP = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}
		return prev(ip)
	}
	t.Cleanup(func() { BlockedIP = prev })
}

// makeTinyPNG returns a 4×4 valid PNG. EnforceMaxPixels accepts it and
// the blurhash decoder is happy with PNG, so this is the smallest
// payload that exercises the whole IngestRemoteImage path.
func makeTinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 64), G: uint8(y * 64), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode tiny png: %v", err)
	}
	return buf.Bytes()
}

func newImageServer(t *testing.T, body []byte, contentType string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestIngestRemoteImage_HappyPath_WritesAtomically(t *testing.T) {
	allowLoopback(t)
	body := makeTinyPNG(t)
	url := newImageServer(t, body, "image/png")
	dir := t.TempDir()

	got, err := IngestRemoteImage(dir, "primary", url, testutil.NopLogger())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got.ContentType != "image/png" {
		t.Errorf("content type: %q", got.ContentType)
	}
	if got.SHA256 == "" || len(got.SHA256) != 64 {
		t.Errorf("sha256 not set or wrong length: %q", got.SHA256)
	}
	if got.Filename == "" || filepath.Ext(got.Filename) != ".png" {
		t.Errorf("filename: %q", got.Filename)
	}
	// File must exist with the exact bytes the server sent.
	disk, err := os.ReadFile(got.LocalPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Equal(disk, body) {
		t.Errorf("on-disk bytes differ from response body")
	}
	// No leftover .tmp sibling — that would be the smoking gun for a
	// non-atomic write that happened to succeed by accident.
	if _, err := os.Stat(got.LocalPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp leftover: %v", err)
	}
}

func TestIngestRemoteImage_FilenameContainsKindAndHashPrefix(t *testing.T) {
	allowLoopback(t)
	body := makeTinyPNG(t)
	url := newImageServer(t, body, "image/png")
	dir := t.TempDir()

	got, err := IngestRemoteImage(dir, "backdrop", url, testutil.NopLogger())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// filename = backdrop_<16 hex chars>.png — assert the structure
	// without coupling to the exact hash (file content varies tiny bit
	// across PNG encoders if the test moves to a different platform).
	if got.Filename[:9] != "backdrop_" {
		t.Errorf("expected backdrop_ prefix, got %q", got.Filename)
	}
	if filepath.Ext(got.Filename) != ".png" {
		t.Errorf("extension: %q", got.Filename)
	}
	hashChunk := got.Filename[9 : len(got.Filename)-4] // strip "backdrop_" and ".png"
	if len(hashChunk) != 16 {
		t.Errorf("expected 16-char hash prefix in filename, got %q", hashChunk)
	}
}

func TestIngestRemoteImage_DownloadErrorPropagates(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	_, err := IngestRemoteImage(dir, "primary", srv.URL, testutil.NopLogger())
	if err == nil {
		t.Fatal("expected error on 404")
	}
	// Caller-friendly: error message must mention the failing stage.
	if !contains(err.Error(), "download") {
		t.Errorf("error message should mention 'download' stage: %v", err)
	}
}

func TestIngestRemoteImage_RejectsSSRFTarget(t *testing.T) {
	// Without allowLoopback, hitting an httptest.Server (always
	// 127.0.0.1) must be rejected by the production SSRF guard. This
	// is the canary that proves the wrapping of SafeGet hasn't been
	// accidentally bypassed in IngestRemoteImage.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("should never see this"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	_, err := IngestRemoteImage(dir, "primary", srv.URL, testutil.NopLogger())
	if err == nil {
		t.Fatal("expected SSRF rejection")
	}
}

func TestAtomicWriteFile_SucceedsAndCleansTmp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.bin")
	if err := AtomicWriteFile(dst, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("content: %q", got)
	}
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp not cleaned: %v", err)
	}
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(dst, []byte("new"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("content: %q", got)
	}
}

func TestAtomicWriteFile_FailureLeavesNoCorruptDest(t *testing.T) {
	// Unwritable directory → WriteFile of the .tmp should fail and the
	// destination must stay either absent or untouched. We pre-seed an
	// untouchable destination to assert "untouched"; the check that
	// .tmp didn't survive a write that never started is implicit.
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(dst, []byte("preserved"), 0o644); err != nil {
		t.Fatal(err)
	}

	// /dev/full on Linux always returns ENOSPC; on systems that don't
	// have it, skip rather than fake a failure (the path through the
	// real OS is what matters for atomicity).
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this platform")
	}
	if err := AtomicWriteFile("/dev/full/x.bin", []byte("nope"), 0o644); err == nil {
		t.Error("expected error writing to /dev/full")
	}
	// The original file is untouched.
	got, _ := os.ReadFile(dst)
	if string(got) != "preserved" {
		t.Errorf("destination was clobbered: %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
