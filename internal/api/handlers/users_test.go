package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

// ─── Fake UserService ───────────────────────────────────────────────────────

type userFakeService struct {
	getByIDFn func(ctx context.Context, id string) (*db.User, error)
	listFn    func(ctx context.Context, limit, offset int) ([]*db.User, int, error)
	deleteFn  func(ctx context.Context, id string) error
	countFn   func(ctx context.Context) (int, error)
}

func (s *userFakeService) GetByID(ctx context.Context, id string) (*db.User, error) {
	if s.getByIDFn != nil {
		return s.getByIDFn(ctx, id)
	}
	return nil, domain.NewNotFound("user")
}
func (s *userFakeService) List(ctx context.Context, limit, offset int) ([]*db.User, int, error) {
	if s.listFn != nil {
		return s.listFn(ctx, limit, offset)
	}
	return nil, 0, nil
}
func (s *userFakeService) Delete(ctx context.Context, id string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, id)
	}
	return nil
}
func (s *userFakeService) Count(ctx context.Context) (int, error) {
	if s.countFn != nil {
		return s.countFn(ctx)
	}
	return 0, nil
}

var _ UserService = (*userFakeService)(nil)

// ─── Env ────────────────────────────────────────────────────────────────────

type userTestEnv struct {
	t       *testing.T
	svc     *userFakeService
	handler *UserHandler
	router  chi.Router
}

func newUserTestEnv(t *testing.T) *userTestEnv {
	t.Helper()
	env := &userTestEnv{t: t, svc: &userFakeService{}}
	env.handler = NewUserHandler(env.svc, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/me", env.handler.Me)
		r.Get("/users", env.handler.List)
		r.Delete("/users/{id}", env.handler.Delete)
	})
	env.router = r
	return env
}

func (e *userTestEnv) do(method, path string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// ─── Me ─────────────────────────────────────────────────────────────────────

func TestUserHandler_Me_Unauthenticated_401(t *testing.T) {
	env := newUserTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/me", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestUserHandler_Me_HappyPath(t *testing.T) {
	env := newUserTestEnv(t)
	env.svc.getByIDFn = func(_ context.Context, id string) (*db.User, error) {
		return &db.User{ID: id, Username: "alice", DisplayName: "Alice", Role: "admin", IsActive: true}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/me", &auth.Claims{UserID: "u-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	data, _ := out["data"].(map[string]any)
	if data["username"] != "alice" || data["role"] != "admin" {
		t.Errorf("shape: %v", data)
	}
}

func TestUserHandler_Me_ServiceError_404(t *testing.T) {
	env := newUserTestEnv(t)
	// Default getByIDFn returns NewNotFound → 404.
	rr := env.do(http.MethodGet, "/api/v1/me", &auth.Claims{UserID: "ghost"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── List ───────────────────────────────────────────────────────────────────

func TestUserHandler_List_Empty(t *testing.T) {
	env := newUserTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/users", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	data, _ := out["data"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty, got %d", len(data))
	}
}

func TestUserHandler_List_RespectsPagination(t *testing.T) {
	env := newUserTestEnv(t)
	var gotLimit, gotOffset int
	env.svc.listFn = func(_ context.Context, limit, offset int) ([]*db.User, int, error) {
		gotLimit, gotOffset = limit, offset
		return []*db.User{{ID: "u-1", Username: "alice"}}, 25, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/users?limit=10&offset=5", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotLimit != 10 || gotOffset != 5 {
		t.Errorf("pagination: limit=%d offset=%d", gotLimit, gotOffset)
	}
	var out map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&out)
	if out["total"] != float64(25) {
		t.Errorf("total: %v", out["total"])
	}
}

func TestUserHandler_List_ServiceError_500(t *testing.T) {
	env := newUserTestEnv(t)
	env.svc.listFn = func(_ context.Context, _, _ int) ([]*db.User, int, error) {
		return nil, 0, errors.New("db down")
	}
	rr := env.do(http.MethodGet, "/api/v1/users", nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── Delete ─────────────────────────────────────────────────────────────────

func TestUserHandler_Delete_HappyPath(t *testing.T) {
	env := newUserTestEnv(t)
	var gotID string
	env.svc.deleteFn = func(_ context.Context, id string) error { gotID = id; return nil }
	rr := env.do(http.MethodDelete, "/api/v1/users/u-1", &auth.Claims{UserID: "admin-1"})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotID != "u-1" {
		t.Errorf("id passed: %q", gotID)
	}
}

func TestUserHandler_Delete_SelfDeletion_400(t *testing.T) {
	env := newUserTestEnv(t)
	// Caller's UserID matches the path param → rejected.
	rr := env.do(http.MethodDelete, "/api/v1/users/u-self", &auth.Claims{UserID: "u-self"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestUserHandler_Delete_NotFound_Mapped(t *testing.T) {
	env := newUserTestEnv(t)
	env.svc.deleteFn = func(_ context.Context, _ string) error { return domain.NewNotFound("user") }
	rr := env.do(http.MethodDelete, "/api/v1/users/ghost", &auth.Claims{UserID: "admin"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rr.Code)
	}
}
