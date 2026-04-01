package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

// ─── Mock auth service ──────────────────────────────────────────────────────

type mockAuthService struct {
	loginFn        func(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	refreshTokenFn func(ctx context.Context, refreshToken string) (*auth.AuthToken, error)
	logoutFn       func(ctx context.Context, refreshToken string) error
	registerFn     func(ctx context.Context, req auth.RegisterRequest) (*db.User, error)
}

func (m *mockAuthService) Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, username, password, deviceName, deviceID, ip)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) RefreshToken(ctx context.Context, refreshToken string) (*auth.AuthToken, error) {
	if m.refreshTokenFn != nil {
		return m.refreshTokenFn(ctx, refreshToken)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) Logout(ctx context.Context, refreshToken string) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx, refreshToken)
	}
	return nil
}

func (m *mockAuthService) Register(ctx context.Context, req auth.RegisterRequest) (*db.User, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) ValidateToken(_ context.Context, _ string) (*auth.Claims, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) Middleware(next http.Handler) http.Handler {
	return next
}

// ─── Mock user service ──────────────────────────────────────────────────────

type mockUserService struct {
	getByIDFn func(ctx context.Context, id string) (*db.User, error)
	listFn    func(ctx context.Context, limit, offset int) ([]*db.User, int, error)
	deleteFn  func(ctx context.Context, id string) error
	countFn   func(ctx context.Context) (int, error)
}

func (m *mockUserService) GetByID(ctx context.Context, id string) (*db.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockUserService) List(ctx context.Context, limit, offset int) ([]*db.User, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, limit, offset)
	}
	return nil, 0, errors.New("not implemented")
}

func (m *mockUserService) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockUserService) Count(ctx context.Context) (int, error) {
	if m.countFn != nil {
		return m.countFn(ctx)
	}
	return 0, nil
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func testAuthCfg() config.AuthConfig {
	return config.AuthConfig{
		AccessTokenTTL:  900,
		RefreshTokenTTL: 604800,
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	token := &auth.AuthToken{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		UserID:       "u1",
		Role:         "admin",
	}
	user := &db.User{
		ID:          "u1",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        "admin",
	}

	authSvc := &mockAuthService{
		loginFn: func(_ context.Context, username, password, _, _, _ string) (*auth.AuthToken, error) {
			if username == "admin" && password == "password" {
				return token, nil
			}
			return nil, domain.ErrUnauthorized
		},
	}
	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, id string) (*db.User, error) {
			if id == "u1" {
				return user, nil
			}
			return nil, domain.ErrNotFound
		},
	}

	handler := NewAuthHandler(authSvc, userSvc, testAuthCfg(), testLogger())

	body := `{"username":"admin","password":"password"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["access_token"] != "access-123" {
		t.Errorf("expected access_token 'access-123', got %v", resp["access_token"])
	}

	// Verify auth cookies are set
	cookies := rr.Result().Cookies()
	var foundAccess, foundRefresh bool
	for _, c := range cookies {
		if c.Name == "hubplay_access" {
			foundAccess = true
			if !c.HttpOnly {
				t.Error("access cookie must be HttpOnly")
			}
		}
		if c.Name == "hubplay_refresh" {
			foundRefresh = true
			if !c.HttpOnly {
				t.Error("refresh cookie must be HttpOnly")
			}
		}
	}
	if !foundAccess {
		t.Error("expected hubplay_access cookie")
	}
	if !foundRefresh {
		t.Error("expected hubplay_refresh cookie")
	}
}

func TestAuthHandler_Login_InvalidCredentials(t *testing.T) {
	authSvc := &mockAuthService{
		loginFn: func(_ context.Context, _, _, _, _, _ string) (*auth.AuthToken, error) {
			return nil, domain.ErrUnauthorized
		},
	}
	userSvc := &mockUserService{}

	handler := NewAuthHandler(authSvc, userSvc, testAuthCfg(), testLogger())

	body := `{"username":"bad","password":"wrong"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthHandler_Login_InvalidBody(t *testing.T) {
	handler := NewAuthHandler(&mockAuthService{}, &mockUserService{}, testAuthCfg(), testLogger())

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUserHandler_Me(t *testing.T) {
	user := &db.User{
		ID:          "u1",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        "admin",
		IsActive:    true,
	}

	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, id string) (*db.User, error) {
			if id == "u1" {
				return user, nil
			}
			return nil, domain.ErrNotFound
		},
	}

	handler := NewUserHandler(userSvc, testLogger())

	req := httptest.NewRequest("GET", "/me", nil)
	// Inject claims into context
	claims := &auth.Claims{UserID: "u1", Role: "admin"}
	ctx := auth.WithClaims(req.Context(), claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.Me(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data := resp["data"].(map[string]any)
	if data["username"] != "admin" {
		t.Errorf("expected username 'admin', got %v", data["username"])
	}
}

func TestUserHandler_Me_Unauthenticated(t *testing.T) {
	handler := NewUserHandler(&mockUserService{}, testLogger())

	req := httptest.NewRequest("GET", "/me", nil)
	rr := httptest.NewRecorder()
	handler.Me(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestUserHandler_Me_NotFound(t *testing.T) {
	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, _ string) (*db.User, error) {
			return nil, domain.ErrNotFound
		},
	}

	handler := NewUserHandler(userSvc, testLogger())

	req := httptest.NewRequest("GET", "/me", nil)
	claims := &auth.Claims{UserID: "nonexistent", Role: "user"}
	ctx := auth.WithClaims(req.Context(), claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.Me(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}
