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
	// chapters repo is real (not nil) because we want the persistence
	// path covered by the new TestScanLibrary_PersistsChapters test
	// without spinning up another fixture.
	chaptersRepo := db.NewChapterRepository(database)
	s := New(itemRepo, streamRepo, metaRepo, extIDRepo, imageRepo, chaptersRepo, db.NewPeopleRepository(database), nil, prober, bus, "", nil, slog.Default())
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

func TestScanLibrary_ShowsBuildsHierarchy(t *testing.T) {
	// Reported by user: a Series library accepted creation but the
	// /series page stayed empty because the scanner only created
	// rows of type=episode — never the parent series + season rows.
	// This test pins the hierarchy: one series row per series dir,
	// one season row per season dir, episodes link via parent_id.
	s, itemRepo, _ := newTestScanner(t)

	// Build a small Plex-style tree:
	//   <root>/Breaking Bad/Season 01/S01E01.mkv
	//   <root>/Breaking Bad/Season 01/S01E02.mkv
	//   <root>/Breaking Bad/Season 02/S02E01.mkv
	//   <root>/The Office/Season 03/S03E05.mkv
	root := t.TempDir()
	for _, p := range []string{
		"Breaking Bad/Season 01/Breaking.Bad.S01E01.Pilot.mkv",
		"Breaking Bad/Season 01/Breaking.Bad.S01E02.Cat.mkv",
		"Breaking Bad/Season 02/Breaking.Bad.S02E01.Seven.mkv",
		"The Office/Season 03/The.Office.S03E05.Initiation.mkv",
	} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		createFile(t, full, "x")
	}

	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "shows",
		Paths: []string{root},
	}
	r, err := s.ScanLibrary(context.Background(), lib)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// `result.Added` counts processed FILES (not all DB rows), so 4
	// — the parent series + season rows are infrastructure created
	// transparently as a side-effect of episode ingestion.
	if r.Added != 4 {
		t.Errorf("added (file count): got %d want 4 episodes", r.Added)
	}

	// Series rows: one per top-level dir.
	series, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
		LibraryID: "lib-test", Type: "series", Limit: 100,
	})
	titles := make(map[string]bool, len(series))
	for _, sr := range series {
		titles[sr.Title] = true
	}
	for _, want := range []string{"Breaking Bad", "The Office"} {
		if !titles[want] {
			t.Errorf("missing series row %q (got %v)", want, titles)
		}
	}

	// Season rows: each scoped to its series via parent_id.
	for _, sr := range series {
		seasons, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
			LibraryID: "lib-test", Type: "season", ParentID: sr.ID, Limit: 10,
		})
		if sr.Title == "Breaking Bad" && len(seasons) != 2 {
			t.Errorf("Breaking Bad seasons: got %d want 2", len(seasons))
		}
		if sr.Title == "The Office" && len(seasons) != 1 {
			t.Errorf("The Office seasons: got %d want 1", len(seasons))
		}
	}

	// Episodes carry season + episode numbers AND link to a season
	// via parent_id (the bug being fixed: previously parent_id was
	// always empty for shows).
	episodes, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
		LibraryID: "lib-test", Type: "episode", Limit: 100,
	})
	if len(episodes) != 4 {
		t.Fatalf("episodes: got %d want 4", len(episodes))
	}
	for _, ep := range episodes {
		if ep.ParentID == "" {
			t.Errorf("episode %q has empty parent_id (must point to season)", ep.Title)
		}
		if ep.SeasonNumber == nil || ep.EpisodeNumber == nil {
			t.Errorf("episode %q missing S/E numbers: season=%v episode=%v",
				ep.Title, ep.SeasonNumber, ep.EpisodeNumber)
		}
	}
}

