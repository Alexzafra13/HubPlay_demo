package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
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

// TestService_RefreshToken_RotatesSecret pins the rotation half of
// the refresh-token lifecycle: a successful refresh MUST mint a
// fresh secret and the rotated token MUST itself be rotatable.
// (The reuse-detection half is covered separately below.)
func TestService_RefreshToken_RotatesSecret(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)
	ctx := context.Background()

	loginToken, err := svc.Login(ctx, "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	refreshed, err := svc.RefreshToken(ctx, loginToken.RefreshToken, "127.0.0.1")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	if refreshed.RefreshToken == "" {
		t.Fatal("rotated refresh token must not be empty")
	}
	if refreshed.RefreshToken == loginToken.RefreshToken {
		t.Fatal("refresh token did not rotate — leak window is back to RefreshTokenTTL")
	}

	// New token must itself be rotatable — rotation is stateful, not
	// a one-shot. This is the well-behaved-client path: client always
	// uses the most recent token and never replays the old one.
	twiceRefreshed, err := svc.RefreshToken(ctx, refreshed.RefreshToken, "127.0.0.1")
	if err != nil {
		t.Fatalf("second refresh with new token: %v", err)
	}
	if twiceRefreshed.RefreshToken == refreshed.RefreshToken {
		t.Error("second refresh did not rotate again")
	}
}

// TestService_RefreshToken_ReuseDetection_RevokesSession pins the
// security-critical invariant: a refresh token that's already been
// rotated past (i.e. matches previous_refresh_token_hash, not
// refresh_token_hash) signals reuse. The safe response is to nuke
// the entire session row so neither the attacker nor the legitimate
// client can keep refreshing — both must come back through /login,
// which is rate-limited the same way.
func TestService_RefreshToken_ReuseDetection_RevokesSession(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	registerTestUser(t, svc)
	ctx := context.Background()

	loginToken, err := svc.Login(ctx, "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Legitimate client rotates once.
	refreshed, err := svc.RefreshToken(ctx, loginToken.RefreshToken, "127.0.0.1")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// Attacker (or stale-cookie client) replays the original token.
	// Different IP so the per-IP rate limiter doesn't conflate it
	// with a brute-force burst from the legit client.
	if _, err := svc.RefreshToken(ctx, loginToken.RefreshToken, "203.0.113.5"); err == nil {
		t.Fatal("reused old refresh token must be rejected")
	}

	// After reuse detection fires, the session is gone. Even the
	// rotated token (which the legitimate client still holds) MUST
	// stop working — that's the whole point of the revoke. The user
	// has to re-authenticate via /login.
	if _, err := svc.RefreshToken(ctx, refreshed.RefreshToken, "127.0.0.1"); err == nil {
		t.Fatal("session should have been revoked by reuse detection; rotated token still accepted")
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

// registerProfileWithPIN registers a child profile under the given
// parent and pins it with the supplied 4-digit PIN. Returns the
// resulting child user record so tests can use its ID.
func registerProfileWithPIN(
	t *testing.T,
	svc *auth.Service,
	parent *db.User,
	username, pin string,
) *db.User {
	t.Helper()
	child, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username:     parent.Username + "/" + username,
		DisplayName:  username,
		Password:     "irrelevant-but-required",
		Role:         "user",
		ParentUserID: parent.ID,
	})
	if err != nil {
		t.Fatalf("register profile: %v", err)
	}
	if err := svc.SetPIN(context.Background(), child.ID, pin); err != nil {
		t.Fatalf("set pin: %v", err)
	}
	return child
}

// TestService_SwitchProfile_PinRateLimited asserts that hammering a
// PIN-protected profile with wrong PINs trips the same loginRateLimiter
// Login uses, locking out further attempts (even with the correct PIN)
// until the lockout expires. Without this guard a 4-digit PIN — only
// 10k combinations — falls in minutes against bcrypt.
func TestService_SwitchProfile_PinRateLimited(t *testing.T) {
	const maxFails = 3
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	parent := registerTestUser(t, svc)
	child := registerProfileWithPIN(t, svc, parent, "kid", "1234")

	const ip = "203.0.113.7"

	// Drive maxFails wrong PINs from the same IP. Each attempt should
	// surface ErrInvalidPassword (the wire-level "wrong credential"
	// sentinel — same code as a wrong password to avoid enumeration).
	for i := 0; i < maxFails; i++ {
		_, err := svc.SwitchProfile(
			context.Background(),
			parent.ID,
			child.ID,
			"0000",
			"Chrome",
			"dev-1",
			ip,
		)
		if !errors.Is(err, domain.ErrInvalidPassword) {
			t.Fatalf("attempt %d: expected ErrInvalidPassword, got %v", i, err)
		}
	}

	// Now even the correct PIN must be rejected — the profile is
	// locked. The error code flips to ErrForbidden so callers can
	// distinguish "your PIN is wrong" from "you've been throttled."
	_, err := svc.SwitchProfile(
		context.Background(),
		parent.ID,
		child.ID,
		"1234",
		"Chrome",
		"dev-1",
		ip,
	)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("expected ErrForbidden after %d wrong PINs, got %v", maxFails, err)
	}
}

