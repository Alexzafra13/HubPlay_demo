package users

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/domain"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"
)

// libraryAccessEnv mounts the same chi sub-tree the production router
// builds so the path params (URL `id`) get resolved exactly like in
// prod. Re-implementing the routes in-line would let a routing change
// pass tests silently — we want the real wiring.
type libraryAccessEnv struct {
	t       *testing.T
	users   *userFakeService
	libs    *libFakeService
	router  chi.Router
	handler *UserHandler
}

func newLibraryAccessEnv(t *testing.T) *libraryAccessEnv {
	t.Helper()
	env := &libraryAccessEnv{
		t:     t,
		users: &userFakeService{},
		libs:  &libFakeService{},
	}
	env.handler = NewUserHandler(env.users, env.libs, nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Route("/api/v1/users", func(r chi.Router) {
		r.Get("/{id}/library-access", env.handler.GetLibraryAccess)
		r.Put("/{id}/library-access", env.handler.SetLibraryAccess)
		r.Post("/{id}/iptv-libraries", env.handler.CreatePersonalIPTV)
	})
	env.router = r
	return env
}

func (e *libraryAccessEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewBuffer(raw)
	} else {
		reader = bytes.NewBufferString("")
	}
	req := httptest.NewRequest(method, path, reader)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// ─── GetLibraryAccess ───────────────────────────────────────────────────────

func TestUserHandler_GetLibraryAccess_HappyPath(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.listAccessFn = func(_ context.Context, userID string) ([]string, error) {
		if userID != "u-1" {
			t.Errorf("expected lookup against top-level user, got %q", userID)
		}
		return []string{"lib-a", "lib-b"}, nil
	}

	rr := env.do(http.MethodGet, "/api/v1/users/u-1/library-access", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			UserID      string   `json:"user_id"`
			OwnerID     string   `json:"owner_id"`
			LibraryIDs  []string `json:"library_ids"`
			IsInherited bool     `json:"is_inherited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.UserID != "u-1" || resp.Data.OwnerID != "u-1" || resp.Data.IsInherited {
		t.Errorf("payload: %+v", resp.Data)
	}
	if len(resp.Data.LibraryIDs) != 2 {
		t.Errorf("expected 2 library_ids, got %v", resp.Data.LibraryIDs)
	}
}

// Profile ids must transparently look up the parent's grants so the
// admin UI can render the inherited set in one round-trip.
func TestUserHandler_GetLibraryAccess_NormalisesProfileToParent(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		if id != "u-profile" {
			t.Errorf("unexpected user lookup: %q", id)
		}
		return &authmodel.User{ID: id, ParentUserID: "u-parent"}, nil
	}
	env.libs.listAccessFn = func(_ context.Context, userID string) ([]string, error) {
		if userID != "u-parent" {
			t.Errorf("listAccess must hit the parent id, got %q", userID)
		}
		return []string{"lib-tv"}, nil
	}

	rr := env.do(http.MethodGet, "/api/v1/users/u-profile/library-access", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data struct {
			UserID      string `json:"user_id"`
			OwnerID     string `json:"owner_id"`
			IsInherited bool   `json:"is_inherited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.UserID != "u-profile" || resp.Data.OwnerID != "u-parent" || !resp.Data.IsInherited {
		t.Errorf("expected is_inherited=true with owner=parent, got %+v", resp.Data)
	}
}

func TestUserHandler_GetLibraryAccess_UserNotFound_404(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, _ string) (*authmodel.User, error) {
		return nil, domain.NewNotFound("user")
	}
	rr := env.do(http.MethodGet, "/api/v1/users/u-ghost/library-access", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

// When the library service is nil we want a clear 503 not a panic, so
// future test setups that forget to pass libraries don't crash the
// process.
func TestUserHandler_GetLibraryAccess_NoLibrariesWired_503(t *testing.T) {
	t.Parallel()
	handler := NewUserHandler(&userFakeService{}, nil, nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Get("/api/v1/users/{id}/library-access", handler.GetLibraryAccess)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/u-1/library-access", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

// ─── SetLibraryAccess ───────────────────────────────────────────────────────

func TestUserHandler_SetLibraryAccess_HappyPath(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.getByIDFn = func(_ context.Context, _ string) (*librarymodel.Library, error) {
		return &librarymodel.Library{}, nil
	}

	rr := env.do(http.MethodPut, "/api/v1/users/u-1/library-access", map[string]any{
		"library_ids": []string{"lib-a", "lib-b"},
	})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 1 {
		t.Fatalf("expected 1 ReplaceAccess call, got %d", len(env.libs.replaceAccessCalls))
	}
	call := env.libs.replaceAccessCalls[0]
	if call.UserID != "u-1" {
		t.Errorf("expected UserID=u-1, got %q", call.UserID)
	}
	if len(call.LibraryIDs) != 2 {
		t.Errorf("expected 2 ids, got %v", call.LibraryIDs)
	}
}

func TestUserHandler_SetLibraryAccess_RejectsProfile_400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id, ParentUserID: "u-parent"}, nil
	}
	rr := env.do(http.MethodPut, "/api/v1/users/u-profile/library-access", map[string]any{
		"library_ids": []string{"lib-a"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 0 {
		t.Errorf("ReplaceAccess must not be called for profile targets")
	}
}

func TestUserHandler_SetLibraryAccess_UnknownLibrary_404(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.getByIDFn = func(_ context.Context, id string) (*librarymodel.Library, error) {
		if id == "lib-good" {
			return &librarymodel.Library{ID: id}, nil
		}
		return nil, domain.NewNotFound("library")
	}
	rr := env.do(http.MethodPut, "/api/v1/users/u-1/library-access", map[string]any{
		"library_ids": []string{"lib-good", "lib-missing"},
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 0 {
		t.Error("ReplaceAccess must not run when validation fails partway through")
	}
}

func TestUserHandler_SetLibraryAccess_EmptyClearsAll(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	rr := env.do(http.MethodPut, "/api/v1/users/u-1/library-access", map[string]any{
		"library_ids": []string{},
	})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 1 {
		t.Fatalf("expected 1 ReplaceAccess call (empty set), got %d", len(env.libs.replaceAccessCalls))
	}
	if len(env.libs.replaceAccessCalls[0].LibraryIDs) != 0 {
		t.Errorf("expected empty desired set, got %v", env.libs.replaceAccessCalls[0].LibraryIDs)
	}
}

func TestUserHandler_SetLibraryAccess_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/u-1/library-access",
		bytes.NewBufferString("{not json"))
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserHandler_SetLibraryAccess_DeduplicatesIDs(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.getByIDFn = func(_ context.Context, _ string) (*librarymodel.Library, error) {
		return &librarymodel.Library{}, nil
	}
	rr := env.do(http.MethodPut, "/api/v1/users/u-1/library-access", map[string]any{
		"library_ids": []string{"lib-a", "lib-a", "lib-b"},
	})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(env.libs.replaceAccessCalls))
	}
	if len(env.libs.replaceAccessCalls[0].LibraryIDs) != 2 {
		t.Errorf("expected dedup to 2 ids, got %v", env.libs.replaceAccessCalls[0].LibraryIDs)
	}
}

func TestUserHandler_SetLibraryAccess_EmptyValue_400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.getByIDFn = func(_ context.Context, _ string) (*librarymodel.Library, error) {
		return &librarymodel.Library{}, nil
	}
	rr := env.do(http.MethodPut, "/api/v1/users/u-1/library-access", map[string]any{
		"library_ids": []string{"lib-a", ""},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.replaceAccessCalls) != 0 {
		t.Error("ReplaceAccess must not run for invalid payload")
	}
}

// ─── CreatePersonalIPTV ─────────────────────────────────────────────────────

func TestUserHandler_CreatePersonalIPTV_HappyPath(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.createPersonalIPTVFn = func(_ context.Context, ownerID string, req library.CreateRequest) (*librarymodel.Library, error) {
		if ownerID != "u-1" {
			t.Errorf("expected owner=u-1, got %q", ownerID)
		}
		if req.Name != "Lista de Juan" || req.M3UURL != "https://example.com/juan.m3u" {
			t.Errorf("forwarded req mismatch: %+v", req)
		}
		return &librarymodel.Library{
			ID: "lib-new", Name: req.Name, ContentType: "livetv",
			M3UURL: req.M3UURL, EPGURL: req.EPGURL, TLSInsecure: req.TLSInsecure,
		}, nil
	}

	rr := env.do(http.MethodPost, "/api/v1/users/u-1/iptv-libraries", map[string]any{
		"name":         "Lista de Juan",
		"m3u_url":      "https://example.com/juan.m3u",
		"epg_url":      "https://example.com/juan.xml",
		"tls_insecure": true,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.createPersonalIPTVCalls) != 1 {
		t.Fatalf("expected 1 CreatePersonalIPTV call, got %d", len(env.libs.createPersonalIPTVCalls))
	}
	var resp struct {
		Data struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			ContentType string `json:"content_type"`
			M3UURL      string `json:"m3u_url"`
			OwnerUserID string `json:"owner_user_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != "lib-new" || resp.Data.ContentType != "livetv" || resp.Data.OwnerUserID != "u-1" {
		t.Errorf("response payload: %+v", resp.Data)
	}
}

func TestUserHandler_CreatePersonalIPTV_RejectsProfile_400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id, ParentUserID: "u-parent"}, nil
	}
	rr := env.do(http.MethodPost, "/api/v1/users/u-profile/iptv-libraries", map[string]any{
		"name":    "Lista",
		"m3u_url": "https://example.com/p.m3u",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.libs.createPersonalIPTVCalls) != 0 {
		t.Error("CreatePersonalIPTV must not be called for profile targets")
	}
}

func TestUserHandler_CreatePersonalIPTV_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/u-1/iptv-libraries",
		bytes.NewBufferString("{not json"))
	rr := httptest.NewRecorder()
	env.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserHandler_CreatePersonalIPTV_ValidationError_PropagatesAs400(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.createPersonalIPTVFn = func(_ context.Context, _ string, _ library.CreateRequest) (*librarymodel.Library, error) {
		return nil, domain.NewValidation(map[string]string{"m3u_url": "required"})
	}

	rr := env.do(http.MethodPost, "/api/v1/users/u-1/iptv-libraries", map[string]any{
		"name": "Lista sin URL",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserHandler_CreatePersonalIPTV_UserNotFound_404(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, _ string) (*authmodel.User, error) {
		return nil, domain.NewNotFound("user")
	}
	rr := env.do(http.MethodPost, "/api/v1/users/u-ghost/iptv-libraries", map[string]any{
		"name":    "Lista",
		"m3u_url": "https://example.com/x.m3u",
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserHandler_CreatePersonalIPTV_NoLibrariesWired_503(t *testing.T) {
	t.Parallel()
	handler := NewUserHandler(&userFakeService{}, nil, nil, testutil.NopLogger())
	r := chi.NewRouter()
	r.Post("/api/v1/users/{id}/iptv-libraries", handler.CreatePersonalIPTV)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/u-1/iptv-libraries",
		bytes.NewBufferString(`{"name":"x","m3u_url":"https://x"}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}

// Service errors that don't match a known domain kind should surface
// as 500, not panic or 200. Keeps the contract for the frontend hook
// honest.
func TestUserHandler_CreatePersonalIPTV_UnknownError_500(t *testing.T) {
	t.Parallel()
	env := newLibraryAccessEnv(t)
	env.users.getByIDFn = func(_ context.Context, id string) (*authmodel.User, error) {
		return &authmodel.User{ID: id}, nil
	}
	env.libs.createPersonalIPTVFn = func(_ context.Context, _ string, _ library.CreateRequest) (*librarymodel.Library, error) {
		return nil, errors.New("boom")
	}
	rr := env.do(http.MethodPost, "/api/v1/users/u-1/iptv-libraries", map[string]any{
		"name":    "Lista",
		"m3u_url": "https://example.com/x.m3u",
	})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}