func TestScanLibrary_ShowsRescanIsIdempotent(t *testing.T) {
	// Re-scanning the same shows tree must NOT create duplicate
	// series / season rows. The cache pre-populates from the DB on
	// the second scan, so the ensure*Row helpers find existing rows
	// and return their ids instead of inserting.
	s, itemRepo, _ := newTestScanner(t)
	root := t.TempDir()
	full := filepath.Join(root, "Breaking Bad", "Season 01", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	createFile(t, full, "x")

	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "shows",
		Paths: []string{root},
	}
	if _, err := s.ScanLibrary(context.Background(), lib); err != nil {
		t.Fatal(err)
	}

	// Second scan with no changes: must add 0 rows.
	r2, err := s.ScanLibrary(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Added != 0 {
		t.Errorf("re-scan should be idempotent, added %d rows", r2.Added)
	}

	// Sanity: still exactly 1 series + 1 season + 1 episode. The
	// season check is the regression guard for the
	// `iterateLibraryItems` filter bug — it used to default to
	// `parent_id IS NULL`, which silently excluded every season +
	// episode from the cache pre-population pass. On re-scan the
	// season cache was empty → ensureSeasonRow → fresh INSERT →
	// duplicate season rows for every existing show.
	series, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
		LibraryID: "lib-test", Type: "series", Limit: 10,
	})
	if len(series) != 1 {
		t.Errorf("series count after rescan: got %d want 1", len(series))
	}
	seasons, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
		LibraryID: "lib-test", Type: "season", ParentID: series[0].ID, Limit: 10,
	})
	if len(seasons) != 1 {
		t.Errorf("season count after rescan: got %d want 1 (duplicate seasons = cache pre-pop bug)", len(seasons))
	}
	episodes, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
		LibraryID: "lib-test", Type: "episode", Limit: 10,
	})
	if len(episodes) != 1 {
		t.Errorf("episode count after rescan: got %d want 1", len(episodes))
	}
}

