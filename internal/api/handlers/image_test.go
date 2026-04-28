package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/imaging"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/library"
	"hubplay/internal/provider"
	"hubplay/internal/testutil"
)

// ─── Fakes ──────────────────────────────────────────────────────────────────
//
// The handlers package defines its own interfaces (see interfaces.go), so fakes
// below satisfy those interfaces without crossing package boundaries.

type fakeImageRepo struct {
	mu     sync.Mutex
	images map[string]*db.Image // by image ID
}

func newFakeImageRepo() *fakeImageRepo {
	return &fakeImageRepo{images: map[string]*db.Image{}}
}

func (r *fakeImageRepo) GetPrimaryURLs(_ context.Context, _ []string) (map[string]map[string]string, error) {
	return map[string]map[string]string{}, nil
}

func (r *fakeImageRepo) ListByItem(_ context.Context, itemID string) ([]*db.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*db.Image{}
	for _, img := range r.images {
		if img.ItemID == itemID {
			out = append(out, img)
		}
	}
	return out, nil
}

func (r *fakeImageRepo) Create(_ context.Context, img *db.Image) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *img
	r.images[img.ID] = &cp
	return nil
}

func (r *fakeImageRepo) SetPrimary(_ context.Context, itemID, imgType, imageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, img := range r.images {
		if img.ItemID == itemID && img.Type == imgType {
			img.IsPrimary = false
		}
	}
	if img, ok := r.images[imageID]; ok {
		img.IsPrimary = true
		return nil
	}
	return domain.NewNotFound("image")
}

func (r *fakeImageRepo) GetByID(_ context.Context, id string) (*db.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if img, ok := r.images[id]; ok {
		cp := *img
		return &cp, nil
	}
	return nil, domain.NewNotFound("image")
}

func (r *fakeImageRepo) DeleteByID(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.images[id]; !ok {
		return domain.NewNotFound("image")
	}
	delete(r.images, id)
	return nil
}

func (r *fakeImageRepo) SetLocked(_ context.Context, id string, locked bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	img, ok := r.images[id]
	if !ok {
		return domain.NewNotFound("image")
	}
	img.IsLocked = locked
	return nil
}

func (r *fakeImageRepo) HasLockedForKind(_ context.Context, itemID, kind string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, img := range r.images {
		if img.ItemID == itemID && img.Type == kind && img.IsLocked {
			return true, nil
		}
	}
	return false, nil
}

type fakeExternalIDRepo struct {
	byItem map[string][]*db.ExternalID
}

func (r *fakeExternalIDRepo) ListByItem(_ context.Context, itemID string) ([]*db.ExternalID, error) {
	return r.byItem[itemID], nil
}

type fakeItemRepo struct {
	byID map[string]*db.Item
}

func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*db.Item, error) {
	if it, ok := r.byID[id]; ok {
		return it, nil
	}
	return nil, domain.NewNotFound("item")
}

func (r *fakeItemRepo) List(_ context.Context, filter db.ItemFilter) ([]*db.Item, int, error) {
	out := []*db.Item{}
	for _, it := range r.byID {
		if filter.LibraryID != "" && it.LibraryID != filter.LibraryID {
			continue
		}
		out = append(out, it)
	}
	return out, len(out), nil
}

type fakeProviderManager struct {
	fetchImagesFn func(ctx context.Context, externalIDs map[string]string, itemType provider.ItemType) ([]provider.ImageResult, error)
}

func (p *fakeProviderManager) SearchMetadata(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return nil, nil
}
func (p *fakeProviderManager) FetchMetadata(_ context.Context, _ string, _ provider.ItemType) (*provider.MetadataResult, error) {
	return nil, nil
}
func (p *fakeProviderManager) FetchImages(ctx context.Context, ids map[string]string, t provider.ItemType) ([]provider.ImageResult, error) {
	if p.fetchImagesFn != nil {
		return p.fetchImagesFn(ctx, ids, t)
	}
	return nil, nil
}
func (p *fakeProviderManager) SearchSubtitles(_ context.Context, _ provider.SubtitleQuery) ([]provider.SubtitleResult, error) {
	return nil, nil
}
func (p *fakeProviderManager) DownloadSubtitle(_ context.Context, _, _ string) ([]byte, error) {
	return nil, nil
}

