package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"
)

// fakeListForUserSvc satisface handlers.LibraryLister — interface
// estrecha que el UploadBrowseHandler usa. El handler NO tipa contra
// LibraryService entera precisamente para que tests como éste no
// tengan que implementar 8 stubs de panic.
type fakeListForUserSvc struct {
	libs []*librarymodel.Library
}

func (f *fakeListForUserSvc) ListForUser(_ context.Context, _ string) ([]*librarymodel.Library, error) {
	return f.libs, nil
}

func mountBrowse(h *handlers.UploadBrowseHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/libraries/{id}/upload-browse", h.Browse)
	r.Post("/libraries/{id}/folders", h.CreateFolder)
	return r
}

func doBrowseRequest(handler http.Handler, method, path, body, userID string) *httptest.ResponseRecorder {
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	var req *http.Request
	if rdr != nil {
		req = httptest.NewRequest(method, path, rdr)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if userID != "" {
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: userID}))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func makeLib(t *testing.T, id, name string) (*librarymodel.Library, string) {
	t.Helper()
	root := t.TempDir()
	return &librarymodel.Library{
		ID:    id,
		Name:  name,
		Paths: []string{root},
	}, root
}

// ─── Browse ─────────────────────────────────────────────────────────

func TestUploadBrowse_ListsSubdirs(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	// Crea sub-dirs reales.
	_ = os.MkdirAll(filepath.Join(root, "Action"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "Drama"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".hidden"), 0o755) // se filtra
	// Y un fichero — se filtra (sólo dirs).
	_ = os.WriteFile(filepath.Join(root, "readme.txt"), []byte("x"), 0o644)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodGet,
		"/libraries/lib-mov/upload-browse", "", "u-alex")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Data struct {
			LibraryID   string `json:"library_id"`
			Path        string `json:"path"`
			Directories []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"directories"`
			Files []struct {
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"files"`
		} `json:"data"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&payload)
	if payload.Data.LibraryID != "lib-mov" {
		t.Errorf("library_id = %s", payload.Data.LibraryID)
	}
	if payload.Data.Path != "" {
		t.Errorf("root path should be empty, got %q", payload.Data.Path)
	}
	if len(payload.Data.Directories) != 2 {
		t.Fatalf("dirs = %v (want 2: Action, Drama; hidden filtrado)", payload.Data.Directories)
	}
	// Orden alfabético: Action antes que Drama.
	if payload.Data.Directories[0].Name != "Action" {
		t.Errorf("order: %v", payload.Data.Directories)
	}
	// readme.txt aparece en files (dotfiles filtrados, ficheros normales sí).
	if len(payload.Data.Files) != 1 {
		t.Errorf("files = %v (want 1: readme.txt)", payload.Data.Files)
	} else if payload.Data.Files[0].Name != "readme.txt" {
		t.Errorf("file name = %q", payload.Data.Files[0].Name)
	}
}

func TestUploadBrowse_RejectsTraversal(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodGet,
		"/libraries/lib-mov/upload-browse?path=..%2Fescape", "", "u-alex")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}

func TestUploadBrowse_NotFoundOnLibraryWithoutAccess(t *testing.T) {
	// El user no tiene acceso a "lib-secret" → 404 (NO 403).
	svc := &fakeListForUserSvc{libs: nil}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodGet,
		"/libraries/lib-secret/upload-browse", "", "u-alex")
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d (want 404)", rr.Code)
	}
}

// ─── CreateFolder ───────────────────────────────────────────────────

func TestUploadBrowse_CreateFolder_HappyPath(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodPost,
		"/libraries/lib-mov/folders",
		`{"path":"Movies/Drama"}`, "u-alex")
	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	// El dir existe en disco.
	if _, err := os.Stat(filepath.Join(root, "Movies", "Drama")); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestUploadBrowse_CreateFolder_Idempotent(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.MkdirAll(filepath.Join(root, "AlreadyHere"), 0o755)
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodPost,
		"/libraries/lib-mov/folders",
		`{"path":"AlreadyHere"}`, "u-alex")
	if rr.Code != http.StatusCreated {
		t.Errorf("status %d (want 201 idempotent)", rr.Code)
	}
}

func TestUploadBrowse_CreateFolder_RejectsEmpty(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodPost,
		"/libraries/lib-mov/folders",
		`{"path":""}`, "u-alex")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}

func TestUploadBrowse_CreateFolder_RejectsTraversal(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowse(h), http.MethodPost,
		"/libraries/lib-mov/folders",
		`{"path":"../escape"}`, "u-alex")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400)", rr.Code)
	}
}

// ─── DeleteEntry ────────────────────────────────────────────────────

func mountBrowseFull(h *handlers.UploadBrowseHandler) http.Handler {
	r := chi.NewRouter()
	r.Get("/libraries/{id}/upload-browse", h.Browse)
	r.Post("/libraries/{id}/folders", h.CreateFolder)
	r.Delete("/libraries/{id}/files", h.DeleteEntry)
	r.Post("/libraries/{id}/files/rename", h.RenameEntry)
	return r
}

func TestUploadBrowse_DeleteFile(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	path := filepath.Join(root, "movie.mkv")
	_ = os.WriteFile(path, []byte("data"), 0o640)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=movie.mkv", "", "u-alex")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file not deleted: %v", err)
	}
}

func TestUploadBrowse_DeleteEmptyDir(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.MkdirAll(filepath.Join(root, "empty"), 0o755)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=empty", "", "u-alex")
	if rr.Code != http.StatusNoContent {
		t.Errorf("status %d (empty dir delete should succeed)", rr.Code)
	}
}

func TestUploadBrowse_DeleteNonEmptyDir_RequiresRecursive(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.MkdirAll(filepath.Join(root, "movies", "drama"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "movies", "drama", "x.mkv"), []byte("x"), 0o640)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	// Sin recursive: 409 CONFLICT.
	rr := doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=movies", "", "u-alex")
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d (want 409 without recursive)", rr.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "movies")); err != nil {
		t.Errorf("dir was deleted despite the conflict: %v", err)
	}

	// Con recursive: borra entero.
	rr = doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=movies&recursive=true", "", "u-alex")
	if rr.Code != http.StatusNoContent {
		t.Errorf("status %d (want 204 with recursive)", rr.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "movies")); !os.IsNotExist(err) {
		t.Errorf("dir not deleted: %v", err)
	}
}

func TestUploadBrowse_DeleteMissing_Idempotent(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=ghost.mkv", "", "u-alex")
	if rr.Code != http.StatusNoContent {
		t.Errorf("status %d (idempotent delete should be 204)", rr.Code)
	}
}

func TestUploadBrowse_DeleteRoot_Rejected(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodDelete,
		"/libraries/lib-mov/files?path=", "", "u-alex")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400 — cannot delete root)", rr.Code)
	}
}

// ─── RenameEntry ────────────────────────────────────────────────────

func TestUploadBrowse_Rename_File(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.WriteFile(filepath.Join(root, "old.mkv"), []byte("data"), 0o640)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodPost,
		"/libraries/lib-mov/files/rename",
		`{"from":"old.mkv","to":"new.mkv"}`, "u-alex")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "new.mkv")); err != nil {
		t.Errorf("renamed file not at destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.mkv")); !os.IsNotExist(err) {
		t.Errorf("source still exists after rename: %v", err)
	}
}

func TestUploadBrowse_Rename_RejectsExistingTarget(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.WriteFile(filepath.Join(root, "old.mkv"), []byte("a"), 0o640)
	_ = os.WriteFile(filepath.Join(root, "new.mkv"), []byte("b"), 0o640)

	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodPost,
		"/libraries/lib-mov/files/rename",
		`{"from":"old.mkv","to":"new.mkv"}`, "u-alex")
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d (want 409 TO_EXISTS)", rr.Code)
	}
	// Source intacto, no se pisó.
	body, _ := os.ReadFile(filepath.Join(root, "old.mkv"))
	if string(body) != "a" {
		t.Errorf("source mutated despite conflict: %q", body)
	}
}

func TestUploadBrowse_Rename_MissingSource(t *testing.T) {
	lib, _ := makeLib(t, "lib-mov", "Movies")
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodPost,
		"/libraries/lib-mov/files/rename",
		`{"from":"ghost.mkv","to":"new.mkv"}`, "u-alex")
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d (want 404 FROM_NOT_FOUND)", rr.Code)
	}
}

func TestUploadBrowse_Rename_SamePath_Rejected(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.WriteFile(filepath.Join(root, "same.mkv"), []byte("x"), 0o640)
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodPost,
		"/libraries/lib-mov/files/rename",
		`{"from":"same.mkv","to":"same.mkv"}`, "u-alex")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d (want 400 SAME_PATH)", rr.Code)
	}
}

func TestUploadBrowse_Rename_CreatesIntermediateDirs(t *testing.T) {
	lib, root := makeLib(t, "lib-mov", "Movies")
	_ = os.WriteFile(filepath.Join(root, "loose.mkv"), []byte("x"), 0o640)
	svc := &fakeListForUserSvc{libs: []*librarymodel.Library{lib}}
	h := handlers.NewUploadBrowseHandler(svc, testutil.NopLogger())

	rr := doBrowseRequest(mountBrowseFull(h), http.MethodPost,
		"/libraries/lib-mov/files/rename",
		`{"from":"loose.mkv","to":"2024/Movie/loose.mkv"}`, "u-alex")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "2024", "Movie", "loose.mkv")); err != nil {
		t.Errorf("file not at nested destination: %v", err)
	}
}
