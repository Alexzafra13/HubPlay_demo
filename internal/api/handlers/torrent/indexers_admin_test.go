package torrenthandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
	"hubplay/internal/torrentstream"
)

// fakeStore implements IndexerStoreAPI for the handler tests.
type fakeStore struct {
	statuses []torrentstream.IndexerStatus
	added    *torrentstream.IndexerInput
	updated  string
	deleted  string
	updErr   error
}

func (f *fakeStore) Statuses(context.Context) ([]torrentstream.IndexerStatus, error) {
	return f.statuses, nil
}
func (f *fakeStore) Add(_ context.Context, in torrentstream.IndexerInput) (torrentstream.IndexerRecord, error) {
	f.added = &in
	return torrentstream.IndexerRecord{ID: "new-id", IndexerInput: in}, nil
}
func (f *fakeStore) Update(_ context.Context, id string, _ torrentstream.IndexerInput) error {
	f.updated = id
	return f.updErr
}
func (f *fakeStore) Delete(_ context.Context, id string) error {
	f.deleted = id
	return nil
}

type fakeInvalidator struct{ calls int }

func (f *fakeInvalidator) Invalidate() { f.calls++ }

func withID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestIndexerAdmin_List(t *testing.T) {
	store := &fakeStore{statuses: []torrentstream.IndexerStatus{
		{IndexerInfo: torrentstream.IndexerInfo{ID: "1", Name: "prowlarr", Enabled: true, HasAPIKey: true}, Reachable: true},
	}}
	h := NewIndexerAdminHandler(store, nil, nil)
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
	h := NewIndexerAdminHandler(nil, nil, nil)
	rr := httptest.NewRecorder()
	h.List(rr, httptest.NewRequest(http.MethodGet, "/admin/indexers", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

func TestIndexerAdmin_Create(t *testing.T) {
	store := &fakeStore{}
	inv := &fakeInvalidator{}
	h := NewIndexerAdminHandler(store, inv, nil)
	rr := httptest.NewRecorder()
	body := `{"name":"prowlarr","base_url":"http://localhost:9696","api_key":"k","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/admin/indexers", strings.NewReader(body))
	h.Create(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d want 201 (%s)", rr.Code, rr.Body.String())
	}
	if store.added == nil || store.added.Name != "prowlarr" {
		t.Errorf("store.Add not called correctly: %+v", store.added)
	}
	if inv.calls != 1 {
		t.Errorf("create should invalidate cache once, got %d", inv.calls)
	}
	// Secret must not be echoed.
	if strings.Contains(rr.Body.String(), `"api_key"`) || strings.Contains(rr.Body.String(), "k\"") {
		t.Errorf("response leaked api key: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"has_api_key":true`) {
		t.Errorf("response should expose has_api_key: %s", rr.Body.String())
	}
}

func TestIndexerAdmin_CreateValidation(t *testing.T) {
	h := NewIndexerAdminHandler(&fakeStore{}, nil, nil)
	rr := httptest.NewRecorder()
	// Missing name + url.
	req := httptest.NewRequest(http.MethodPost, "/admin/indexers", strings.NewReader(`{"api_key":"k"}`))
	h.Create(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIndexerAdmin_UpdateNotFound(t *testing.T) {
	store := &fakeStore{updErr: domain.ErrNotFound}
	h := NewIndexerAdminHandler(store, &fakeInvalidator{}, nil)
	rr := httptest.NewRecorder()
	body := `{"name":"x","url":"http://y/torznab"}`
	req := withID(httptest.NewRequest(http.MethodPut, "/admin/indexers/nope", strings.NewReader(body)), "nope")
	h.Update(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404 (%s)", rr.Code, rr.Body.String())
	}
}

func TestIndexerAdmin_Delete(t *testing.T) {
	store := &fakeStore{}
	inv := &fakeInvalidator{}
	h := NewIndexerAdminHandler(store, inv, nil)
	rr := httptest.NewRecorder()
	req := withID(httptest.NewRequest(http.MethodDelete, "/admin/indexers/abc", nil), "abc")
	h.Delete(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rr.Code)
	}
	if store.deleted != "abc" {
		t.Errorf("delete id: got %q", store.deleted)
	}
	if inv.calls != 1 {
		t.Errorf("delete should invalidate cache")
	}
}

func TestIndexerAdmin_TestMissingTarget(t *testing.T) {
	h := NewIndexerAdminHandler(&fakeStore{}, nil, nil)
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

	h := NewIndexerAdminHandler(&fakeStore{}, nil, nil)
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
