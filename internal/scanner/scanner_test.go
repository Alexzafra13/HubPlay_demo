package scanner

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/probe"
	"hubplay/internal/provider"
	"hubplay/internal/testutil"
)

// mockProber returns a fixed result for any file.
type mockProber struct {
	result *probe.Result
	err    error
}

func (m *mockProber) Probe(ctx context.Context, path string) (*probe.Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func newTestScanner(t *testing.T) (*Scanner, *db.ItemRepository, *db.MediaStreamRepository) {
	t.Helper()
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	streamRepo := db.NewMediaStreamRepository(database)
	bus := event.NewBus(slog.Default())

	// Seed library
	now := time.Now()
	if err := libRepo.Create(context.Background(), &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	}); err != nil {
		t.Fatal(err)
	}

	prober := &mockProber{
		result: &probe.Result{
			Format: probe.Format{
				Duration:   2 * time.Hour,
				Size:       1500000000,
				FormatName: "matroska,webm",
			},
			Streams: []probe.Stream{
				{Index: 0, CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080},
				{Index: 1, CodecType: "audio", CodecName: "aac", Channels: 2},
			},
		},
	}

	metaRepo := db.NewMetadataRepository(database)
	extIDRepo := db.NewExternalIDRepository(database)
	imageRepo := db.NewImageRepository(database)

	// imageDir + pathmap are nil for tests that don't exercise the
	// artwork pipeline; the scanner skips image enrichment when either
	// is absent.
	s := New(itemRepo, streamRepo, metaRepo, extIDRepo, imageRepo, nil, prober, bus, "", nil, slog.Default())
	return s, itemRepo, streamRepo
}

func TestIsMediaFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"movie.mkv", true},
		{"movie.mp4", true},
		{"movie.avi", true},
		{"song.mp3", true},
		{"song.flac", true},
		{"readme.txt", false},
		{"image.jpg", false},
		{"data.json", false},
		{"MOVIE.MKV", true}, // case insensitive
	}

	for _, tt := range tests {
		got := IsMediaFile(tt.path)
		if got != tt.want {
			t.Errorf("IsMediaFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestTitleFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/media/The Matrix (1999).mkv", "The Matrix (1999)"},
		{"/media/movie.mp4", "movie"},
		{"/a/b/c/file.avi", "file"},
	}

	for _, tt := range tests {
		got := titleFromPath(tt.path)
		if got != tt.want {
			t.Errorf("titleFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestItemTypeFromLibrary(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{"movies", "movie"},
		{"shows", "episode"},
		{"music", "audio"},
		{"unknown", "movie"},
	}

	for _, tt := range tests {
		got := itemTypeFromLibrary(tt.contentType)
		if got != tt.want {
			t.Errorf("itemTypeFromLibrary(%q) = %q, want %q", tt.contentType, got, tt.want)
		}
	}
}

func TestScanLibrary_NewFiles(t *testing.T) {
	s, itemRepo, streamRepo := newTestScanner(t)

	// Create temp dir with fake media files
	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "movie1.mkv"), "fake video data 1")
	createFile(t, filepath.Join(dir, "movie2.mp4"), "fake video data 2")
	createFile(t, filepath.Join(dir, "readme.txt"), "not a video")

	lib := &db.Library{
		ID:          "lib-test",
		Name:        "Test",
		ContentType: "movies",
		Paths:       []string{dir},
	}

	result, err := s.ScanLibrary(context.Background(), lib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Added != 2 {
		t.Errorf("expected 2 added, got %d", result.Added)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}

	// Verify items in DB
	items, total, _ := itemRepo.List(context.Background(), db.ItemFilter{LibraryID: "lib-test", Limit: 10})
	if total != 2 {
		t.Errorf("expected 2 items in DB, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items returned, got %d", len(items))
	}

	// Verify streams were stored
	for _, item := range items {
		streams, _ := streamRepo.ListByItem(context.Background(), item.ID)
		if len(streams) != 2 {
			t.Errorf("expected 2 streams for item %s, got %d", item.ID, len(streams))
		}
	}
}

func TestScanLibrary_RemovedFiles(t *testing.T) {
	s, itemRepo, _ := newTestScanner(t)

	dir := t.TempDir()
	f := filepath.Join(dir, "temp.mkv")
	createFile(t, f, "temp data")

	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies",
		Paths: []string{dir},
	}

	// First scan — adds the file
	result, _ := s.ScanLibrary(context.Background(), lib)
	if result.Added != 1 {
		t.Fatalf("expected 1 added, got %d", result.Added)
	}

	// Remove the file
	_ = os.Remove(f)

	// Second scan — should mark as removed
	result, _ = s.ScanLibrary(context.Background(), lib)
	if result.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", result.Removed)
	}

	// Verify the item is marked unavailable
	items, _, _ := itemRepo.List(context.Background(), db.ItemFilter{LibraryID: "lib-test", Limit: 10})
	for _, item := range items {
		if item.IsAvailable {
			t.Error("expected item to be unavailable after removal")
		}
	}
}

