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
