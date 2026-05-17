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
	"time"

	authmodel "hubplay/internal/auth/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/domain"
)

// ─── Mock auth service ──────────────────────────────────────────────────────

type mockAuthService struct {
	loginFn        func(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error)
	refreshTokenFn func(ctx context.Context, refreshToken, ip string) (*auth.AuthToken, error)
	logoutFn       func(ctx context.Context, refreshToken string) error
	registerFn     func(ctx context.Context, req auth.RegisterRequest) (*authmodel.User, error)
}

func (m *mockAuthService) Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*auth.AuthToken, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, username, password, deviceName, deviceID, ip)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) RefreshToken(ctx context.Context, refreshToken, ip string) (*auth.AuthToken, error) {
	if m.refreshTokenFn != nil {
		return m.refreshTokenFn(ctx, refreshToken, ip)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) Logout(ctx context.Context, refreshToken string) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx, refreshToken)
	}
	return nil
}

func (m *mockAuthService) Register(ctx context.Context, req auth.RegisterRequest) (*authmodel.User, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) ValidateToken(_ context.Context, _ string) (*auth.Claims, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) ResetPassword(_ context.Context, _ string) (string, error) {
	return "stub-password", nil
}

func (m *mockAuthService) ChangePassword(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockAuthService) ListProfiles(_ context.Context, _ string) ([]*authmodel.User, error) {
	return nil, nil
}

func (m *mockAuthService) SwitchProfile(_ context.Context, _, _, _, _, _, _ string) (*auth.AuthToken, error) {
	return nil, errors.New("not implemented")
}

func (m *mockAuthService) SetPIN(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAuthService) Middleware(next http.Handler) http.Handler {
	return next
}

func (m *mockAuthService) ListSessions(_ context.Context, _ string) ([]*authmodel.Session, error) {
	return nil, nil
}

func (m *mockAuthService) RevokeSession(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAuthService) CurrentSessionID(_ context.Context, _ string) string {
	return ""
}

// ─── Mock user service ──────────────────────────────────────────────────────

type mockUserService struct {
	getByIDFn func(ctx context.Context, id string) (*authmodel.User, error)
	listFn    func(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error)
	deleteFn  func(ctx context.Context, id string) error
	countFn   func(ctx context.Context) (int, error)
}

