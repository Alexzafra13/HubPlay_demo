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
	env.handler = NewImageHandler(env.images, env.externals, env.items, env.providers, imageDir, testutil.NopLogger())

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

func TestImageHandler_ServeFile_RemoteFallbackRedirect(t *testing.T) {
	env := newImageTestEnv(t)
	// Image exists in DB with http:// path but no local mapping → expect 307 redirect.
	env.images.images["remote-1"] = &db.Image{
		ID:     "remote-1",
		ItemID: "item-1",
		Type:   "primary",
		Path:   "https://cdn.example.com/x.jpg",
	}
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, _ := http.NewRequest(http.MethodGet, env.server.URL+"/api/v1/images/file/remote-1", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status: got %d want 307", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://cdn.example.com/x.jpg" {
		t.Fatalf("Location: got %q", got)
	}
}

// ─── Tests: RefreshLibraryImages ────────────────────────────────────────────

func TestImageHandler_RefreshLibraryImages_AddsMissingTypes(t *testing.T) {
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

// ─── Pure helper tests (anchor current behavior before moving them) ────────

func TestIsValidImageType(t *testing.T) {
	cases := map[string]bool{
		"primary":  true,
		"backdrop": true,
		"logo":     true,
		"thumb":    true,
		"banner":   true,
		"":         false,
		"bogus":    false,
		"PRIMARY":  false,
	}
	for in, want := range cases {
		if got := isValidImageType(in); got != want {
			t.Errorf("isValidImageType(%q) = %v want %v", in, got, want)
		}
	}
}

func TestIsValidImageContentType(t *testing.T) {
	cases := map[string]bool{
		"image/jpeg":              true,
		"image/jpeg; charset=x":   true,
		"image/png":               true,
		"image/webp":              true,
		"image/gif":               false,
		"text/html":               false,
		"application/octet-stream": false,
		"":                        false,
	}
	for in, want := range cases {
		if got := isValidImageContentType(in); got != want {
			t.Errorf("isValidImageContentType(%q) = %v want %v", in, got, want)
		}
	}
}

func TestExtensionForContentType(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".jpg", // current default-case fallthrough
		"":           ".jpg",
	}
	for in, want := range cases {
		if got := extensionForContentType(in); got != want {
			t.Errorf("extensionForContentType(%q) = %q want %q", in, got, want)
		}
	}
}

// ─── Small utility used across tests ────────────────────────────────────────

func readAll(resp *http.Response) string {
	defer resp.Body.Close() //nolint:errcheck
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
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