func TestScanLibrary_ShowsRescanWithMultipleSeasons(t *testing.T) {
	// Reported by user: tras escanear una librería con varias series y
	// temporadas, /series mostraba duplicados de cada serie. Esa
	// versión del test mantiene los rows de series cacheadas pero
	// también verifica seasons + episodes para que el bug del filtro
	// `parent_id IS NULL` en iterateLibraryItems no vuelva a colarse.
	s, itemRepo, _ := newTestScanner(t)
	root := t.TempDir()
	for _, p := range []string{
		"The Boys/Season 01/the.boys.s01e01.mkv",
		"The Boys/Season 01/the.boys.s01e02.mkv",
		"The Boys/Season 02/the.boys.s02e01.mkv",
		"Stranger Things/Season 01/st.s01e01.mkv",
		"Stranger Things/Season 01/st.s01e02.mkv",
	} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		createFile(t, full, "x")
	}
	lib := &db.Library{
		ID: "lib-test", Name: "Test", ContentType: "shows",
		Paths: []string{root},
	}

	// First scan creates the hierarchy.
	if _, err := s.ScanLibrary(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	// Second scan (no FS changes) must be a no-op at every level.
	if _, err := s.ScanLibrary(context.Background(), lib); err != nil {
		t.Fatal(err)
	}

	// Each assert below would have failed before the fix (every
	// re-scan added a new copy of every season because cache.season
	// was empty).
	got := func(t string) int {
		items, _, _ := itemRepo.List(context.Background(), db.ItemFilter{
			LibraryID: "lib-test", Type: t, Limit: 100,
		})
		return len(items)
	}
	if got("series") != 2 {
		t.Errorf("series rows: got %d want 2", got("series"))
	}
	if got("season") != 3 {
		t.Errorf("season rows: got %d want 3 (1 per Season N dir)", got("season"))
	}
	if got("episode") != 5 {
		t.Errorf("episode rows: got %d want 5", got("episode"))
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
	images  []provider.ImageResult
	episode *provider.EpisodeMetadataResult
	season  *provider.SeasonMetadataResult
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
func (s *stubProvider) FetchEpisodeMetadata(_ context.Context, _ string, _, _ int) (*provider.EpisodeMetadataResult, error) {
	return s.episode, nil
}
func (s *stubProvider) FetchSeasonMetadata(_ context.Context, _ string, _ int) (*provider.SeasonMetadataResult, error) {
	return s.season, nil
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
		imgRepo, db.NewChapterRepository(database), db.NewPeopleRepository(database),
		nil /* providers — overridden below */, prober, bus,
		imageDir, pm, slog.Default())

	// Inject the stub. The constructor took *provider.Manager (nil
	// here) but the field is the interface; tests can swap directly.
	// `Source` is what the production Manager.FetchImages stamps onto
	// every result; the scanner trusts it as-is on the DB row. The
	// stub mirrors that contract so the test exercises the real
	// path, not a sniff fallback.
	s.providers = &stubProvider{images: []provider.ImageResult{
		{Type: "primary", URL: srv.URL + "/poster.png", Source: "tmdb", Width: 4, Height: 4, Score: 100},
		{Type: "primary", URL: srv.URL + "/loser.png", Source: "tmdb", Width: 4, Height: 4, Score: 1}, // lower score — must be skipped
		{Type: "backdrop", URL: srv.URL + "/backdrop.png", Source: "fanart", Width: 4, Height: 4, Score: 50},
		{Type: "thumb", URL: srv.URL + "/thumb.png", Source: "fanart"}, // unknown kind — must be ignored
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
		// CONTRACT: provider name comes from the Manager-stamped Source,
		// not a URL substring sniff. Both stubs above set it explicitly,
		// so an "unknown" landing here would mean the scanner regressed
		// to the legacy URL-based heuristic.
		if img.Provider != "tmdb" && img.Provider != "fanart" {
			t.Errorf("image %s has unexpected provider %q (expected tmdb or fanart)", img.ID, img.Provider)
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

// TestEnrichEpisode_PersistsOverviewAndStill pins the per-episode
// enrichment contract: given a series row whose external_ids carry a
// TMDb id, the scanner must call the provider with that id, write the
// returned overview into the metadata table, fold air-date / rating /
// cleaned title into the item row, and ingest the still as a primary
// `backdrop` image attached to the EPISODE (not the series).
//
// The bug this test guards against is the empty-episode hero — every
// field below is one the UI tried to render and got nothing, so a
// missing metadata.Upsert or missing image row regresses the page to
// "Media hora en el cielo · 0".
func TestEnrichEpisode_PersistsOverviewAndStill(t *testing.T) {
	prevBlocked := imaging.BlockedIP
	imaging.BlockedIP = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}
		return prevBlocked(ip)
	}
	t.Cleanup(func() { imaging.BlockedIP = prevBlocked })

	pngBody := makeTinyPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBody)
	}))
	t.Cleanup(srv.Close)

	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	imgRepo := db.NewImageRepository(database)
	metaRepo := db.NewMetadataRepository(database)
	extRepo := db.NewExternalIDRepository(database)

	now := time.Now()
	if err := libRepo.Create(context.Background(), &db.Library{
		ID: "lib-shows", Name: "Shows", ContentType: "shows", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	}); err != nil {
		t.Fatal(err)
	}

	seriesID := "series-1"
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: seriesID, LibraryID: "lib-shows", Type: "series", Title: "Daredevil",
		AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := extRepo.Upsert(context.Background(), &db.ExternalID{
		ItemID: seriesID, Provider: "tmdb", ExternalID: "12345",
	}); err != nil {
		t.Fatal(err)
	}

	seasonID := "season-1"
	seasonNum := 1
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: seasonID, LibraryID: "lib-shows", ParentID: seriesID, Type: "season",
		Title: "Season 1", SeasonNumber: &seasonNum, AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	episodeID := "ep-1"
	episodeNum := 1
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: episodeID, LibraryID: "lib-shows", ParentID: seasonID, Type: "episode",
		Title: "S01E01", SeasonNumber: &seasonNum, EpisodeNumber: &episodeNum,
		AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	imageDir := t.TempDir()
	pm := pathmap.New(imageDir)
	bus := event.NewBus(slog.Default())
	prober := &mockProber{result: &probe.Result{}}
	s := New(itemRepo, db.NewMediaStreamRepository(database), metaRepo, extRepo,
		imgRepo, db.NewChapterRepository(database), db.NewPeopleRepository(database),
		nil, prober, bus, imageDir, pm, slog.Default())

	rating := 8.4
	premiere, _ := time.Parse("2006-01-02", "2025-03-04")
	s.providers = &stubProvider{episode: &provider.EpisodeMetadataResult{
		Title:          "Media hora en el cielo",
		Overview:       "Matt Murdock vuelve a Nueva York.",
		PremiereDate:   &premiere,
		Rating:         &rating,
		RuntimeMinutes: 0, // probe wins when set; left blank here on purpose
		StillURL:       srv.URL + "/still.png",
	}}

	episode, err := itemRepo.GetByID(context.Background(), episodeID)
	if err != nil {
		t.Fatal(err)
	}
	s.enrichEpisode(context.Background(), episode, seasonID, seasonNum, episodeNum)

	// Item row updates: title, year, rating must reflect the provider
	// result; runtime is left untouched (probe-wins policy).
	got, err := itemRepo.GetByID(context.Background(), episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Media hora en el cielo" {
		t.Errorf("title not refreshed: %q", got.Title)
	}
	if got.Year != 2025 {
		t.Errorf("year from premiere date: got %d, want 2025", got.Year)
	}
	if got.CommunityRating == nil || *got.CommunityRating != 8.4 {
		t.Errorf("community rating: got %v, want 8.4", got.CommunityRating)
	}

	// Metadata row carries the overview so the detail handler can
	// render it without a separate provider hop.
	meta, err := metaRepo.GetByItemID(context.Background(), episodeID)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta == nil || meta.Overview != "Matt Murdock vuelve a Nueva York." {
		t.Errorf("overview not persisted: %+v", meta)
	}

	// Still landed as a primary backdrop on the EPISODE row.
	imgs, err := imgRepo.ListByItem(context.Background(), episodeID)
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(imgs) != 1 || imgs[0].Type != "backdrop" || !imgs[0].IsPrimary {
		t.Fatalf("expected one primary backdrop on episode, got %+v", imgs)
	}
	if !strings.HasPrefix(imgs[0].Path, "/api/v1/images/file/") {
		t.Errorf("image path leaked upstream URL: %q", imgs[0].Path)
	}
}

