package handlers_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/api/handlers"
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
	repo := &fakeCollectionRepo{
		result: &librarymodel.Collection{
			ID:     "collection:9485",
			TMDBID: 9485,
			Name:   "A todo gas - Colección",
		},
	}
	h := handlers.NewCollectionHandler(repo, slog.Default())

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
	repo := &fakeCollectionRepo{
		result: &librarymodel.Collection{
			ID:     "collection:9485",
			TMDBID: 9485,
			Name:   "A todo gas",
		},
	}
	h := handlers.NewCollectionHandler(repo, slog.Default())

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