// ─── Test server wiring ─────────────────────────────────────────────────────

type imageTestEnv struct {
	t          *testing.T
	imageDir   string
	images     *fakeImageRepo
	externals  *fakeExternalIDRepo
	items      *fakeItemRepo
	providers  *fakeProviderManager
	handler    *ImageHandler
	router     chi.Router
	server     *httptest.Server
}

func newImageTestEnv(t *testing.T) *imageTestEnv {
	t.Helper()
	dir := t.TempDir()
	imageDir := filepath.Join(dir, "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("mkdir imageDir: %v", err)
	}

	env := &imageTestEnv{
		t:         t,
		imageDir:  imageDir,
		images:    newFakeImageRepo(),
		externals: &fakeExternalIDRepo{byItem: map[string][]*db.ExternalID{}},
		items:     &fakeItemRepo{byID: map[string]*db.Item{}},
		providers: &fakeProviderManager{},
	}
	// Build a real library.ImageRefresher over the same fakes so the
	// end-to-end refresh path stays exercised by
	// TestImageHandler_RefreshLibraryImages_AddsMissingTypes.
	refresher := library.NewImageRefresher(
		env.items, env.externals, env.images, env.providers,
		pathmap.New(imageDir), imageDir, testutil.NopLogger(),
	)
	env.handler = NewImageHandler(env.images, env.externals, env.items, env.providers,
		refresher, imageDir, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/items/{id}/images", func(r chi.Router) {
			r.Get("/", env.handler.List)
			r.Get("/available", env.handler.Available)
			r.Put("/{type}/select", env.handler.Select)
			r.Post("/{type}/upload", env.handler.Upload)
			r.Put("/{imageId}/primary", env.handler.SetPrimary)
			r.Delete("/{imageId}", env.handler.Delete)
		})
		r.Get("/images/file/{id}", env.handler.ServeFile)
		r.Post("/libraries/{id}/images/refresh", env.handler.RefreshLibraryImages)
	})
	env.router = r
	env.server = httptest.NewServer(r)
	t.Cleanup(env.server.Close)
	return env
}

func (e *imageTestEnv) do(method, path string, body io.Reader, contentType string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, body)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.server.Client().Do(req)
	if err != nil {
		e.t.Fatalf("do request: %v", err)
	}
	return resp
}

// Convenience: read JSON envelope {"data": ...}.
func decodeDataEnvelope(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

// ─── Test helpers: synthesize real image bytes ──────────────────────────────

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func multipartUpload(t *testing.T, fieldName, filename, contentType string, data []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, fieldName, filename)}
	if contentType != "" {
		hdr["Content-Type"] = []string{contentType}
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &body, mw.FormDataContentType()
}

// ─── Tests: List ────────────────────────────────────────────────────────────

func TestImageHandler_List_Empty(t *testing.T) {
	env := newImageTestEnv(t)

	resp := env.do(http.MethodGet, "/api/v1/items/item-1/images/", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	got := decodeDataEnvelope(t, resp)
	data, _ := got["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %v", data)
	}
}

func TestImageHandler_List_PopulatedReturnsImages(t *testing.T) {
	env := newImageTestEnv(t)
	env.images.images["img-1"] = &db.Image{ID: "img-1", ItemID: "item-1", Type: "primary", Path: "/api/v1/images/file/img-1"}
	env.images.images["img-2"] = &db.Image{ID: "img-2", ItemID: "item-1", Type: "backdrop", Path: "/api/v1/images/file/img-2"}
	env.images.images["img-3"] = &db.Image{ID: "img-3", ItemID: "item-other", Type: "primary"}

	resp := env.do(http.MethodGet, "/api/v1/items/item-1/images/", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	env_ := decodeDataEnvelope(t, resp)
	data, _ := env_["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 images for item-1, got %d", len(data))
	}
}

// ─── Tests: Available ───────────────────────────────────────────────────────