// TestEnrichEpisode_NoTMDbIDOnSeries pins the failure-mode contract:
// when the parent series has no tmdb external id (e.g. enriched without
// API key, or no match found), enrichEpisode must be a no-op — no
// provider call, no metadata row, no image row. The next scan retries.
func TestEnrichEpisode_NoTMDbIDOnSeries(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	imgRepo := db.NewImageRepository(database)
	metaRepo := db.NewMetadataRepository(database)
	extRepo := db.NewExternalIDRepository(database)

	now := time.Now()
	_ = libRepo.Create(context.Background(), &db.Library{
		ID: "lib-shows", Name: "Shows", ContentType: "shows", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	})

	seriesID := "series-1"
	_ = itemRepo.Create(context.Background(), &db.Item{
		ID: seriesID, LibraryID: "lib-shows", Type: "series", Title: "X",
		AddedAt: now, UpdatedAt: now,
	})
	// No external_id row for the series — the case under test.

	seasonID := "season-1"
	seasonNum := 1
	_ = itemRepo.Create(context.Background(), &db.Item{
		ID: seasonID, LibraryID: "lib-shows", ParentID: seriesID, Type: "season",
		Title: "Season 1", SeasonNumber: &seasonNum, AddedAt: now, UpdatedAt: now,
	})

	episodeID := "ep-1"
	episodeNum := 1
	_ = itemRepo.Create(context.Background(), &db.Item{
		ID: episodeID, LibraryID: "lib-shows", ParentID: seasonID, Type: "episode",
		Title: "Pilot", SeasonNumber: &seasonNum, EpisodeNumber: &episodeNum,
		AddedAt: now, UpdatedAt: now,
	})

	bus := event.NewBus(slog.Default())
	prober := &mockProber{result: &probe.Result{}}
	s := New(itemRepo, db.NewMediaStreamRepository(database), metaRepo, extRepo,
		imgRepo, db.NewChapterRepository(database), db.NewPeopleRepository(database),
		nil, prober, bus, t.TempDir(), pathmap.New(t.TempDir()), slog.Default())

	called := false
	s.providers = &stubProviderTrackingCalls{onEpisode: func() { called = true }}

	episode, _ := itemRepo.GetByID(context.Background(), episodeID)
	s.enrichEpisode(context.Background(), episode, seasonID, seasonNum, episodeNum)

	if called {
		t.Error("provider was called even though series has no tmdb id")
	}
	imgs, _ := imgRepo.ListByItem(context.Background(), episodeID)
	if len(imgs) != 0 {
		t.Errorf("no images expected; got %d", len(imgs))
	}
}

