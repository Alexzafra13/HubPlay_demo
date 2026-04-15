package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func newTestAuthService(t *testing.T) (*auth.Service, *db.UserRepository, *db.SessionRepository) {
	t.Helper()
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository(database)
	sessionRepo := db.NewSessionRepository(database)
	keyRepo := db.NewSigningKeyRepository(database)

	cfg := config.AuthConfig{
		JWTSecret:          "test-secret-32-bytes-long-enough!",
		BCryptCost:         10, // Low for fast tests
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    720 * time.Hour,
		MaxSessionsPerUser: 5,
	}

	clk := &clock.Mock{CurrentTime: time.Now().UTC()}

	// Seed the keystore the same way main.go does: bootstrap from the config
	// secret, then load. This keeps the test path identical to production
	// and catches wiring bugs between Bootstrap and NewKeyStore.
	ctx := context.Background()
	if _, err := auth.Bootstrap(ctx, keyRepo, clk, cfg.JWTSecret); err != nil {
		t.Fatalf("bootstrap keystore: %v", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, keyRepo, clk)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}

	svc := auth.NewService(userRepo, sessionRepo, keyStore, cfg, clk, slog.Default())
	return svc, userRepo, sessionRepo
}

func registerTestUser(t *testing.T, svc *auth.Service) *db.User {
	t.Helper()
	u, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username:    "testuser",
		DisplayName: "Test User",
		Password:    "password123",
		Role:        "user",
	})
	if err != nil {
		t.Fatalf("registering test user: %v", err)
	}
	return u
}

func TestService_Register(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	u, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username:    "alex",
		DisplayName: "Alex",
		Password:    "securepassword",
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if u.Username != "alex" {
		t.Errorf("expected username 'alex', got %q", u.Username)
	}
	if u.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", u.Role)
	}
	if u.ID == "" {
		t.Error("user ID should be generated")
	}
}

func TestService_Register_DefaultRole(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	u, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username: "bob",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Role != "user" {
		t.Errorf("expected default role 'user', got %q", u.Role)
	}
}

func TestService_Login_Success(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	token, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token.AccessToken == "" {
		t.Error("access token should not be empty")
	}
	if token.RefreshToken == "" {
		t.Error("refresh token should not be empty")
	}
	if token.UserID == "" {
		t.Error("user ID should be set")
	}
}

func TestService_Login_WrongPassword(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	_, err := svc.Login(context.Background(), "testuser", "wrongpassword", "Chrome", "dev-1", "127.0.0.1")
	if !errors.Is(err, domain.ErrInvalidPassword) {
		t.Errorf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestService_Login_NonexistentUser(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	_, err := svc.Login(context.Background(), "nobody", "password123", "Chrome", "dev-1", "127.0.0.1")
	if !errors.Is(err, domain.ErrInvalidPassword) {
		t.Errorf("expected ErrInvalidPassword (not ErrNotFound to avoid user enumeration), got %v", err)
	}
}

func TestService_Login_DisabledAccount(t *testing.T) {
	svc, userRepo, _ := newTestAuthService(t)
	u := registerTestUser(t, svc)

	// Disable the account
	u.IsActive = false
	if err := userRepo.Update(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if !errors.Is(err, domain.ErrAccountDisabled) {
		t.Errorf("expected ErrAccountDisabled, got %v", err)
	}
}

func TestService_RefreshToken_Success(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	loginToken, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	refreshed, err := svc.RefreshToken(context.Background(), loginToken.RefreshToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if refreshed.AccessToken == "" {
		t.Error("refreshed access token should not be empty")
	}
}

func TestService_RefreshToken_InvalidToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	_, err := svc.RefreshToken(context.Background(), "nonexistent-token")
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestService_Logout(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	token, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	if err := svc.Logout(context.Background(), token.RefreshToken); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Refresh should fail after logout
	_, err = svc.RefreshToken(context.Background(), token.RefreshToken)
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken after logout, got %v", err)
	}
}

func TestService_ValidateToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	token, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	claims, err := svc.ValidateToken(context.Background(), token.AccessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", claims.Username)
	}
}

func TestService_ValidateToken_Invalid(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	_, err := svc.ValidateToken(context.Background(), "garbage-token")
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}