func TestImageHandler_Available_NoExternalIDs_EmptyData(t *testing.T) {
	env := newImageTestEnv(t)
	resp := env.do(http.MethodGet, "/api/v1/items/item-1/images/available", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	e := decodeDataEnvelope(t, resp)
	data, _ := e["data"].([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty, got %v", data)
	}
}

func TestImageHandler_Available_ReturnsProviderImages(t *testing.T) {
	env := newImageTestEnv(t)
	env.externals.byItem["item-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}
	env.items.byID["item-1"] = &db.Item{ID: "item-1", Type: "movie"}
	env.providers.fetchImagesFn = func(_ context.Context, ids map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		if ids["tmdb"] != "42" {
			t.Errorf("unexpected ids: %v", ids)
		}
		return []provider.ImageResult{
			{URL: "https://img.example/poster.jpg", Type: "primary", Width: 500, Height: 750, Score: 0.9},
			{URL: "https://img.example/bd.jpg", Type: "backdrop", Width: 1920, Height: 1080, Score: 0.8},
		}, nil
	}

	resp := env.do(http.MethodGet, "/api/v1/items/item-1/images/available", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", resp.StatusCode, readAll(resp))
	}
	e := decodeDataEnvelope(t, resp)
	data, _ := e["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 provider images, got %d", len(data))
	}
}

func TestImageHandler_Available_FilterByType(t *testing.T) {
	env := newImageTestEnv(t)
	env.externals.byItem["item-1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "42"}}
	env.items.byID["item-1"] = &db.Item{ID: "item-1", Type: "movie"}
	env.providers.fetchImagesFn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: "a.jpg", Type: "primary"},
			{URL: "b.jpg", Type: "backdrop"},
		}, nil
	}
	resp := env.do(http.MethodGet, "/api/v1/items/item-1/images/available?type=backdrop", nil, "")
	e := decodeDataEnvelope(t, resp)
	data, _ := e["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("filter: want 1, got %d", len(data))
	}
}

// ─── Tests: Upload ──────────────────────────────────────────────────────────

func TestImageHandler_Upload_JPEGHappyPath(t *testing.T) {
	env := newImageTestEnv(t)
	data := makeJPEG(t, 50, 50)
	body, ct := multipartUpload(t, "file", "poster.jpg", "image/jpeg", data)

	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", resp.StatusCode, readAll(resp))
	}
	e := decodeDataEnvelope(t, resp)
	d, _ := e["data"].(map[string]any)
	if d["type"] != "primary" {
		t.Fatalf("type: %v", d["type"])
	}
	if d["is_primary"] != true {
		t.Fatalf("is_primary: %v", d["is_primary"])
	}
	// Blurhash should be present for JPEG (decodable).
	if bh, ok := d["blurhash"].(string); !ok || bh == "" {
		t.Fatalf("blurhash: got %v", d["blurhash"])
	}
	// File written to disk under <imageDir>/<itemID>/.
	entries, err := os.ReadDir(filepath.Join(env.imageDir, "item-1"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected file on disk: err=%v entries=%d", err, len(entries))
	}
}

func TestImageHandler_Upload_PNGHappyPath(t *testing.T) {
	env := newImageTestEnv(t)
	data := makePNG(t, 40, 40)
	body, ct := multipartUpload(t, "file", "poster.png", "image/png", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-2/images/backdrop/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, body: %s", resp.StatusCode, readAll(resp))
	}
}

func TestImageHandler_Upload_InvalidType_Rejected(t *testing.T) {
	env := newImageTestEnv(t)
	data := makeJPEG(t, 10, 10)
	body, ct := multipartUpload(t, "file", "x.jpg", "image/jpeg", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/bogus/upload", body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid type: got %d want 400", resp.StatusCode)
	}
}

func TestImageHandler_Upload_WrongContentType_Rejected(t *testing.T) {
	env := newImageTestEnv(t)
	body, ct := multipartUpload(t, "file", "x.gif", "image/gif", []byte("GIF89a"))
	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("gif rejected: got %d want 400", resp.StatusCode)
	}
}

func TestImageHandler_Upload_MissingFileField_Rejected(t *testing.T) {
	env := newImageTestEnv(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("notfile", "x")
	_ = mw.Close()
	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/primary/upload", &body, mw.FormDataContentType())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing file: got %d want 400", resp.StatusCode)
	}
}