func (m *mockUserService) GetByID(ctx context.Context, id string) (*authmodel.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockUserService) List(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error) {
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

func (m *mockUserService) SetMaxContentRating(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockUserService) SetDisplayName(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockUserService) SetAvatarColor(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockUserService) SetRole(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockUserService) SetActive(_ context.Context, _ string, _ bool) error {
	return nil
}

func (m *mockUserService) PrimaryAdminID(_ context.Context) (string, error) {
	return "", nil
}

func (m *mockUserService) SetAccessExpiresAt(_ context.Context, _ string, _ *time.Time) error {
	return nil
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func testAuthCfg() config.AuthConfig {
	return config.AuthConfig{
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 720 * time.Hour,
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	token := &auth.AuthToken{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		UserID:       "u1",
		Role:         "admin",
	}
	user := &authmodel.User{
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
		getByIDFn: func(_ context.Context, id string) (*authmodel.User, error) {
			if id == "u1" {
				return user, nil
			}
			return nil, domain.ErrNotFound
		},
	}

	handler := NewAuthHandler(authSvc, userSvc, nil, testAuthCfg(), testLogger())

	body := `{"username":"admin","password":"password"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'data' envelope, got %v", envelope)
	}
	if data["access_token"] != "access-123" {
		t.Errorf("expected access_token 'access-123', got %v", data["access_token"])
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

	handler := NewAuthHandler(authSvc, userSvc, nil, testAuthCfg(), testLogger())

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
	handler := NewAuthHandler(&mockAuthService{}, &mockUserService{}, nil, testAuthCfg(), testLogger())

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Login(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestAuthHandler_Setup_AllowedWhenNoUsers ensures the bootstrap path
// works on a clean install: with zero users in the DB, an unauthenticated
// caller can create the first admin. This is the legitimate "first run"
// path that the React wizard hits.
func TestAuthHandler_Setup_AllowedWhenNoUsers(t *testing.T) {
	created := &authmodel.User{
		ID:          "u1",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        "admin",
		IsActive:    true,
	}
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, req auth.RegisterRequest) (*authmodel.User, error) {
			if req.Role != "admin" {
				t.Errorf("setup must register the first user as admin, got role=%q", req.Role)
			}
			return created, nil
		},
		loginFn: func(_ context.Context, _, _, _, _, _ string) (*auth.AuthToken, error) {
			return &auth.AuthToken{AccessToken: "a", RefreshToken: "r", UserID: "u1", Role: "admin"}, nil
		},
	}
	userSvc := &mockUserService{
		countFn: func(_ context.Context) (int, error) { return 0, nil },
		getByIDFn: func(_ context.Context, id string) (*authmodel.User, error) {
			if id == "u1" {
				return created, nil
			}
			return nil, domain.ErrNotFound
		},
	}

	handler := NewAuthHandler(authSvc, userSvc, nil, testAuthCfg(), testLogger())
	body := `{"username":"admin","password":"password123","display_name":"Admin"}`
	req := httptest.NewRequest("POST", "/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Setup(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestAuthHandler_Setup_BlockedWhenUsersExist is the privilege-escalation
// gate: once any user exists, the unauthenticated /auth/setup endpoint
// MUST refuse to create another admin. A single line in the handler is
// the only barrier between "fresh install" and "any anonymous visitor
// registers themselves as admin." A refactor that flips this check —
// e.g. changing `> 0` to `>= 0`, or removing it entirely while moving
// the wizard logic — would silently break authentication. This test
// pins the contract so that cannot regress.
func TestAuthHandler_Setup_BlockedWhenUsersExist(t *testing.T) {
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			t.Fatal("Register must NOT be called when users already exist")
			return nil, nil
		},
		loginFn: func(_ context.Context, _, _, _, _, _ string) (*auth.AuthToken, error) {
			t.Fatal("Login must NOT be called when setup is blocked")
			return nil, nil
		},
	}
	userSvc := &mockUserService{
		countFn: func(_ context.Context) (int, error) { return 1, nil },
	}

	handler := NewAuthHandler(authSvc, userSvc, nil, testAuthCfg(), testLogger())
	body := `{"username":"attacker","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Setup(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("setup must return 403 when users exist, got %d: %s", rr.Code, rr.Body.String())
	}

	// The response cookies must NOT include auth cookies — even on the
	// off chance the handler partially succeeded, no session must be
	// minted for an unauthenticated caller hitting a closed setup gate.
	for _, c := range rr.Result().Cookies() {
		if c.Name == "hubplay_access" || c.Name == "hubplay_refresh" {
			t.Errorf("blocked setup must not set auth cookie %q", c.Name)
		}
	}
}

// TestAuthHandler_Setup_BlockedWhenManyUsersExist guards against the
// "off-by-one" refactor mistake (changing `> 0` to `> 1`, etc.).
func TestAuthHandler_Setup_BlockedWhenManyUsersExist(t *testing.T) {
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			t.Fatal("Register must NOT be called")
			return nil, nil
		},
	}
	userSvc := &mockUserService{
		countFn: func(_ context.Context) (int, error) { return 42, nil },
	}

	handler := NewAuthHandler(authSvc, userSvc, nil, testAuthCfg(), testLogger())
	body := `{"username":"attacker","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Setup(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("setup must return 403 with many users, got %d", rr.Code)
	}
}

func TestUserHandler_Me(t *testing.T) {
	user := &authmodel.User{
		ID:          "u1",
		Username:    "admin",
		DisplayName: "Admin",
		Role:        "admin",
		IsActive:    true,
	}

	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, id string) (*authmodel.User, error) {
			if id == "u1" {
				return user, nil
			}
			return nil, domain.ErrNotFound
		},
	}

	handler := NewUserHandler(userSvc, nil, testLogger())

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
	handler := NewUserHandler(&mockUserService{}, nil, testLogger())

	req := httptest.NewRequest("GET", "/me", nil)
	rr := httptest.NewRecorder()
	handler.Me(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestUserHandler_Me_NotFound(t *testing.T) {
	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, _ string) (*authmodel.User, error) {
			return nil, domain.ErrNotFound
		},
	}

	handler := NewUserHandler(userSvc, nil, testLogger())

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

// ─── Register w/ grant_library_ids ──────────────────────────────────────────

// TestAuthHandler_Register_AppliesLibraryGrants verifies the one-shot
// "create user AND attach grants" admin flow: a single POST creates the
// row AND tx-applies the requested grants. The grants must hit
// ReplaceAccess with the new user's id and the deduplicated set; the
// library service must be probed for existence before user creation so
// a typo doesn't strand a half-created account.
func TestAuthHandler_Register_AppliesLibraryGrants(t *testing.T) {
	createdUser := &authmodel.User{ID: "new-u", Username: "alice", DisplayName: "Alice", Role: "user"}
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			return createdUser, nil
		},
	}
	libSvc := &libFakeService{
		getByIDFn: func(_ context.Context, _ string) (*librarymodel.Library, error) {
			return &librarymodel.Library{}, nil
		},
	}
	handler := NewAuthHandler(authSvc, &mockUserService{}, libSvc, testAuthCfg(), testLogger())

	body := `{"username":"alice","password":"password123","grant_library_ids":["lib-a","lib-b","lib-a"]}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(libSvc.replaceAccessCalls) != 1 {
		t.Fatalf("expected 1 ReplaceAccess call, got %d", len(libSvc.replaceAccessCalls))
	}
	call := libSvc.replaceAccessCalls[0]
	if call.UserID != "new-u" {
		t.Errorf("expected ReplaceAccess against new user id, got %q", call.UserID)
	}
	if len(call.LibraryIDs) != 2 {
		t.Errorf("expected dedup to 2 ids, got %v", call.LibraryIDs)
	}
}

// Profile creation MUST reject grant_library_ids — grants always target
// the parent (ADR-014). Anything else would create orphan rows the
// predicate never consults.
func TestAuthHandler_Register_GrantsOnProfile_400(t *testing.T) {
	libSvc := &libFakeService{}
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			t.Fatal("Register must NOT be called when validation fails")
			return nil, nil
		},
	}
	userSvc := &mockUserService{
		getByIDFn: func(_ context.Context, _ string) (*authmodel.User, error) {
			return &authmodel.User{ID: "parent-1", Username: "parent"}, nil
		},
	}
	handler := NewAuthHandler(authSvc, userSvc, libSvc, testAuthCfg(), testLogger())

	body := `{"parent_user_id":"parent-1","display_name":"Kid","grant_library_ids":["lib-a"]}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(libSvc.replaceAccessCalls) != 0 {
		t.Error("ReplaceAccess must not run on profile create with grants")
	}
}

// Unknown library_id during validation MUST short-circuit before the
// user row is created. The mock Register fatals if invoked.
func TestAuthHandler_Register_GrantsUnknownLibrary_404(t *testing.T) {
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			t.Fatal("Register must NOT be called when library validation fails")
			return nil, nil
		},
	}
	libSvc := &libFakeService{
		getByIDFn: func(_ context.Context, id string) (*librarymodel.Library, error) {
			if id == "lib-good" {
				return &librarymodel.Library{ID: id}, nil
			}
			return nil, domain.NewNotFound("library")
		},
	}
	handler := NewAuthHandler(authSvc, &mockUserService{}, libSvc, testAuthCfg(), testLogger())

	body := `{"username":"alice","password":"password123","grant_library_ids":["lib-good","lib-missing"]}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(libSvc.replaceAccessCalls) != 0 {
		t.Error("ReplaceAccess must not run when validation fails")
	}
}

// Register without grant_library_ids must work even when libraries is
// nil (test setups don't always wire the library service). The grants
// branch is purely opt-in.
func TestAuthHandler_Register_NoGrants_NilLibraries_OK(t *testing.T) {
	created := &authmodel.User{ID: "u1", Username: "alice", DisplayName: "Alice", Role: "user"}
	authSvc := &mockAuthService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (*authmodel.User, error) {
			return created, nil
		},
	}
	handler := NewAuthHandler(authSvc, &mockUserService{}, nil, testAuthCfg(), testLogger())

	body := `{"username":"alice","password":"password123"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
}
