package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	stdpng "image/png"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers/media"
	librarymodel "hubplay/internal/library/model"
)

// fakeCollectionRepo records the id passed to GetByID so the test can
// assert the handler decoded it before looking up. ListItemsForCollection
// returns an empty slice — exercising the not-found branch only requires
// a positive GetByID with no children.
type fakeCollectionRepo struct {
	gotID  string
	result *librarymodel.Collection
}

func (f *fakeCollectionRepo) GetByID(_ context.Context, id string) (*librarymodel.Collection, error) {
	f.gotID = id
	return f.result, nil
}

func (f *fakeCollectionRepo) List(_ context.Context) ([]*librarymodel.CollectionListEntry, error) {
	return nil, nil
}

func (f *fakeCollectionRepo) ListItemsForCollection(_ context.Context, _ string) ([]*librarymodel.CollectionItem, error) {
	return nil, nil
}

// TestCollectionHandler_Get_DecodesPercentEncodedID is the regression
// test for the prod 404. Frontend navigation to a collection card uses
// `encodeURIComponent("collection:<tmdb_id>")`, which turns the colon
// into `%3A`. chi v5 returns URL params raw, so without a
// url.PathUnescape on the way in the handler queried the DB for the
// literal string "collection%3A9485" — never matching anything,
// always 404. Pin the decode behaviour so a future refactor can't
// regress the saga page back into a "Colección no encontrada" loop.
func TestCollectionHandler_Get_DecodesPercentEncodedID(t *testing.T) {
	t.Parallel()
	repo := &fakeCollectionRepo{
		result: &librarymodel.Collection{
			ID:     "collection:9485",
			TMDBID: 9485,
			Name:   "A todo gas - Colección",
		},
	}
	h := media.NewCollectionHandler(repo, nil, nil, "", nil, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/collections/collection%3A9485", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "collection%3A9485")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (want 200); body=%s", rr.Code, rr.Body.String())
	}
	if repo.gotID != "collection:9485" {
		t.Errorf("repo received id = %q, want %q (handler must url.PathUnescape)", repo.gotID, "collection:9485")
	}

	var body struct {
		Data struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.ID != "collection:9485" {
		t.Errorf("response id = %q, want collection:9485", body.Data.ID)
	}
}

// TestCollectionHandler_Get_PassesUnescapedIDThrough covers the path
// where the frontend (or a curl user) hits the endpoint without
// percent-encoding the colon. PathUnescape is a no-op on a value that
// has no escape sequences, so the handler should still resolve the
// row.
func TestCollectionHandler_Get_PassesUnescapedIDThrough(t *testing.T) {
	t.Parallel()
	repo := &fakeCollectionRepo{
		result: &librarymodel.Collection{
			ID:     "collection:9485",
			TMDBID: 9485,
			Name:   "A todo gas",
		},
	}
	h := media.NewCollectionHandler(repo, nil, nil, "", nil, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/collections/collection:9485", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "collection:9485")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	h.Get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (want 200); body=%s", rr.Code, rr.Body.String())
	}
	if repo.gotID != "collection:9485" {
		t.Errorf("repo received id = %q, want %q (no double-decoding)", repo.gotID, "collection:9485")
	}
}

// fakeOverrideRepo registra los UpsertFile para que el test vea si el
// handler llegó a guardar un override.
type fakeOverrideRepo struct {
	upserts []string
}

func (f *fakeOverrideRepo) UpsertURL(context.Context, string, string, string) error { return nil }
func (f *fakeOverrideRepo) UpsertFile(_ context.Context, _, _, basename string) error {
	f.upserts = append(f.upserts, basename)
	return nil
}
func (f *fakeOverrideRepo) Delete(context.Context, string, string) error { return nil }
func (f *fakeOverrideRepo) Get(context.Context, string, string) (*librarymodel.CollectionImageOverride, error) {
	return nil, nil
}
func (f *fakeOverrideRepo) ListByCollection(context.Context, string) ([]librarymodel.CollectionImageOverride, error) {
	return nil, nil
}

// TestCollectionHandler_UploadImage_RejectsTraversalID: el id llega
// decodificado (PathUnescape) y forma parte del nombre del fichero, así
// que "..%2F..%2Fescape" escribía la imagen dos carpetas por encima de
// collection-images/. Debe ser 400 sin tocar disco ni la BD.
func TestCollectionHandler_UploadImage_RejectsTraversalID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	imageDir := filepath.Join(root, "a", "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	overrides := &fakeOverrideRepo{}
	h := media.NewCollectionHandler(&fakeCollectionRepo{}, overrides, nil, imageDir, nil, slog.Default())

	var png bytes.Buffer
	if err := stdpng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "poster.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(png.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/collections/x/images/poster/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "..%2F..%2Fescape")
	rctx.URLParams.Add("type", "poster")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()

	h.UploadCollectionImage(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (want 400); body=%s", rr.Code, rr.Body.String())
	}
	if len(overrides.upserts) != 0 {
		t.Errorf("override guardado con id inseguro: %v", overrides.upserts)
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Errorf("fichero escrito: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
