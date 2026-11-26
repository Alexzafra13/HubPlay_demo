package fedhandler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
	"hubplay/internal/federation"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"
)

// childFakeRepo: una serie con dos temporadas y una película colgada
// por error del mismo padre (no debe salir: solo temporadas/episodios).
type childFakeRepo struct {
	byID     map[string]*librarymodel.Item
	children map[string][]*librarymodel.Item
}

func (r *childFakeRepo) GetByID(_ context.Context, id string) (*librarymodel.Item, error) {
	if it, ok := r.byID[id]; ok {
		return it, nil
	}
	return nil, domain.NewNotFound("item")
}

func (r *childFakeRepo) GetChildren(_ context.Context, parentID string) ([]*librarymodel.Item, error) {
	return r.children[parentID], nil
}

func intPtr(v int) *int { return &v }

// childrenEnv monta el handler de hijos sobre el entorno federado real
// (manager + peer emparejado + JWT) de federation_stream_test.go.
func childrenEnv(t *testing.T) (*fedTestEnv, string, *httptest.Server) {
	t.Helper()
	env := newFedTestEnv(t)
	seriesID := "series-1"
	repo := &childFakeRepo{
		byID: map[string]*librarymodel.Item{
			seriesID: {ID: seriesID, LibraryID: env.libraryID, Type: "series", Title: "Oscuro"},
		},
		children: map[string][]*librarymodel.Item{
			seriesID: {
				{ID: "s0", LibraryID: env.libraryID, Type: "season", Title: "Especiales", SeasonNumber: intPtr(0)},
				{ID: "s1", LibraryID: env.libraryID, Type: "season", Title: "Temporada 1", SeasonNumber: intPtr(1)},
				{ID: "stray", LibraryID: env.libraryID, Type: "movie", Title: "No soy hijo"},
			},
		},
	}
	images := &fakeImageRepoForFedTest{primaryByItem: map[string]string{"s1": "img-s1"}}
	h := NewFederationChildrenHandler(env.mgr, repo, images, testutil.NopLogger())

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(federation.RequirePeerJWT(env.mgr))
		r.Get("/api/v1/peer/items/{itemId}/children", h.ItemChildren)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return env, seriesID, srv
}

func getChildren(t *testing.T, env *fedTestEnv, srv *httptest.Server, itemID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/peer/items/"+itemID+"/children", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+env.peerToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestFederationChildren_ListsSeasonsInOrderWithPosters(t *testing.T) {
	env, seriesID, srv := childrenEnv(t)
	env.share(federation.ShareScopes{CanBrowse: true})

	resp := getChildren(t, env, srv, seriesID)
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []federation.SharedItem `json:"items"`
		Total int                     `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("items = %+v, want the two seasons only", body.Items)
	}
	s0, s1 := body.Items[0], body.Items[1]
	if s0.ID != "s0" || s0.SeasonNumber == nil || *s0.SeasonNumber != 0 {
		t.Errorf("first = %+v, want season 0 (specials) with its number kept", s0)
	}
	if s0.HasPoster || !s1.HasPoster {
		t.Errorf("has_poster = %v/%v, want false/true", s0.HasPoster, s1.HasPoster)
	}
}

// Sin share, o con share sin can_browse, el peer recibe el mismo 404
// que para un id inexistente: no puede enumerar el catálogo.
func TestFederationChildren_NoBrowse_Returns404LikeMissing(t *testing.T) {
	env, seriesID, srv := childrenEnv(t)

	for _, tc := range []struct {
		name  string
		share *federation.ShareScopes
		item  string
	}{
		{"no share", nil, seriesID},
		{"play without browse", &federation.ShareScopes{CanPlay: true}, seriesID},
		{"missing item", &federation.ShareScopes{CanBrowse: true}, "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.share != nil {
				env.share(*tc.share)
			}
			resp := getChildren(t, env, srv, tc.item)
			defer resp.Body.Close() //nolint:errcheck
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			var body map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&body)
			errObj, _ := body["error"].(map[string]any)
			if code, _ := errObj["code"].(string); code != "ITEM_NOT_FOUND" {
				t.Errorf("error.code = %q, want ITEM_NOT_FOUND", code)
			}
		})
	}
}