// ─── Tests: SetPrimary ──────────────────────────────────────────────────────

func TestImageHandler_SetPrimary_ExistingImage(t *testing.T) {
	env := newImageTestEnv(t)
	env.images.images["img-1"] = &db.Image{ID: "img-1", ItemID: "item-1", Type: "primary"}
	resp := env.do(http.MethodPut, "/api/v1/items/item-1/images/img-1/primary", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if !env.images.images["img-1"].IsPrimary {
		t.Fatalf("image was not marked primary")
	}
}

func TestImageHandler_SetPrimary_UnknownImage_404(t *testing.T) {
	env := newImageTestEnv(t)
	resp := env.do(http.MethodPut, "/api/v1/items/item-1/images/missing/primary", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing image: got %d want 404", resp.StatusCode)
	}
}

func TestImageHandler_SetPrimary_WrongItem_404(t *testing.T) {
	env := newImageTestEnv(t)
	env.images.images["img-1"] = &db.Image{ID: "img-1", ItemID: "item-A", Type: "primary"}
	resp := env.do(http.MethodPut, "/api/v1/items/item-B/images/img-1/primary", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong item: got %d want 404", resp.StatusCode)
	}
}

// ─── Tests: Delete ──────────────────────────────────────────────────────────

func TestImageHandler_Delete_RemovesRecordAndMapping(t *testing.T) {
	env := newImageTestEnv(t)
	// First upload to create mapping + on-disk file.
	data := makeJPEG(t, 20, 20)
	body, ct := multipartUpload(t, "file", "x.jpg", "image/jpeg", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-X/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d %s", resp.StatusCode, readAll(resp))
	}
	e := decodeDataEnvelope(t, resp)
	id := e["data"].(map[string]any)["id"].(string)

	// Now delete.
	del := env.do(http.MethodDelete, "/api/v1/items/item-X/images/"+id, nil, "")
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: got %d want 204", del.StatusCode)
	}
	if _, ok := env.images.images[id]; ok {
		t.Fatalf("image still in repo after delete")
	}
	// Path mapping file removed.
	if _, err := os.Stat(filepath.Join(env.imageDir, ".mappings", id)); !os.IsNotExist(err) {
		t.Fatalf("mapping file not removed: err=%v", err)
	}
}

func TestImageHandler_Delete_RemovesCachedThumbnails(t *testing.T) {
	// The serve handler generates `<imageDir>/.thumbnails/<id>_wN.<ext>`
	// on demand when the client requests a sized variant. Delete must
	// reap those siblings too, otherwise long-lived servers leak
	// thumbnails for every since-deleted image.
	env := newImageTestEnv(t)
	data := makeJPEG(t, 20, 20)
	body, ct := multipartUpload(t, "file", "x.jpg", "image/jpeg", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-T/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d %s", resp.StatusCode, readAll(resp))
	}
	id := decodeDataEnvelope(t, resp)["data"].(map[string]any)["id"].(string)

	// Pre-seed a couple of thumbnail-shaped files. We don't go through
	// the resizer on purpose — the test wants to assert the cleanup
	// glob matches the documented filename pattern, not whatever the
	// real resizer produces (which would couple this test to ffmpeg).
	thumbDir := filepath.Join(env.imageDir, ".thumbnails")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		t.Fatalf("mkdir thumbs: %v", err)
	}
	thumbs := []string{
		filepath.Join(thumbDir, id+"_w300.jpg"),
		filepath.Join(thumbDir, id+"_w600.jpg"),
		// Different image's thumb — must NOT be removed.
		filepath.Join(thumbDir, "other-id_w300.jpg"),
	}
	for _, p := range thumbs {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	del := env.do(http.MethodDelete, "/api/v1/items/item-T/images/"+id, nil, "")
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: got %d want 204", del.StatusCode)
	}

	for _, p := range thumbs[:2] {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("thumbnail %q should have been removed: err=%v", p, err)
		}
	}
	// Other image's thumbnail untouched — the glob only matches the
	// deleted ID's prefix.
	if _, err := os.Stat(thumbs[2]); err != nil {
		t.Errorf("unrelated thumbnail %q got swept: %v", thumbs[2], err)
	}
}

