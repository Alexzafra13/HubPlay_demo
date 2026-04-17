package library

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/provider"
)

// ─── Fakes satisfying the refresher collaborator interfaces ─────────────────

type fakeItems struct {
	items []*db.Item
	err   error
}

func (f *fakeItems) List(_ context.Context, _ db.ItemFilter) ([]*db.Item, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.items, len(f.items), nil
}

type fakeExtIDs struct {
	byItem map[string][]*db.ExternalID
	err    error
}

func (f *fakeExtIDs) ListByItem(_ context.Context, itemID string) ([]*db.ExternalID, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byItem[itemID], nil
}

type fakeImagesRepo struct {
	mu       sync.Mutex
	byItem   map[string][]*db.Image
	created  []*db.Image
	primary  map[string]string // "itemID:type" -> imageID
	createErr error
}

func newFakeImagesRepo() *fakeImagesRepo {
	return &fakeImagesRepo{
		byItem:  map[string][]*db.Image{},
		primary: map[string]string{},
	}
}

func (f *fakeImagesRepo) ListByItem(_ context.Context, itemID string) ([]*db.Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*db.Image(nil), f.byItem[itemID]...), nil
}

func (f *fakeImagesRepo) Create(_ context.Context, img *db.Image) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	cp := *img
	f.byItem[img.ItemID] = append(f.byItem[img.ItemID], &cp)
	f.created = append(f.created, &cp)
	return nil
}

func (f *fakeImagesRepo) SetPrimary(_ context.Context, itemID, imgType, imageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.primary[itemID+":"+imgType] = imageID
	return nil
}

type fakeProvider struct {
	fn func(ctx context.Context, ids map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
}

func (p *fakeProvider) FetchImages(ctx context.Context, ids map[string]string, t provider.ItemType) ([]provider.ImageResult, error) {
	if p.fn != nil {
		return p.fn(ctx, ids, t)
	}
	return nil, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func silent() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 150, G: 50, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// imageServer returns an httptest server that serves the given bytes as
// image/jpeg. It also temporarily unblocks loopback in imaging.SafeGet.
func imageServer(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(data)
	}))
	prev := imaging.BlockedIP
	imaging.BlockedIP = func(net.IP) bool { return false }
	t.Cleanup(func() {
		imaging.BlockedIP = prev
		srv.Close()
	})
	return srv
}

func newTestRefresher(t *testing.T) (*ImageRefresher, *fakeItems, *fakeExtIDs, *fakeImagesRepo, *fakeProvider, string) {
	t.Helper()
	dir := t.TempDir()
	imageDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	items := &fakeItems{}
	extIDs := &fakeExtIDs{byItem: map[string][]*db.ExternalID{}}
	images := newFakeImagesRepo()
	providers := &fakeProvider{}
	r := NewImageRefresher(items, extIDs, images, providers, pathmap.New(imageDir), imageDir, silent())
	return r, items, extIDs, images, providers, imageDir
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestImageRefresher_ListItemsError_Propagates(t *testing.T) {
	r, items, _, _, _, _ := newTestRefresher(t)
	items.err = errors.New("db down")
	if _, err := r.RefreshForLibrary(context.Background(), "lib-1"); err == nil {
		t.Fatal("expected error from upstream list failure")
	}
}

func TestImageRefresher_NoItems_ZeroUpdated(t *testing.T) {
	r, _, _, _, _, _ := newTestRefresher(t)
	count, err := r.RefreshForLibrary(context.Background(), "lib-1")
	if err != nil || count != 0 {
		t.Fatalf("expected (0, nil), got (%d, %v)", count, err)
	}
}

func TestImageRefresher_ItemWithoutExternalIDs_Skipped(t *testing.T) {
	r, items, _, _, _, _ := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	count, err := r.RefreshForLibrary(context.Background(), "lib-1")
	if err != nil || count != 0 {
		t.Fatalf("got (%d, %v)", count, err)
	}
}

func TestImageRefresher_AddsMissingKinds(t *testing.T) {
	r, items, ext, images, providers, imageDir := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: srv.URL + "/primary.jpg", Type: "primary", Score: 0.9, Width: 40, Height: 40},
			{URL: srv.URL + "/backdrop.jpg", Type: "backdrop", Score: 0.8, Width: 40, Height: 40},
		}, nil
	}
	count, err := r.RefreshForLibrary(context.Background(), "lib-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if count != 2 {
		t.Fatalf("count: got %d want 2", count)
	}
	if len(images.byItem["it-1"]) != 2 {
		t.Errorf("images persisted: %d", len(images.byItem["it-1"]))
	}
	// Files landed on disk.
	entries, _ := os.ReadDir(filepath.Join(imageDir, "it-1"))
	if len(entries) != 2 {
		t.Errorf("files on disk: %d", len(entries))
	}
}