// TestService_SwitchProfile_PinLockoutPerProfile asserts the lock is
// scoped to the target profile, not the whole family. A different
// PIN-protected sibling under the same parent must remain reachable
// even after the first sibling is locked out.
func TestService_SwitchProfile_PinLockoutPerProfile(t *testing.T) {
	const maxFails = 3
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	parent := registerTestUser(t, svc)
	locked := registerProfileWithPIN(t, svc, parent, "kid-a", "1111")
	free := registerProfileWithPIN(t, svc, parent, "kid-b", "2222")

	// Hammer the first sibling from a clean IP; we exercise the
	// per-profile counter rather than the per-IP one by spreading
	// each attempt across a fresh IP.
	for i := 0; i < maxFails; i++ {
		_, err := svc.SwitchProfile(
			context.Background(),
			parent.ID,
			locked.ID,
			"0000",
			"Chrome",
			"dev-1",
			"10.0.0."+string(rune('a'+i)),
		)
		if !errors.Is(err, domain.ErrInvalidPassword) {
			t.Fatalf("attempt %d: expected ErrInvalidPassword, got %v", i, err)
		}
	}

	// Sibling A is now locked even with a correct PIN.
	if _, err := svc.SwitchProfile(
		context.Background(),
		parent.ID,
		locked.ID,
		"1111",
		"Chrome",
		"dev-1",
		"198.51.100.1",
	); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("expected sibling-a locked, got %v", err)
	}

	// Sibling B with its own PIN must still resolve from a fresh IP.
	if _, err := svc.SwitchProfile(
		context.Background(),
		parent.ID,
		free.ID,
		"2222",
		"Chrome",
		"dev-2",
		"198.51.100.2",
	); err != nil {
		t.Errorf("expected sibling-b to remain reachable, got %v", err)
	}
}

// TestService_SwitchProfile_PinSuccessClearsCounter asserts that a
// correct PIN before the limit clears the failure counter, so users
// who fat-finger their PIN once or twice don't see the lockout drift
// closer over the day.
func TestService_SwitchProfile_PinSuccessClearsCounter(t *testing.T) {
	const maxFails = 3
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	parent := registerTestUser(t, svc)
	child := registerProfileWithPIN(t, svc, parent, "kid", "1234")

	const ip = "203.0.113.42"

	// Two wrong PINs (one short of the lock).
	for i := 0; i < maxFails-1; i++ {
		if _, err := svc.SwitchProfile(
			context.Background(),
			parent.ID,
			child.ID,
			"0000",
			"Chrome",
			"dev-1",
			ip,
		); !errors.Is(err, domain.ErrInvalidPassword) {
			t.Fatalf("warmup %d: %v", i, err)
		}
	}

	// One success — clears both per-profile and per-IP counters.
	if _, err := svc.SwitchProfile(
		context.Background(),
		parent.ID,
		child.ID,
		"1234",
		"Chrome",
		"dev-1",
		ip,
	); err != nil {
		t.Fatalf("correct PIN should succeed before lockout: %v", err)
	}

	// We can now afford another full burst of `maxFails-1` wrong
	// PINs without tripping the lock — proves the counter reset.
	for i := 0; i < maxFails-1; i++ {
		if _, err := svc.SwitchProfile(
			context.Background(),
			parent.ID,
			child.ID,
			"9999",
			"Chrome",
			"dev-1",
			ip,
		); !errors.Is(err, domain.ErrInvalidPassword) {
			t.Errorf("after-success attempt %d should still be invalid-password (not locked), got %v", i, err)
		}
	}
}