func TestImageHandler_Delete_Missing_404(t *testing.T) {
	env := newImageTestEnv(t)
	resp := env.do(http.MethodDelete, "/api/v1/items/item-1/images/missing", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
}

// ─── Tests: ServeFile ───────────────────────────────────────────────────────

func TestImageHandler_ServeFile_LocalFile(t *testing.T) {
	env := newImageTestEnv(t)
	data := makeJPEG(t, 30, 30)
	body, ct := multipartUpload(t, "file", "x.jpg", "image/jpeg", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-Y/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload failed: %d %s", resp.StatusCode, readAll(resp))
	}
	id := decodeDataEnvelope(t, resp)["data"].(map[string]any)["id"].(string)

	serve := env.do(http.MethodGet, "/api/v1/images/file/"+id, nil, "")
	if serve.StatusCode != http.StatusOK {
		t.Fatalf("serve: got %d want 200", serve.StatusCode)
	}
	defer serve.Body.Close() //nolint:errcheck
	got, _ := io.ReadAll(serve.Body)
	if !bytes.Equal(got, data) {
		t.Fatalf("served bytes differ from uploaded (got %d, want %d)", len(got), len(data))
	}
	if cc := serve.Header.Get("Cache-Control"); cc == "" {
		t.Fatalf("missing Cache-Control header")
	}
}

func TestImageHandler_ServeFile_Thumbnail(t *testing.T) {
	env := newImageTestEnv(t)
	// Upload PNG so thumbnail.go (std-lib decode) can resize it.
	data := makePNG(t, 400, 400)
	body, ct := multipartUpload(t, "file", "x.png", "image/png", data)
	resp := env.do(http.MethodPost, "/api/v1/items/item-T/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload failed: %d %s", resp.StatusCode, readAll(resp))
	}
	id := decodeDataEnvelope(t, resp)["data"].(map[string]any)["id"].(string)

	serve := env.do(http.MethodGet, "/api/v1/images/file/"+id+"?w=100", nil, "")
	if serve.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail serve: got %d", serve.StatusCode)
	}
	defer serve.Body.Close() //nolint:errcheck
	_, _ = io.ReadAll(serve.Body)

	thumbDir := filepath.Join(env.imageDir, ".thumbnails")
	entries, err := os.ReadDir(thumbDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("thumbnail not created: err=%v entries=%d", err, len(entries))
	}
}

func TestImageHandler_ServeFile_UnknownID_404(t *testing.T) {
	env := newImageTestEnv(t)
	resp := env.do(http.MethodGet, "/api/v1/images/file/unknown-id", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: got %d want 404", resp.StatusCode)
	}
}

// ─── Tests: RefreshLibraryImages ────────────────────────────────────────────

func TestImageHandler_RefreshLibraryImages_AddsMissingTypes(t *testing.T) {
	// Allow httptest.Server's 127.0.0.1 target through the SSRF guard for the
	// duration of this test — restored in cleanup.
	prev := imaging.BlockedIP
	imaging.BlockedIP = func(net.IP) bool { return false }
	t.Cleanup(func() { imaging.BlockedIP = prev })

	env := newImageTestEnv(t)
	env.items.byID["item-r1"] = &db.Item{ID: "item-r1", LibraryID: "lib-1", Type: "movie"}
	env.externals.byItem["item-r1"] = []*db.ExternalID{{Provider: "tmdb", ExternalID: "100"}}

	// Small JPEG served by an httptest server so downloadImage succeeds.
	imgBytes := makeJPEG(t, 60, 60)
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imgBytes)
	}))
	t.Cleanup(imgSrv.Close)

	env.providers.fetchImagesFn = func(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
		return []provider.ImageResult{
			{URL: imgSrv.URL + "/p.jpg", Type: "primary", Score: 0.9, Width: 60, Height: 60},
		}, nil
	}

	resp := env.do(http.MethodPost, "/api/v1/libraries/lib-1/images/refresh", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %s", resp.StatusCode, readAll(resp))
	}
	e := decodeDataEnvelope(t, resp)
	d, _ := e["data"].(map[string]any)
	if got := d["updated"]; got != float64(1) {
		t.Fatalf("updated count: got %v want 1", got)
	}
	// Repo should now hold 1 image.
	if len(env.images.images) != 1 {
		t.Fatalf("expected 1 image in repo, got %d", len(env.images.images))
	}
}

