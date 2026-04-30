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

func newTestDeviceCodeService(t *testing.T) (*auth.DeviceCodeService, *auth.Service, *db.UserRepository, *clock.Mock) {
	t.Helper()
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository(database)
	sessionRepo := db.NewSessionRepository(database)
	keyRepo := db.NewSigningKeyRepository(database)
	codeRepo := db.NewDeviceCodeRepository(database)

	cfg := config.AuthConfig{
		JWTSecret:          "test-secret-32-bytes-long-enough!",
		BCryptCost:         10,
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    720 * time.Hour,
		MaxSessionsPerUser: 5,
	}
	clk := &clock.Mock{CurrentTime: time.Now().UTC()}

	ctx := context.Background()
	if _, err := auth.Bootstrap(ctx, keyRepo, clk, cfg.JWTSecret); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, keyRepo, clk)
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}

	authSvc := auth.NewService(userRepo, sessionRepo, keyStore, cfg, clk, slog.Default())
	devSvc := auth.NewDeviceCodeService(authSvc, codeRepo, userRepo, slog.Default())
	return devSvc, authSvc, userRepo, clk
}

func registerDeviceUser(t *testing.T, svc *auth.Service) *db.User {
	t.Helper()
	u, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username:    "operator",
		DisplayName: "Operator",
		Password:    "password123",
		Role:        "user",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return u
}

func TestDeviceCode_HappyPath(t *testing.T) {
	dev, authSvc, _, clk := newTestDeviceCodeService(t)
	ctx := context.Background()
	user := registerDeviceUser(t, authSvc)

	// Step 1: device starts a flow.
	pair, err := dev.StartDevice(ctx, "Living-room TV", "https://example.com/link")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if pair.DeviceCode == "" || pair.UserCode == "" {
		t.Fatal("start should return both codes")
	}
	if pair.ExpiresIn != auth.DeviceCodeTTL {
		t.Errorf("expires_in mismatch: got %v want %v", pair.ExpiresIn, auth.DeviceCodeTTL)
	}

	// Step 2: poll BEFORE approval — must return authorization_pending.
	if _, err := dev.PollDevice(ctx, pair.DeviceCode, "127.0.0.1"); !errors.Is(err, auth.ErrAuthorizationPending) {
		t.Fatalf("pre-approval poll: got %v, want ErrAuthorizationPending", err)
	}

	// Step 3: operator approves on /link.
	if err := dev.ApproveDevice(ctx, pair.UserCode, user.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Step 4: advance past minPollGap so the next poll isn't slow_down,
	// then poll — issues tokens.
	clk.Advance(5 * time.Second)
	tok, err := dev.PollDevice(ctx, pair.DeviceCode, "127.0.0.1")
	if err != nil {
		t.Fatalf("post-approval poll: %v", err)
	}
	if tok.UserID != user.ID {
		t.Errorf("token user_id: got %q want %q", tok.UserID, user.ID)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Error("token shape: access + refresh both required")
	}

	// Step 5: poll AGAIN — code is consumed, must return expired_token.
	clk.Advance(5 * time.Second)
	if _, err := dev.PollDevice(ctx, pair.DeviceCode, "127.0.0.1"); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("post-consume poll: got %v, want ErrTokenExpired", err)
	}
}

func TestDeviceCode_SlowDown(t *testing.T) {
	dev, _, _, _ := newTestDeviceCodeService(t)
	ctx := context.Background()

	pair, err := dev.StartDevice(ctx, "TV", "https://example.com/link")
	if err != nil {
		t.Fatal(err)
	}

	// First poll — pending (last_polled_at gets set).
	if _, err := dev.PollDevice(ctx, pair.DeviceCode, "127.0.0.1"); !errors.Is(err, auth.ErrAuthorizationPending) {
		t.Fatalf("first poll: %v", err)
	}

	// Immediate second poll — should hit slow_down (gap < minPollGap).
	if _, err := dev.PollDevice(ctx, pair.DeviceCode, "127.0.0.1"); !errors.Is(err, auth.ErrSlowDown) {
		t.Fatalf("rapid second poll: got %v, want ErrSlowDown", err)
	}
}

func TestDeviceCode_UnknownUserCodeOnApprove(t *testing.T) {
	dev, authSvc, _, _ := newTestDeviceCodeService(t)
	user := registerDeviceUser(t, authSvc)

	if err := dev.ApproveDevice(context.Background(), "NEVERSEEN", user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("approve unknown user_code: got %v, want ErrNotFound", err)
	}
}

func TestDeviceCode_UnknownDeviceCodeOnPoll(t *testing.T) {
	dev, _, _, _ := newTestDeviceCodeService(t)

	if _, err := dev.PollDevice(context.Background(), "deadbeef", "127.0.0.1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("poll unknown device_code: got %v, want ErrNotFound", err)
	}
}

func TestDeviceCode_UserCodeNormalisation(t *testing.T) {
	dev, authSvc, _, _ := newTestDeviceCodeService(t)
	ctx := context.Background()
	user := registerDeviceUser(t, authSvc)

	pair, err := dev.StartDevice(ctx, "TV", "https://example.com/link")
	if err != nil {
		t.Fatal(err)
	}

	// Approve with the dash-formatted version + extra whitespace + lowercase.
	display := auth.FormatUserCodeDisplay(pair.UserCode)
	messy := "  " + toLower(display) + "  "
	if err := dev.ApproveDevice(ctx, messy, user.ID); err != nil {
		t.Fatalf("approve normalisation: %v", err)
	}
}

// toLower is a tiny inline helper to keep the test self-contained
// without dragging in strings just for one site.
func toLower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}
