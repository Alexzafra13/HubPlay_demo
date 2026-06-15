package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hubplay/internal/torrentstream"
)

type fakeDownloader struct {
	started   bool
	src, dest string
	onDone    func(error)
	jobs      []torrentstream.DownloadJob
}

func (f *fakeDownloader) StartDownload(src, destDir string, onDone func(error)) torrentstream.DownloadJob {
	f.started = true
	f.src = src
	f.dest = destDir
	f.onDone = onDone
	return torrentstream.DownloadJob{ID: "j1", Status: torrentstream.DownloadQueued}
}

func (f *fakeDownloader) Downloads() []torrentstream.DownloadJob { return f.jobs }

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