// Pure helpers moved to internal/imaging — see imaging/validators_test.go and
// imaging/blurhash_test.go for the unit-level coverage. The characterization
// tests above still exercise them end-to-end via Upload / Select.

// ─── Security regression tests (Phase 4 hardening) ──────────────────────────

// Upload must reject a multipart payload that claims image/jpeg in the part
// header but has an HTML body — content-type sniffing, not client trust.
func TestImageHandler_Upload_MIMESpoofRejected(t *testing.T) {
	env := newImageTestEnv(t)
	htmlPayload := []byte(`<!doctype html><html><body><script>pwn()</script></body></html>`)
	body, ct := multipartUpload(t, "file", "evil.jpg", "image/jpeg", htmlPayload)

	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mime spoof: got %d want 400, body: %s", resp.StatusCode, readAll(resp))
	}
}

// Upload must reject a PNG whose IHDR advertises impossible dimensions.
func TestImageHandler_Upload_DecompressionBombRejected(t *testing.T) {
	env := newImageTestEnv(t)
	// Reuse the forged-PNG helper idea inline: 50000x50000 IHDR claim.
	bomb := forgedPNGIHDR(50000, 50000)
	body, ct := multipartUpload(t, "file", "bomb.png", "image/png", bomb)

	resp := env.do(http.MethodPost, "/api/v1/items/item-1/images/primary/upload", body, ct)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bomb: got %d want 400, body: %s", resp.StatusCode, readAll(resp))
	}
}

// Upload must reject a non-safe itemID before touching the filesystem.
// We invoke the handler directly with a crafted URL param because chi's
// routing layer normalizes ".." out of HTTP paths — so the in-handler guard
// is only reachable via a programmatic call. Both layers (chi + handler)
// combine as defense in depth.
func TestImageHandler_Upload_TraversalItemIDRejected(t *testing.T) {
	env := newImageTestEnv(t)
	data := makeJPEG(t, 20, 20)
	body, ct := multipartUpload(t, "file", "x.jpg", "image/jpeg", data)

	req := httptest.NewRequest(http.MethodPost, "/x", body)
	req.Header.Set("Content-Type", ct)
	// Inject URL params manually (bypasses chi routing).
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "../etc")
	rctx.URLParams.Add("type", "primary")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	env.handler.Upload(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("traversal itemID: got %d want 400, body: %s", rr.Code, rr.Body.String())
	}
	entries, _ := os.ReadDir(env.imageDir)
	if len(entries) > 0 {
		t.Fatalf("filesystem touched despite rejection: %v", entries)
	}
}

// ServeFile must refuse non-UUID ids via pathmap's internal validation,
// falling through to the DB lookup which returns NOT_FOUND.
func TestImageHandler_ServeFile_TraversalIDReturns404(t *testing.T) {
	env := newImageTestEnv(t)
	resp := env.do(http.MethodGet, "/api/v1/images/file/..%2F..%2Fetc%2Fpasswd", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("traversal ID: got %d want 404", resp.StatusCode)
	}
}

// Select must refuse URLs that resolve to loopback (SSRF defense).
func TestImageHandler_Select_LoopbackURL_Blocked(t *testing.T) {
	env := newImageTestEnv(t)
	// A localhost target — default BlockedIP rejects this.
	bodyJSON := strings.NewReader(`{"url":"http://127.0.0.1:1/x.jpg","width":0,"height":0}`)
	resp := env.do(http.MethodPut, "/api/v1/items/item-1/images/primary/select", bodyJSON, "application/json")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("ssrf: got %d want 502, body: %s", resp.StatusCode, readAll(resp))
	}
}

