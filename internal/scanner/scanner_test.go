package scanner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/probe"
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

	s := New(itemRepo, streamRepo, prober, bus, slog.Default())
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
