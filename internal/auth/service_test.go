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

	refreshed, err := svc.RefreshToken(context.Background(), loginToken.RefreshToken, "127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if refreshed.AccessToken == "" {
		t.Error("refreshed access token should not be empty")
	}
}

func TestService_RefreshToken_InvalidToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	_, err := svc.RefreshToken(context.Background(), "nonexistent-token", "127.0.0.1")
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

// newTestAuthServiceWithRL builds a service with a tight rate-limit policy
// so the refresh-bruteforce test does not have to drive 10 failures.
func newTestAuthServiceWithRL(t *testing.T, maxFails int) (*auth.Service, *db.UserRepository, *db.SessionRepository) {
	t.Helper()
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository(database)
	sessionRepo := db.NewSessionRepository(database)
	keyRepo := db.NewSigningKeyRepository(database)

	cfg := config.AuthConfig{
		JWTSecret:          "test-secret-32-bytes-long-enough!",
		BCryptCost:         10,
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    720 * time.Hour,
		MaxSessionsPerUser: 5,
	}
	rl := config.RateLimitConfig{
		LoginAttempts: maxFails,
		LoginWindow:   15 * time.Minute,
		LoginLockout:  5 * time.Minute,
	}
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}
	ctx := context.Background()
	if _, err := auth.Bootstrap(ctx, keyRepo, clk, cfg.JWTSecret); err != nil {
		t.Fatalf("bootstrap keystore: %v", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, keyRepo, clk)
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	svc := auth.NewService(userRepo, sessionRepo, keyStore, cfg, clk, slog.Default(), rl)
	return svc, userRepo, sessionRepo
}

// TestService_RefreshToken_RateLimited asserts that a refresh-token
// bruteforce is locked out after maxFails. Without this gate, a leaked
// or guessable refresh token can be hammered indefinitely — a parallel
// of the password-bruteforce surface that Login already protects against.
func TestService_RefreshToken_RateLimited(t *testing.T) {
	const maxFails = 3
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	registerTestUser(t, svc)

	const ip = "203.0.113.7"

	// Drive maxFails failures with garbage tokens from the same IP. Each
	// uses a different bogus token so the per-token key counter stays at
	// 1; the per-IP counter is what trips the lock.
	for i := 0; i < maxFails; i++ {
		_, err := svc.RefreshToken(context.Background(), "garbage-"+string(rune('a'+i)), ip)
		if !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatalf("attempt %d: expected ErrInvalidToken, got %v", i, err)
		}
	}

	// The next attempt — even with a valid login refresh token — must be
	// rejected because the IP is locked out.
	loginToken, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "10.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	_, err = svc.RefreshToken(context.Background(), loginToken.RefreshToken, ip)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("expected ErrForbidden after %d failures from same IP, got %v", maxFails, err)
	}

	// A different IP should still be able to refresh — the lock is per-IP,
	// not global, so legitimate users on other connections are not punished.
	if _, err := svc.RefreshToken(context.Background(), loginToken.RefreshToken, "198.51.100.1"); err != nil {
		t.Errorf("refresh from clean IP should succeed, got %v", err)
	}
}

// TestService_RefreshToken_PerTokenLockout: even if an attacker rotates
// IPs, a single refresh token cannot be hammered past maxFails.
func TestService_RefreshToken_PerTokenLockout(t *testing.T) {
	const maxFails = 3
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	registerTestUser(t, svc)

	loginToken, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "10.0.0.1")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Tamper the token so it is invalid but persists the same hash space.
	bad := loginToken.RefreshToken + "tampered"
	for i := 0; i < maxFails; i++ {
		_, err := svc.RefreshToken(context.Background(), bad, "ip-"+string(rune('a'+i)))
		if !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}

	// Same bad token from yet another fresh IP must now be locked.
	_, err = svc.RefreshToken(context.Background(), bad, "ip-fresh")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("expected per-token lockout after %d failures, got %v", maxFails, err)
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
	_, err = svc.RefreshToken(context.Background(), token.RefreshToken, "127.0.0.1")
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