// forgedPNGIHDR duplicates imaging/safety_test.go's helper so this test file
// doesn't need to export internals.
func forgedPNGIHDR(w, h uint32) []byte {
	var out []byte
	out = append(out, 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A)
	out = append(out, 0x00, 0x00, 0x00, 13)
	typeAndData := []byte{'I', 'H', 'D', 'R',
		byte(w >> 24), byte(w >> 16), byte(w >> 8), byte(w),
		byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h),
		8, 2, 0, 0, 0}
	out = append(out, typeAndData...)
	crc := crc32IEEE(typeAndData)
	out = append(out, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	return out
}

func crc32IEEE(b []byte) uint32 {
	// Standalone IEEE CRC-32, polynomial 0xEDB88320. Self-contained so the
	// test file doesn't pull in another import.
	crc := uint32(0xFFFFFFFF)
	for _, x := range b {
		crc ^= uint32(x)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xFFFFFFFF
}

// ─── Small utility used across tests ────────────────────────────────────────

func readAll(resp *http.Response) string {
	defer resp.Body.Close() //nolint:errcheck
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// TestImageHandler_persistManualImage_HappyPath pins the contract of
// the shared persistence helper that Select and Upload both call. The
// integration tests above exercise the helper through HTTP, but those
// tests would still pass if a future refactor accidentally dropped one
// of the helper's nine steps. This test calls the helper directly so
// each step's effect (file on disk, DB row, IsLocked, IsPrimary,
// pathmap entry, response Path) is asserted independently.
func TestImageHandler_persistManualImage_HappyPath(t *testing.T) {
	env := newImageTestEnv(t)
	data := makeJPEG(t, 32, 32)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	img, err := env.handler.persistManualImage(req, "item-mp", "primary", data, "image/jpeg", "upload", 100, 200)
	if err != nil {
		t.Fatalf("persistManualImage: %v", err)
	}
	if img == nil {
		t.Fatal("nil image returned")
	}

	// 1) File written to disk under <imageDir>/<itemID>/
	entries, err := os.ReadDir(filepath.Join(env.imageDir, "item-mp"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 file on disk, got entries=%d err=%v", len(entries), err)
	}

	// 2) DB row was created with the right shape.
	stored, ok := env.images.images[img.ID]
	if !ok {
		t.Fatalf("image %q not stored", img.ID)
	}
	// 3) IsLocked = true (manual selection lock).
	if !stored.IsLocked {
		t.Error("expected IsLocked=true (manual pick)")
	}
	// 4) Provider tag flowed through.
	if stored.Provider != "upload" {
		t.Errorf("provider: got %q want %q", stored.Provider, "upload")
	}
	// 5) Width / height preserved from the call.
	if stored.Width != 100 || stored.Height != 200 {
		t.Errorf("dimensions: got %dx%d want 100x200", stored.Width, stored.Height)
	}
	// 6) Path is the API URL, not the on-disk path (clients hit the
	// resolver endpoint, never the local FS).
	wantPath := "/api/v1/images/file/" + img.ID
	if stored.Path != wantPath {
		t.Errorf("Path: got %q want %q", stored.Path, wantPath)
	}
	// 7) IsPrimary flipped to true via SetPrimary (the helper does
	// this best-effort; the in-memory fake never errors).
	if !img.IsPrimary {
		t.Error("expected IsPrimary=true after promotion")
	}
	// 8) Blurhash + dominant colours were computed from the bytes.
	if stored.Blurhash == "" {
		t.Error("expected non-empty blurhash for a decodable JPEG")
	}
	// 9) Pathmap entry exists so /images/file/<id> can resolve.
	mapped, err := env.handler.pathmap.Read(img.ID)
	if err != nil || mapped == "" {
		t.Errorf("pathmap entry missing: err=%v mapped=%q", err, mapped)
	}
}

// Defensive: ensure our fakes really satisfy the package interfaces at compile time.
var (
	_ ImageRepository      = (*fakeImageRepo)(nil)
	_ ExternalIDRepository = (*fakeExternalIDRepo)(nil)
	_ ItemRepository       = (*fakeItemRepo)(nil)
	_ ProviderManager      = (*fakeProviderManager)(nil)
)

// Silence unused-import complaints if any test path stops using errors.
var _ = errors.New