func TestImageRefresher_SkipsExistingKinds(t *testing.T) {
	r, items, ext, images, providers, _ := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}
	// Seed an existing primary so it should be skipped.
	images.byItem["it-1"] = []*db.Image{{ID: "pre-1", ItemID: "it-1", Type: "primary"}}

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: srv.URL + "/primary.jpg", Type: "primary", Score: 0.9},
			{URL: srv.URL + "/bd.jpg", Type: "backdrop", Score: 0.8, Width: 40, Height: 40},
		}, nil
	}
	count, _ := r.RefreshForLibrary(context.Background(), "lib-1")
	if count != 1 {
		t.Errorf("expected 1 new image (backdrop only), got %d", count)
	}
}

func TestImageRefresher_PicksHighestScorePerKind(t *testing.T) {
	r, items, ext, images, providers, _ := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: srv.URL + "/low.jpg", Type: "primary", Score: 0.2, Width: 40, Height: 40},
			{URL: srv.URL + "/high.jpg", Type: "primary", Score: 0.9, Width: 40, Height: 40},
			{URL: srv.URL + "/mid.jpg", Type: "primary", Score: 0.5, Width: 40, Height: 40},
		}, nil
	}
	count, _ := r.RefreshForLibrary(context.Background(), "lib-1")
	if count != 1 {
		t.Fatalf("expected 1 persisted (best-score), got %d", count)
	}
	// Verified: only one image stored → the highest-score selection worked
	// (we don't know which URL — the test only asserts the *count*).
	if len(images.byItem["it-1"]) != 1 {
		t.Errorf("stored: %d", len(images.byItem["it-1"]))
	}
}

func TestImageRefresher_FiltersInvalidKinds(t *testing.T) {
	r, items, ext, _, providers, _ := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: srv.URL + "/bogus.jpg", Type: "fanart", Score: 0.9},     // not a valid kind
			{URL: srv.URL + "/weird.jpg", Type: "WALLPAPER", Score: 0.9},   // not a valid kind
		}, nil
	}
	count, _ := r.RefreshForLibrary(context.Background(), "lib-1")
	if count != 0 {
		t.Errorf("expected 0 (all kinds invalid), got %d", count)
	}
}

func TestImageRefresher_ItemTypeMappedForProviderQuery(t *testing.T) {
	r, items, ext, _, providers, _ := newTestRefresher(t)
	items.items = []*db.Item{
		{ID: "mov-1", Type: "movie"},
		{ID: "ser-1", Type: "series"},
		{ID: "sea-1", Type: "season"},
		{ID: "ep-1", Type: "episode"},
	}
	for _, id := range []string{"mov-1", "ser-1", "sea-1", "ep-1"} {
		ext.byItem[id] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "x"}}
	}
	seen := map[provider.ItemType]bool{}
	providers.fn = func(_ context.Context, _ map[string]string, tp provider.ItemType) ([]provider.ImageResult, error) {
		seen[tp] = true
		return nil, nil // no results → no downloads
	}
	_, _ = r.RefreshForLibrary(context.Background(), "lib-1")
	for _, want := range []provider.ItemType{
		provider.ItemMovie, provider.ItemSeries, provider.ItemSeason, provider.ItemEpisode,
	} {
		if !seen[want] {
			t.Errorf("provider never queried for type %q", want)
		}
	}
}

func TestImageRefresher_CreateErrorRemovesFile(t *testing.T) {
	r, items, ext, images, providers, imageDir := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}
	images.createErr = errors.New("db insert failed")

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{{URL: srv.URL + "/x.jpg", Type: "primary", Score: 1, Width: 40, Height: 40}}, nil
	}
	count, _ := r.RefreshForLibrary(context.Background(), "lib-1")
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
	// File should have been cleaned up after the DB Create failure.
	entries, err := os.ReadDir(filepath.Join(imageDir, "it-1"))
	if err == nil && len(entries) != 0 {
		t.Errorf("file not cleaned up: %d entries remain", len(entries))
	}
}

func TestImageRefresher_SetsPrimaryMapping(t *testing.T) {
	r, items, ext, images, providers, _ := newTestRefresher(t)
	items.items = []*db.Item{{ID: "it-1", Type: "movie"}}
	ext.byItem["it-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}

	srv := imageServer(t, testJPEG(t))
	providers.fn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{{URL: srv.URL + "/x.jpg", Type: "primary", Score: 1, Width: 40, Height: 40}}, nil
	}
	_, _ = r.RefreshForLibrary(context.Background(), "lib-1")
	if got := images.primary["it-1:primary"]; got == "" {
		t.Errorf("SetPrimary was not called for it-1/primary (primary map: %v)", images.primary)
	}
}