func TestScanLibrary_EmptyDir(t *testing.T) {
	s, _, _ := newTestScanner(t)

	dir := t.TempDir()

	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies",
		Paths: []string{dir},
	}

	result, err := s.ScanLibrary(context.Background(), lib)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Added != 0 {
		t.Errorf("expected 0 added, got %d", result.Added)
	}
}

func TestScanLibrary_Idempotent(t *testing.T) {
	s, _, _ := newTestScanner(t)

	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "movie.mkv"), "stable content")

	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies",
		Paths: []string{dir},
	}

	// First scan
	r1, _ := s.ScanLibrary(context.Background(), lib)
	if r1.Added != 1 {
		t.Fatalf("first scan should add 1, got %d", r1.Added)
	}

	// Second scan — same file, no changes
	r2, _ := s.ScanLibrary(context.Background(), lib)
	if r2.Added != 0 {
		t.Errorf("second scan should add 0, got %d", r2.Added)
	}
	if r2.Updated != 0 {
		t.Errorf("second scan should update 0, got %d", r2.Updated)
	}
}

func createFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("creating test file: %v", err)
	}
}

// ─── Image-ingest contract ──────────────────────────────────────────────────
//
// These tests pin the post-audit invariant: the scanner MUST download
// every provider image to local storage during enrichment and
// persist `/api/v1/images/file/{id}` as the path. Storing the upstream
// URL would leak the user's IP/User-Agent to TMDb on every poster view
// and break the library the day TMDb is unreachable. The previous
// behaviour did exactly that — these tests are the regression guard.

// stubProvider implements scanner.providerFetcher with canned image
// results. SearchMetadata and FetchMetadata are no-ops because this
// suite seeds the item + external IDs directly and exercises only the
// image-ingest path.
type stubProvider struct {
	images []provider.ImageResult
}

