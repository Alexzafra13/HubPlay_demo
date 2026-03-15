package api_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/api"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
	"hubplay/internal/user"
)

type testApp struct {
	server *httptest.Server
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()

	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	cfg := config.TestConfig()
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}

	authSvc := auth.NewService(repos.Users, repos.Sessions, cfg.Auth, clk, slog.Default())
	userSvc := user.NewService(repos.Users, slog.Default())

	router := api.NewRouter(api.Dependencies{
		Auth:   authSvc,
		Users:  userSvc,
		Config: cfg,
		Logger: slog.Default(),
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &testApp{server: server}
}

func (a *testApp) do(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encoding request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, a.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("executing request: %v", err)
	}
	return resp
}

func (a *testApp) decode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	_ = resp.Body.Close()
	return result
}

// ─── Health ───

func TestHealth(t *testing.T) {
	app := newTestApp(t)
	resp := app.do(t, "GET", "/api/v1/health", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body := app.decode(t, resp)
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}
}

// ─── Setup ───

func TestSetup_FirstAdmin(t *testing.T) {
	app := newTestApp(t)
	resp := app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "admin12345",
	}, "")

	if resp.StatusCode != http.StatusCreated {
		body := app.decode(t, resp)
		t.Fatalf("expected 201, got %d: %v", resp.StatusCode, body)
	}

	body := app.decode(t, resp)
	data := body["data"].(map[string]any)
	if data["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", data["role"])
	}
}

func TestSetup_FailsAfterFirstUser(t *testing.T) {
	app := newTestApp(t)

	// First setup succeeds
	resp := app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "admin12345",
	}, "")
	body := app.decode(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first setup should succeed: %v", body)
	}

	// Second setup fails
	resp = app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin2",
		"password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Full Auth Flow ───

func TestAuthFlow_Setup_Login_Me_Refresh_Logout(t *testing.T) {
	app := newTestApp(t)

	// 1. Setup
	resp := app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin",
		"password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 2. Login
	resp = app.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusOK {
		body := app.decode(t, resp)
		t.Fatalf("login failed: %d: %v", resp.StatusCode, body)
	}

	loginBody := app.decode(t, resp)
	data := loginBody["data"].(map[string]any)
	accessToken := data["access_token"].(string)
	refreshToken := data["refresh_token"].(string)

	if accessToken == "" || refreshToken == "" {
		t.Fatal("tokens should not be empty")
	}

	// 3. Get /me
	resp = app.do(t, "GET", "/api/v1/me", nil, accessToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /me, got %d", resp.StatusCode)
	}

	meBody := app.decode(t, resp)
	meData := meBody["data"].(map[string]any)
	if meData["username"] != "admin" {
		t.Errorf("expected username 'admin', got %v", meData["username"])
	}
	if meData["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", meData["role"])
	}

	// 4. Refresh token
	resp = app.do(t, "POST", "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")
	if resp.StatusCode != http.StatusOK {
		body := app.decode(t, resp)
		t.Fatalf("refresh failed: %d: %v", resp.StatusCode, body)
	}

	refreshBody := app.decode(t, resp)
	refreshData := refreshBody["data"].(map[string]any)
	newAccessToken := refreshData["access_token"].(string)
	if newAccessToken == "" {
		t.Error("new access token should not be empty")
	}

	// 5. Logout
	resp = app.do(t, "POST", "/api/v1/auth/logout", map[string]string{
		"refresh_token": refreshToken,
	}, newAccessToken)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 for logout, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 6. Refresh should fail after logout
	resp = app.do(t, "POST", "/api/v1/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Auth Guard ───

func TestMe_RequiresAuth(t *testing.T) {
	app := newTestApp(t)
	resp := app.do(t, "GET", "/api/v1/me", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestMe_InvalidToken(t *testing.T) {
	app := newTestApp(t)
	resp := app.do(t, "GET", "/api/v1/me", nil, "invalid-token")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid token, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Admin Guard ───

func TestUsers_RequiresAdmin(t *testing.T) {
	app := newTestApp(t)

	// Create admin and a regular user
	resp := app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin", "password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Login as admin
	resp = app.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "admin12345",
	}, "")
	adminData := app.decode(t, resp)["data"].(map[string]any)
	adminToken := adminData["access_token"].(string)

	// Create regular user via admin
	resp = app.do(t, "POST", "/api/v1/users", map[string]string{
		"username": "regularuser", "password": "password123",
	}, adminToken)
	if resp.StatusCode != http.StatusCreated {
		body := app.decode(t, resp)
		t.Fatalf("expected 201 for user creation, got %d: %v", resp.StatusCode, body)
	}
	_ = resp.Body.Close()

	// Login as regular user
	resp = app.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "regularuser", "password": "password123",
	}, "")
	userData := app.decode(t, resp)["data"].(map[string]any)
	userToken := userData["access_token"].(string)

	// Admin can list users
	resp = app.do(t, "GET", "/api/v1/users", nil, adminToken)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin should be able to list users, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Regular user cannot list users
	resp = app.do(t, "GET", "/api/v1/users", nil, userToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("regular user should get 403, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Login Validation ───

func TestLogin_BadPassword(t *testing.T) {
	app := newTestApp(t)

	resp := app.do(t, "POST", "/api/v1/auth/setup", map[string]string{
		"username": "admin", "password": "admin12345",
	}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup failed: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = app.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wrongpassword",
	}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestLogin_MissingFields(t *testing.T) {
	app := newTestApp(t)

	resp := app.do(t, "POST", "/api/v1/auth/login", map[string]string{
		"username": "",
		"password": "",
	}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// ─── Setup Validation ───

func TestSetup_Validation(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]string
		wantCode int
	}{
		{"short username", map[string]string{"username": "ab", "password": "password123"}, http.StatusBadRequest},
		{"short password", map[string]string{"username": "admin", "password": "short"}, http.StatusBadRequest},
		{"empty fields", map[string]string{}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t) // Fresh app per subtest — no prior users
			resp := app.do(t, "POST", "/api/v1/auth/setup", tt.body, "")
			if resp.StatusCode != tt.wantCode {
				body := app.decode(t, resp)
				t.Errorf("expected %d, got %d: %v", tt.wantCode, resp.StatusCode, body)
			} else {
				_ = resp.Body.Close()
			}
		})
	}
}
