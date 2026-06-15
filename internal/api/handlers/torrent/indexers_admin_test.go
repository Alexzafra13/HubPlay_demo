package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hubplay/internal/torrentstream"
)

type fakeIndexerAPI struct {
	status []torrentstream.IndexerStatus
}

func (f fakeIndexerAPI) Status(context.Context) []torrentstream.IndexerStatus {
	return f.status
}

func TestIndexerAdmin_List(t *testing.T) {
	api := fakeIndexerAPI{status: []torrentstream.IndexerStatus{
		{IndexerInfo: torrentstream.IndexerInfo{Name: "prowlarr", HasAPIKey: true}, Reachable: true},
	}}
	h := NewIndexerAdminHandler(api, nil)
	rr := httptest.NewRecorder()
	h.List(rr, httptest.NewRequest(http.MethodGet, "/admin/indexers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"reachable":true`) ||
		!strings.Contains(rr.Body.String(), `"name":"prowlarr"`) {
		t.Errorf("body: %s", rr.Body.String())
	}
}

func TestIndexerAdmin_ListDisabled(t *testing.T) {
	h := NewIndexerAdminHandler(nil, nil)
	rr := httptest.NewRecorder()
	h.List(rr, httptest.NewRequest(http.MethodGet, "/admin/indexers", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

func TestIndexerAdmin_TestMissingTarget(t *testing.T) {
	h := NewIndexerAdminHandler(fakeIndexerAPI{}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/indexers/test", strings.NewReader(`{}`))
	h.Test(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIndexerAdmin_TestConnectsToServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server/></caps>`))
	}))
	defer srv.Close()

	h := NewIndexerAdminHandler(fakeIndexerAPI{}, nil)
	rr := httptest.NewRecorder()
	body := `{"url":"` + srv.URL + `/torznab","api_key":"k"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/indexers/test", strings.NewReader(body))
	h.Test(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Errorf("body: %s", rr.Body.String())
	}
}