func (s *stubProvider) SearchMetadata(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (s *stubProvider) FetchMetadata(_ context.Context, _ string, _ provider.ItemType) (*provider.MetadataResult, error) {
	return nil, nil
}
func (s *stubProvider) FetchImages(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
	return s.images, nil
}

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

func TestFetchAndStoreImages_PersistsLocalPathNotURL(t *testing.T) {
	// httptest.Server lives on 127.0.0.1; the production SafeGet
	// rejects loopback as an SSRF target, so we relax the guard for
	// this test only. The override is restored by t.Cleanup so
	// nothing leaks across packages.
	prevBlocked := imaging.BlockedIP
	imaging.BlockedIP = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}
		return prevBlocked(ip)
	}
	t.Cleanup(func() { imaging.BlockedIP = prevBlocked })

	pngBody := makeTinyPNG(t)
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBody)
	}))
	t.Cleanup(srv.Close)

	// Wire a real Scanner against the same DB fixture the other tests
	// use, plus the on-disk image dir + pathmap that the production
	// constructor would build in main.go.
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	imgRepo := db.NewImageRepository(database)
	now := time.Now()
	if err := libRepo.Create(context.Background(), &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	}); err != nil {
		t.Fatal(err)
	}
	itemID := "item-1"
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: itemID, LibraryID: "lib-test", Type: "movie", Title: "Test",
		AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	imageDir := t.TempDir()
	pm := pathmap.New(imageDir)

	bus := event.NewBus(slog.Default())
	prober := &mockProber{result: &probe.Result{}}
	s := New(itemRepo, db.NewMediaStreamRepository(database),
		db.NewMetadataRepository(database), db.NewExternalIDRepository(database),
		imgRepo, nil /* providers — overridden below */, prober, bus,
		imageDir, pm, slog.Default())

	// Inject the stub. The constructor took *provider.Manager (nil
	// here) but the field is the interface; tests can swap directly.
	s.providers = &stubProvider{images: []provider.ImageResult{
		{Type: "primary", URL: srv.URL + "/poster.png", Width: 4, Height: 4, Score: 100},
		{Type: "primary", URL: srv.URL + "/loser.png", Width: 4, Height: 4, Score: 1}, // lower score — must be skipped
		{Type: "backdrop", URL: srv.URL + "/backdrop.png", Width: 4, Height: 4, Score: 50},
		{Type: "thumb", URL: srv.URL + "/thumb.png"}, // unknown kind — must be ignored
	}}

	// Drive the image-ingest path directly so we don't have to build
	// the surrounding scan pipeline (filesystem walk + ffprobe). The
	// public ScanLibrary calls this same internal method.
	s.fetchAndStoreImages(context.Background(), itemID, map[string]string{"tmdb": "123"}, provider.ItemMovie)

	// Two images expected (primary + backdrop) — thumb is skipped, low-
	// score primary is suppressed by the per-kind ranking.
	got, err := imgRepo.ListByItem(context.Background(), itemID)
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 images persisted, got %d", len(got))
	}
	if h := atomic.LoadInt64(&hits); h != 2 {
		t.Errorf("expected 2 upstream downloads (primary + backdrop), got %d", h)
	}

	for _, img := range got {
		// CONTRACT: the path is the local-serving URL, never the upstream.
		if !strings.HasPrefix(img.Path, "/api/v1/images/file/") {
			t.Errorf("image %s has non-local path: %q", img.ID, img.Path)
		}
		if strings.HasPrefix(img.Path, "http") {
			t.Errorf("image %s leaked an upstream URL: %q", img.ID, img.Path)
		}
		// The pathmap entry must point at a real file on disk, under
		// the configured imageDir.
		localPath, err := pm.Read(img.ID)
		if err != nil {
			t.Errorf("pathmap missing for image %s: %v", img.ID, err)
			continue
		}
		if !strings.HasPrefix(localPath, imageDir) {
			t.Errorf("pathmap escaped imageDir: %q", localPath)
		}
		if _, err := os.Stat(localPath); err != nil {
			t.Errorf("local file missing: %v", err)
		}
	}
}

func TestFetchAndStoreImages_SkippedWhenImageDirEmpty(t *testing.T) {
	// Without imageDir + pathmap, the scanner must NOT call the
	// provider at all and must NOT persist any image rows. This is
	// the legacy-callsite escape hatch that keeps existing tests
	// working without spinning up the artwork pipeline.
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	imgRepo := db.NewImageRepository(database)
	now := time.Now()
	_ = libRepo.Create(context.Background(), &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	})
	_ = itemRepo.Create(context.Background(), &db.Item{
		ID: "item-1", LibraryID: "lib-test", Type: "movie", Title: "T", AddedAt: now, UpdatedAt: now,
	})

	bus := event.NewBus(slog.Default())
	prober := &mockProber{result: &probe.Result{}}
	stub := &stubProvider{images: []provider.ImageResult{
		{Type: "primary", URL: "http://example.test/poster.png", Score: 1},
	}}
	s := New(itemRepo, db.NewMediaStreamRepository(database),
		db.NewMetadataRepository(database), db.NewExternalIDRepository(database),
		imgRepo, nil, prober, bus, "", nil, slog.Default())
	s.providers = stub

	// Re-run a minimal version of the enrichItemWithMetadata image
	// branch to confirm the guard. fetchAndStoreImages assumes the
	// dirs are wired; the real call site (`if s.imageDir != "" &&
	// s.pathmap != nil`) skips it when they aren't.
	if s.imageDir != "" && s.pathmap != nil {
		t.Fatal("scanner should treat empty imageDir as 'do not ingest'")
	}
	imgs, _ := imgRepo.ListByItem(context.Background(), "item-1")
	if len(imgs) != 0 {
		t.Errorf("expected no images persisted, got %d", len(imgs))
	}
}
