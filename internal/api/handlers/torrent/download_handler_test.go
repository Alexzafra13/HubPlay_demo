package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/torrentstream"
)

// withURLParam inyecta un parámetro de ruta chi (p.ej. {id}) en la request,
// como haría el router en producción.
func withURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

type fakeDownloader struct {
	started   bool
	src, dest string
	onDone    func(error)
	jobs      []torrentstream.DownloadJob
	removed   string
	removeOK  bool
	removeErr error
}

func (f *fakeDownloader) StartDownload(src, destDir string, onDone func(error)) torrentstream.DownloadJob {
	f.started = true
	f.src = src
	f.dest = destDir
	f.onDone = onDone
	return torrentstream.DownloadJob{ID: "j1", Status: torrentstream.DownloadQueued}
}

func (f *fakeDownloader) Downloads() []torrentstream.DownloadJob { return f.jobs }

func (f *fakeDownloader) RemoveDownload(id string) (bool, error) {
	f.removed = id
	return f.removeOK, f.removeErr
}

type fakeLibTarget struct {
	id, dir string
	err     error
	scanned string
}

func (f *fakeLibTarget) DownloadDir(_ context.Context, _ torrentstream.MediaType) (string, string, error) {
	return f.id, f.dir, f.err
}
func (f *fakeLibTarget) Scan(_ context.Context, id string) error {
	f.scanned = id
	return nil
}

func TestDownload_Create_AdminStartsAndScans(t *testing.T) {
	dl := &fakeDownloader{}
	lib := &fakeLibTarget{id: "lib-1", dir: "/media/pelis/Descargas"}
	h := NewDownloadHandler(dl, lib, adminTrue, nil)

	rr := httptest.NewRecorder()
	body := `{"src":"magnet:?xt=urn:btih:abc","type":"movie"}`
	req := httptest.NewRequest(http.MethodPost, "/torrent/download", strings.NewReader(body))
	h.Create(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202 (%s)", rr.Code, rr.Body.String())
	}
	if !dl.started || dl.dest != "/media/pelis/Descargas" {
		t.Errorf("StartDownload not called right: started=%v dest=%q", dl.started, dl.dest)
	}
	// onDone(nil) must trigger a rescan of the resolved library.
	dl.onDone(nil)
	if lib.scanned != "lib-1" {
		t.Errorf("expected rescan of lib-1, got %q", lib.scanned)
	}
}

func TestDownload_Create_NonAdminForbidden(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{}, &fakeLibTarget{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/torrent/download", strings.NewReader(`{"src":"magnet:?xt=urn:btih:abc"}`))
	h.Create(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", rr.Code)
	}
}

func TestDownload_Create_InvalidSource(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{}, &fakeLibTarget{}, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/torrent/download", strings.NewReader(`{"src":"ftp://x/y"}`))
	h.Create(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestDownload_Create_NoLibrary(t *testing.T) {
	lib := &fakeLibTarget{err: ErrNoLibrary}
	h := NewDownloadHandler(&fakeDownloader{}, lib, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/torrent/download", strings.NewReader(`{"src":"magnet:?xt=urn:btih:abc","type":"movie"}`))
	h.Create(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d want 422 (%s)", rr.Code, rr.Body.String())
	}
}

func TestDownload_List(t *testing.T) {
	dl := &fakeDownloader{jobs: []torrentstream.DownloadJob{
		{ID: "j1", Name: "Movie", Status: torrentstream.DownloadDownloading, BytesDone: 5, BytesTotal: 10},
	}}
	h := NewDownloadHandler(dl, &fakeLibTarget{}, adminTrue, nil)
	rr := httptest.NewRecorder()
	h.List(rr, httptest.NewRequest(http.MethodGet, "/torrent/downloads", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"name":"Movie"`) {
		t.Errorf("body: %s", rr.Body.String())
	}
}

func TestDownload_List_NonAdminForbidden(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{}, &fakeLibTarget{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	h.List(rr, httptest.NewRequest(http.MethodGet, "/torrent/downloads", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", rr.Code)
	}
}

func TestDownload_Delete_Removes(t *testing.T) {
	dl := &fakeDownloader{removeOK: true}
	h := NewDownloadHandler(dl, &fakeLibTarget{}, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/torrent/downloads/j1", nil), "id", "j1")
	h.Delete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204 (%s)", rr.Code, rr.Body.String())
	}
	if dl.removed != "j1" {
		t.Errorf("RemoveDownload called with %q, want j1", dl.removed)
	}
}

func TestDownload_Delete_NotFound(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{removeOK: false}, &fakeLibTarget{}, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/torrent/downloads/nope", nil), "id", "nope")
	h.Delete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

func TestDownload_Delete_Active(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{removeErr: torrentstream.ErrDownloadActive}, &fakeLibTarget{}, adminTrue, nil)
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/torrent/downloads/j1", nil), "id", "j1")
	h.Delete(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status: got %d want 409", rr.Code)
	}
}

func TestDownload_Delete_NonAdminForbidden(t *testing.T) {
	h := NewDownloadHandler(&fakeDownloader{}, &fakeLibTarget{}, adminFalse, nil)
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodDelete, "/torrent/downloads/j1", nil), "id", "j1")
	h.Delete(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", rr.Code)
	}
}