// TestService_SwitchProfile_NoPin_NotRateLimited asserts the rate
// limiter is dormant when the target profile has no PIN — switching
// to an unlocked profile should never grow attempt records that could
// tip a sibling into lockout.
func TestService_SwitchProfile_NoPin_NotRateLimited(t *testing.T) {
	const maxFails = 2
	svc, _, _ := newTestAuthServiceWithRL(t, maxFails)
	parent := registerTestUser(t, svc)
	child, err := svc.Register(context.Background(), auth.RegisterRequest{
		Username:     parent.Username + "/open",
		DisplayName:  "open",
		Password:     "irrelevant-but-required",
		Role:         "user",
		ParentUserID: parent.ID,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// More iterations than maxFails — none should accumulate a
	// failure record because the PIN branch is skipped entirely.
	for i := 0; i < maxFails*3; i++ {
		if _, err := svc.SwitchProfile(
			context.Background(),
			parent.ID,
			child.ID,
			"",
			"Chrome",
			"dev-1",
			"10.0.0.1",
		); err != nil {
			t.Fatalf("switch %d: expected success, got %v", i, err)
		}
	}
}

// recordingSubscriber collects events for assertions. The bus
// dispatches handlers in goroutines (see internal/event/bus.go),
// so waitForEvents polls the snapshot until the expected count
// arrives or the deadline expires.
type recordingSubscriber struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *recordingSubscriber) handle(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSubscriber) snapshot() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.Event, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recordingSubscriber) waitForCount(t *testing.T, want int) []event.Event {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got := r.snapshot()
		if len(got) >= want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return r.snapshot()
}

// TestService_RevokeSession_PublishesUserLoggedOut pins the contract
// the frontend "Tus dispositivos" panel relies on: an event hits
// /me/events the moment a session is revoked, no 30 s poll needed.
// The previous behaviour was silent — only Logout published.
func TestService_RevokeSession_PublishesUserLoggedOut(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	bus := event.NewBus(testutil.NopLogger())
	svc.SetEventBus(bus)
	rec := &recordingSubscriber{}
	bus.Subscribe(event.UserLoggedOut, rec.handle)

	user := registerTestUser(t, svc)
	token, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Resolve the session ID from the refresh-token cookie path the
	// handler uses, so the test mirrors production.
	sessionID := svc.CurrentSessionID(context.Background(), token.RefreshToken)
	if sessionID == "" {
		t.Fatal("expected non-empty session id from CurrentSessionID")
	}

	if err := svc.RevokeSession(context.Background(), user.ID, sessionID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	events := rec.waitForCount(t, 1)
	if len(events) != 1 {
		t.Fatalf("expected 1 UserLoggedOut event, got %d: %+v", len(events), events)
	}
	got := events[0]
	if got.Type != event.UserLoggedOut {
		t.Errorf("event type: got %q want %q", got.Type, event.UserLoggedOut)
	}
	if uid, _ := got.Data["user_id"].(string); uid != user.ID {
		t.Errorf("event user_id: got %q want %q", uid, user.ID)
	}
	if sid, _ := got.Data["session_id"].(string); sid != sessionID {
		t.Errorf("event session_id: got %q want %q", sid, sessionID)
	}
}

// TestService_RevokeSession_ForeignSession_NoPublish guards the
// anti-enumeration carve-out: revoking a session that belongs to
// another user must NOT publish, because publishing would confirm
// the session ID exists to an attacker probing the endpoint.
func TestService_RevokeSession_ForeignSession_NoPublish(t *testing.T) {
	svc, _, _ := newTestAuthService(t)
	bus := event.NewBus(testutil.NopLogger())
	svc.SetEventBus(bus)
	rec := &recordingSubscriber{}
	bus.Subscribe(event.UserLoggedOut, rec.handle)

	owner := registerTestUser(t, svc)
	token, err := svc.Login(context.Background(), "testuser", "password123", "Chrome", "dev-1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	sessionID := svc.CurrentSessionID(context.Background(), token.RefreshToken)
	if sessionID == "" {
		t.Fatal("expected non-empty session id")
	}

	// Try to revoke owner's session as a different user.
	attackerID := owner.ID + "-other"
	err = svc.RevokeSession(context.Background(), attackerID, sessionID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign session, got %v", err)
	}
	if got := rec.snapshot(); len(got) != 0 {
		t.Errorf("foreign-session revoke must not publish; got %d events: %+v", len(got), got)
	}
}