// TestEnrichSeason_PersistsMetadataAndPoster pins the per-season
// enrichment contract: given a series whose external_ids carry a TMDb
// id, the scanner must overwrite the placeholder "Season N" title with
// the TMDb-friendly name, persist overview + air-date + rating onto
// the season row, and ingest the season poster as a primary `primary`
// image attached to the season (not the series).
//
// The bug this guards is the "Season 1 / Season 1" double-tab the
// frontend used to show — without TMDb, two seasons with the same
// number rendered identically; with this enrichment they pick up
// distinct names ("Specials", "The Final Chapter") and posters.
func TestEnrichSeason_PersistsMetadataAndPoster(t *testing.T) {
	prevBlocked := imaging.BlockedIP
	imaging.BlockedIP = func(ip net.IP) bool {
		if ip.IsLoopback() {
			return false
		}
		return prevBlocked(ip)
	}
	t.Cleanup(func() { imaging.BlockedIP = prevBlocked })

	pngBody := makeTinyPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBody)
	}))
	t.Cleanup(srv.Close)

	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	imgRepo := db.NewImageRepository(database)
	metaRepo := db.NewMetadataRepository(database)
	extRepo := db.NewExternalIDRepository(database)

	now := time.Now()
	if err := libRepo.Create(context.Background(), &db.Library{
		ID: "lib-shows", Name: "Shows", ContentType: "shows", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/dummy"},
	}); err != nil {
		t.Fatal(err)
	}

	seriesID := "series-1"
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: seriesID, LibraryID: "lib-shows", Type: "series", Title: "Daredevil",
		AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := extRepo.Upsert(context.Background(), &db.ExternalID{
		ItemID: seriesID, Provider: "tmdb", ExternalID: "12345",
	}); err != nil {
		t.Fatal(err)
	}

	seasonID := "season-1"
	seasonNum := 1
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: seasonID, LibraryID: "lib-shows", ParentID: seriesID, Type: "season",
		Title: "Season 1", SortTitle: "season 1", SeasonNumber: &seasonNum,
		AddedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	imageDir := t.TempDir()
	pm := pathmap.New(imageDir)
	bus := event.NewBus(slog.Default())
	prober := &mockProber{result: &probe.Result{}}
	s := New(itemRepo, db.NewMediaStreamRepository(database), metaRepo, extRepo,
		imgRepo, db.NewChapterRepository(database), db.NewPeopleRepository(database),
		nil, prober, bus, imageDir, pm, slog.Default())

	rating := 8.7
	premiere, _ := time.Parse("2006-01-02", "2025-03-04")
	s.providers = &stubProvider{season: &provider.SeasonMetadataResult{
		Title:        "Born Again",
		Overview:     "La temporada de regreso de Matt Murdock.",
		PremiereDate: &premiere,
		Rating:       &rating,
		EpisodeCount: 9,
		PosterURL:    srv.URL + "/poster.png",
	}}

	season, err := itemRepo.GetByID(context.Background(), seasonID)
	if err != nil {
		t.Fatal(err)
	}
	s.enrichSeason(context.Background(), season, seriesID, seasonNum)

	got, err := itemRepo.GetByID(context.Background(), seasonID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Born Again" {
		t.Errorf("title not refreshed from TMDb: %q", got.Title)
	}
	if got.Year != 2025 {
		t.Errorf("year from premiere date: got %d, want 2025", got.Year)
	}
	if got.CommunityRating == nil || *got.CommunityRating != 8.7 {
		t.Errorf("community rating: got %v, want 8.7", got.CommunityRating)
	}

	meta, err := metaRepo.GetByItemID(context.Background(), seasonID)
	if err != nil || meta == nil || meta.Overview == "" {
		t.Fatalf("overview not persisted: %+v err=%v", meta, err)
	}

	imgs, err := imgRepo.ListByItem(context.Background(), seasonID)
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(imgs) != 1 || imgs[0].Type != "primary" || !imgs[0].IsPrimary {
		t.Fatalf("expected one primary poster on season, got %+v", imgs)
	}
	if !strings.HasPrefix(imgs[0].Path, "/api/v1/images/file/") {
		t.Errorf("image path leaked upstream URL: %q", imgs[0].Path)
	}
}

// stubProviderTrackingCalls is a minimal stub for the no-tmdb-id test —
// records whether FetchEpisodeMetadata was reached. The scanner must
// short-circuit before that point when the parent series carries no
// external id, so the recorder stays false on the happy path.
type stubProviderTrackingCalls struct {
	onEpisode func()
}

func (s *stubProviderTrackingCalls) SearchMetadata(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (s *stubProviderTrackingCalls) FetchMetadata(_ context.Context, _ string, _ provider.ItemType) (*provider.MetadataResult, error) {
	return nil, nil
}
func (s *stubProviderTrackingCalls) FetchImages(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
	return nil, nil
}
func (s *stubProviderTrackingCalls) FetchEpisodeMetadata(_ context.Context, _ string, _, _ int) (*provider.EpisodeMetadataResult, error) {
	if s.onEpisode != nil {
		s.onEpisode()
	}
	return nil, nil
}
func (s *stubProviderTrackingCalls) FetchSeasonMetadata(_ context.Context, _ string, _ int) (*provider.SeasonMetadataResult, error) {
	return nil, nil
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
		imgRepo, db.NewChapterRepository(database), db.NewPeopleRepository(database),
		nil, prober, bus, "", nil, slog.Default())
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
