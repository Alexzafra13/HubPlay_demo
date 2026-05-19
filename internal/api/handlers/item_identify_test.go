package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
	"hubplay/internal/testutil"
)

// fakeIdentifier implementa MetadataIdentifier para los tests de los
// handlers de identify. Guarda lo que se le ha pedido para que los
// asserts comprueben que el handler pasa exactamente los argumentos
// correctos al servicio (item id, query, year, external id).
type fakeIdentifier struct {
	searchCalledWith struct {
		itemID, query string
		year          int
	}
	searchResults []provider.SearchResult
	searchErr     error

	applyCalledWith struct {
		itemID, externalID string
	}
	applyErr error
}

func (f *fakeIdentifier) SearchCandidates(_ context.Context, itemID, query string, year int) ([]provider.SearchResult, error) {
	f.searchCalledWith.itemID = itemID
	f.searchCalledWith.query = query
	f.searchCalledWith.year = year
	return f.searchResults, f.searchErr
}

func (f *fakeIdentifier) IdentifyAndApply(_ context.Context, itemID, externalID string) error {
	f.applyCalledWith.itemID = itemID
	f.applyCalledWith.externalID = externalID
	return f.applyErr
}

func (f *fakeIdentifier) UpdateItemMetadata(_ context.Context, _ string, _ scanner.ItemMetadataPatch) (*librarymodel.Item, error) {
	return &librarymodel.Item{}, nil
}

func (f *fakeIdentifier) SetMetadataLock(_ context.Context, _ string, _ bool) error {
	return nil
}

func (f *fakeIdentifier) IsMetadataLocked(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeIdentifier) RefreshItemMetadata(_ context.Context, _ string) error {
	return nil
}

// identifyEnv monta un router mínimo con sólo los dos endpoints de
// identify. Reusa el ItemHandler real porque la única dep que importa
// para estos tests es `identifier`; el resto se pasa nil.
type identifyEnv struct {
	handler *ItemHandler
	id      *fakeIdentifier
	router  chi.Router
}

func newIdentifyEnv(t *testing.T, id *fakeIdentifier) identifyEnv {
	t.Helper()
	handler := NewItemHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, id, "", nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Route("/api/v1/items/{id}", func(r chi.Router) {
		r.Get("/identify/candidates", handler.IdentifyCandidates)
		r.Post("/identify", handler.Identify)
	})
	return identifyEnv{handler: handler, id: id, router: r}
}

func TestIdentifyCandidates_Success(t *testing.T) {
	id := &fakeIdentifier{
		searchResults: []provider.SearchResult{
			{ExternalID: "550", Title: "Fight Club", Year: 1999, Overview: "An insomniac...", PosterURL: "https://image.tmdb.org/poster.jpg", Score: 0.95},
			{ExternalID: "12345", Title: "Fight Club (TV)", Year: 2020, Overview: "Unrelated.", PosterURL: "", Score: 0.10},
		},
	}
	env := newIdentifyEnv(t, id)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/item-1/identify/candidates?query=fight+club&year=1999", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", w.Code, w.Body.String())
	}

	if id.searchCalledWith.itemID != "item-1" || id.searchCalledWith.query != "fight club" || id.searchCalledWith.year != 1999 {
		t.Fatalf("SearchCandidates called with wrong args: %+v", id.searchCalledWith)
	}

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(resp.Data))
	}
	first := resp.Data[0]
	if first["external_id"] != "550" || first["title"] != "Fight Club" || first["poster_url"] == "" {
		t.Fatalf("first candidate fields wrong: %+v", first)
	}
	if first["provider"] != "tmdb" {
		t.Fatalf("provider should be tmdb, got %v", first["provider"])
	}
}

func TestIdentifyCandidates_NoProviderConfigured(t *testing.T) {
	// identifier=nil → 503, sin tocar nada más.
	handler := NewItemHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "", nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/items/{id}/identify/candidates", handler.IdentifyCandidates)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/x/identify/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", w.Code)
	}
}

func TestIdentifyCandidates_ItemNotFound(t *testing.T) {
	id := &fakeIdentifier{searchErr: domain.ErrNotFound}
	env := newIdentifyEnv(t, id)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/missing/identify/candidates", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", w.Code)
	}
}

func TestIdentifyCandidates_ProviderError(t *testing.T) {
	// Errores que no son NotFound se mapean a 502 — son fallos de un
	// servicio externo (TMDb caído, rate-limit) y el cliente puede
	// reintentar. Distinto código que 5xx genérico para que monitoring
	// distinga "yo me caí" de "TMDb se cayó".
	id := &fakeIdentifier{searchErr: errors.New("tmdb timeout")}
	env := newIdentifyEnv(t, id)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/item-1/identify/candidates", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d want 502", w.Code)
	}
}

func TestIdentify_AppliesMatch(t *testing.T) {
	id := &fakeIdentifier{}
	env := newIdentifyEnv(t, id)

	body := bytes.NewBufferString(`{"provider":"tmdb","external_id":"550"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/item-1/identify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", w.Code, w.Body.String())
	}
	if id.applyCalledWith.itemID != "item-1" || id.applyCalledWith.externalID != "550" {
		t.Fatalf("IdentifyAndApply called with wrong args: %+v", id.applyCalledWith)
	}
}

func TestIdentify_DefaultsProviderToTMDb(t *testing.T) {
	// El campo `provider` es opcional en el body — si falta, se asume
	// tmdb (único soportado hoy). Sin este default, el cliente tendría
	// que conocer el nombre del provider para cada item, cosa que
	// estructuralmente no aporta nada al MVP single-provider.
	id := &fakeIdentifier{}
	env := newIdentifyEnv(t, id)

	body := bytes.NewBufferString(`{"external_id":"550"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/item-1/identify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", w.Code, w.Body.String())
	}
}

func TestIdentify_RejectsUnsupportedProvider(t *testing.T) {
	id := &fakeIdentifier{}
	env := newIdentifyEnv(t, id)

	body := bytes.NewBufferString(`{"provider":"imdb","external_id":"tt0137523"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/item-1/identify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
	if id.applyCalledWith.itemID != "" {
		t.Fatalf("apply should not have been called for unsupported provider")
	}
}

func TestIdentify_RejectsMissingExternalID(t *testing.T) {
	id := &fakeIdentifier{}
	env := newIdentifyEnv(t, id)

	body := bytes.NewBufferString(`{"provider":"tmdb"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/item-1/identify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

func TestIdentify_PropagatesNotFound(t *testing.T) {
	id := &fakeIdentifier{applyErr: domain.ErrNotFound}
	env := newIdentifyEnv(t, id)

	body := bytes.NewBufferString(`{"provider":"tmdb","external_id":"550"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items/missing/identify", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", w.Code)
	}
}
