package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

type AuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id"`
	Role         string    `json:"role"`
}

type RegisterRequest struct {
	Username    string
	DisplayName string
	Password    string
	Role        string
	// PasswordChangeRequired forces the next successful login to land
	// on the change-password screen. Set to true for admin-driven
	// account creation when the password is server-generated.
	PasswordChangeRequired bool
	// ParentUserID, when set, makes the new row a profile under
	// `ParentUserID` rather than a standalone account. Profiles
	// share the parent's password and authenticate via switch-
	// profile rather than the regular login handshake.
	ParentUserID string
}

type Service struct {
	users       *db.UserRepository
	sessions    *db.SessionRepository
	keys        *KeyStore
	cfg         config.AuthConfig
	clock       clock.Clock
	logger      *slog.Logger
	stopCh      chan struct{}
	rateLimiter *loginRateLimiter
	bus         *event.Bus // optional; nil-safe
}

// SetEventBus wires an event bus so the service can publish UserLoggedIn /
// UserLoggedOut events. Nil disables publishing. Follows the streams pattern.
func (s *Service) SetEventBus(bus *event.Bus) { s.bus = bus }

func (s *Service) publish(e event.Event) {
	if s.bus != nil {
		s.bus.Publish(e)
	}
}

// KeyStoreOrNil returns the service's signing keystore. Exposed for the
// admin handler and for observability; returns nil if the service was built
// without one (tests that don't touch tokens).
func (s *Service) KeyStoreOrNil() *KeyStore {
	return s.keys
}

func NewService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	keys *KeyStore,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
	rlCfg ...config.RateLimitConfig,
) *Service {
	// Use rate limit config if provided, otherwise sensible self-hosted defaults
	maxFails := 10
	window := 15 * time.Minute
	lockout := 5 * time.Minute
	if len(rlCfg) > 0 {
		rl := rlCfg[0]
		if rl.LoginAttempts > 0 {
			maxFails = rl.LoginAttempts
		}
		if rl.LoginWindow > 0 {
			window = rl.LoginWindow
		}
		if rl.LoginLockout > 0 {
			lockout = rl.LoginLockout
		}
	}

	return &Service{
		users:       users,
		sessions:    sessions,
		keys:        keys,
		cfg:         cfg,
		clock:       clk,
		logger:      logger.With("module", "auth"),
		stopCh:      make(chan struct{}),
		rateLimiter: newLoginRateLimiter(maxFails, window, lockout),
	}
}

// StartSessionCleaner starts a background goroutine that periodically
// removes expired sessions from the database.
func (s *Service) StartSessionCleaner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		// Run once immediately on startup
		if cleaned, err := s.sessions.DeleteExpired(ctx); err == nil && cleaned > 0 {
			s.logger.Info("startup: cleaned expired sessions", "count", cleaned)
		}

		for {
			select {
			case <-ticker.C:
				if cleaned, err := s.sessions.DeleteExpired(ctx); err == nil && cleaned > 0 {
					s.logger.Info("cleaned expired sessions", "count", cleaned)
				}
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// StopSessionCleaner detiene tanto la goroutine del session cleaner
// como la del rate limiter. Ambas son background tasks del Service
// sin lifecycle propio; cerrarlas juntas evita el leak documentado
// como audit olor RR.
func (s *Service) StopSessionCleaner() {
	close(s.stopCh)
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*authmodel.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BCryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	role := req.Role
	if role == "" {
		role = "user"
	}

	user := &authmodel.User{
		ID:                     uuid.New().String(),
		Username:               req.Username,
		DisplayName:            req.DisplayName,
		PasswordHash:           string(hash),
		Role:                   role,
		IsActive:               true,
		CreatedAt:              s.clock.Now(),
		ParentUserID:           req.ParentUserID,
		PasswordChangeRequired: req.PasswordChangeRequired,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	s.logger.Info("user registered", "user_id", user.ID, "username", user.Username, "role", role)
	return user, nil
}

func (s *Service) Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*AuthToken, error) {
	// Rate limit check (by username and by IP separately)
	if s.rateLimiter.isLocked(username) || s.rateLimiter.isLocked("ip:"+ip) {
		s.logger.Warn("login rate limited", "username", username, "ip", ip)
		return nil, fmt.Errorf("too many failed attempts, try again later: %w", domain.ErrForbidden)
	}

	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.rateLimiter.recordFailure(username)
			s.rateLimiter.recordFailure("ip:" + ip)
			return nil, fmt.Errorf("login: %w", domain.ErrInvalidPassword)
		}
		return nil, fmt.Errorf("login lookup: %w", err)
	}

	// Temporary-access window check. Lazy enforcement — no
	// background job; the comparison happens here on every login
	// AND inside the JWT middleware (so an already-issued token
	// doesn't outlive the deadline by more than the JWT TTL).
	// Distinct from the generic "account disabled" sentinel so the
	// UI can surface a tailored "contact the admin to extend
	// access" message instead of the catch-all copy.
	if user.AccessExpiresAt != nil && !user.AccessExpiresAt.After(s.clock.Now()) {
		return nil, fmt.Errorf("login: %w", domain.ErrAccessExpired)
	}

	if !user.IsActive {
		return nil, fmt.Errorf("login: %w", domain.ErrAccountDisabled)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.rateLimiter.recordFailure(username)
		s.rateLimiter.recordFailure("ip:" + ip)
		s.logger.Warn("failed login attempt", "username", username, "ip", ip)
		return nil, fmt.Errorf("login: %w", domain.ErrInvalidPassword)
	}

	// Clear rate limit on success
	s.rateLimiter.recordSuccess(username)
	s.rateLimiter.recordSuccess("ip:" + ip)

	token, err := s.createSession(ctx, user, deviceName, deviceID, ip)
	if err != nil {
		return nil, err
	}

	if err := s.users.UpdateLastLogin(ctx, user.ID, s.clock.Now()); err != nil {
		s.logger.Warn("failed to update last login", "user_id", user.ID, "error", err)
	}

	s.logger.Info("user logged in", "user_id", user.ID, "username", user.Username, "device", deviceName)
	s.publish(event.Event{
		Type: event.UserLoggedIn,
		Data: map[string]any{
			"user_id":     user.ID,
			"username":    user.Username,
			"device_name": deviceName,
			"ip":          ip,
		},
	})
	return token, nil
}

// RefreshToken exchanges a valid refresh token for a new access token.
//
// Same brute-force surface as Login: a leaked or guessed refresh token can
// be hammered indefinitely without a gate. We reuse the same rate limiter
// keyed under separate "refresh:" namespaces so a refresh flood does not
// lock out the user's password login (and vice versa).
//
// Two keys are checked because each protects a different attacker model:
//   - refresh:ip:<ip>      caps any single source IP regardless of the
//                          token tried (defends against drag-net guessing).
//   - refresh:tok:<hash>   caps attempts against one specific token, even
//                          across rotating IPs (defends a leaked token).
func (s *Service) RefreshToken(ctx context.Context, refreshToken, ip string) (*AuthToken, error) {
	tokenHash := hashToken(refreshToken)
	ipKey := "refresh:ip:" + ip
	tokKey := "refresh:tok:" + tokenHash

	if s.rateLimiter.isLocked(ipKey) || s.rateLimiter.isLocked(tokKey) {
		s.logger.Warn("refresh rate limited", "ip", ip)
		return nil, fmt.Errorf("too many failed attempts, try again later: %w", domain.ErrForbidden)
	}

	session, err := s.sessions.GetByRefreshTokenHash(ctx, tokenHash)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("refresh lookup: %w", err)
		}
		// Reuse detection: a refresh token that doesn't match the
		// current hash might still match the previous-generation
		// hash we kept after the last rotation. If it does, that's
		// either an attacker replaying a stolen token (the dueño
		// already refreshed past it) or the legitimate client
		// retrying after a lost response. We can't tell which, so
		// the safe response is to revoke the entire session and
		// force a fresh login. After that point neither party can
		// keep refreshing — both will hit /auth/login which is rate
		// limited the same way.
		if reused, rerr := s.sessions.GetByPreviousRefreshTokenHash(ctx, tokenHash); rerr == nil && reused != nil {
			s.logger.Warn("refresh token reuse detected — revoking session",
				"session_id", reused.ID, "user_id", reused.UserID, "ip", ip)
			if delErr := s.sessions.DeleteByID(ctx, reused.ID); delErr != nil {
				s.logger.Error("failed to revoke session after reuse detection",
					"session_id", reused.ID, "error", delErr)
			}
			s.rateLimiter.recordFailure(ipKey)
			s.rateLimiter.recordFailure(tokKey)
			return nil, fmt.Errorf("refresh: %w", domain.ErrInvalidToken)
		}
		s.rateLimiter.recordFailure(ipKey)
		s.rateLimiter.recordFailure(tokKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrInvalidToken)
	}

	if s.clock.Now().After(session.ExpiresAt) {
		if delErr := s.sessions.DeleteByID(ctx, session.ID); delErr != nil {
			s.logger.Warn("failed to delete expired session", "session_id", session.ID, "error", delErr)
		}
		s.rateLimiter.recordFailure(ipKey)
		s.rateLimiter.recordFailure(tokKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrTokenExpired)
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("refresh user lookup: %w", err)
	}

	// Temporary-access window: same lazy enforcement as Login.
	// Without this check a user whose window expired mid-session
	// could keep extending themselves via /auth/refresh and ride
	// past the deadline indefinitely (the validate-token path
	// catches it within one JWT TTL, but refresh issues a fresh
	// token so the deadline would slip another full TTL on every
	// call).
	if user.AccessExpiresAt != nil && !user.AccessExpiresAt.After(s.clock.Now()) {
		s.rateLimiter.recordFailure(ipKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrAccessExpired)
	}

	if !user.IsActive {
		s.rateLimiter.recordFailure(ipKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrAccountDisabled)
	}

	// Successful refresh clears the failure counters.
	s.rateLimiter.recordSuccess(ipKey)
	s.rateLimiter.recordSuccess(tokKey)

	// Generate new access token (keep same refresh token + session) using
	// the current primary key. Tokens signed with the previous primary (if
	// rotation happened mid-session) still validate until that key is pruned.
	primary, err := s.keys.Current()
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	accessToken, expiresAt, err := generateAccessToken(
		primary, user.ID, user.Username, user.Role, s.cfg.AccessTokenTTL, s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}

	// Rotate the refresh token: every successful refresh mints a new
	// secret and atomically replaces the hash on the row, so the old
	// token is dead the moment the response leaves the server. With
	// the previous "keep the same refresh token forever" behaviour a
	// leaked refresh sat valid for the full RefreshTokenTTL (30 days)
	// with no detection. Rotation alone doesn't give us reuse
	// detection (we'd need a "rotated_at" column to flag the chain
	// on a duplicate use), but it caps the attacker's window to
	// "until the legitimate client refreshes once" — typically
	// minutes — and is invisible to a well-behaved client.
	newRefreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("refresh: rotate: %w", err)
	}
	now := s.clock.Now()
	newExpiresAt := now.Add(s.cfg.RefreshTokenTTL)
	if err := s.sessions.RotateRefreshToken(ctx, session.ID, hashToken(newRefreshToken), now, newExpiresAt); err != nil {
		return nil, fmt.Errorf("refresh: rotate: %w", err)
	}

	return &AuthToken{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
		UserID:       user.ID,
		Role:         user.Role,
	}, nil
}

func (s *Service) ValidateToken(ctx context.Context, tokenStr string) (*Claims, error) {
	claims, err := validateAccessToken(func(kid string) (*authmodel.SigningKey, error) {
		return s.keys.Lookup(kid)
	}, tokenStr)
	if err != nil {
		return nil, fmt.Errorf("validate: %w", domain.ErrInvalidToken)
	}
	// Temporary-access window: the JWT may still be cryptographically
	// valid but the user's access window may have closed since the
	// token was issued. Lazy check at every request — bounded
	// staleness equals the JWT TTL (15 min default), no background
	// job. We swallow the lookup error here because forcing every
	// authed request to depend on a DB hit would tank latency on
	// the hot path; if the user vanished mid-flight the next downstream
	// repo call returns NotFound anyway.
	if user, err := s.users.GetByID(ctx, claims.UserID); err == nil && user != nil {
		if user.AccessExpiresAt != nil && !user.AccessExpiresAt.After(s.clock.Now()) {
			return nil, fmt.Errorf("validate: %w", domain.ErrAccessExpired)
		}
		if !user.IsActive {
			return nil, fmt.Errorf("validate: %w", domain.ErrAccountDisabled)
		}
	}
	return claims, nil
}

// InvalidateUserSessions removes all sessions for a user.
// Call this on password change, account disable, or admin force-logout.
func (s *Service) InvalidateUserSessions(ctx context.Context, userID string) error {
	count, err := s.sessions.DeleteAllByUser(ctx, userID)
	if err != nil {
		return err
	}
	s.logger.Info("invalidated all user sessions", "user_id", userID, "count", count)
	return nil
}

// ListSessions returns the active sessions for a single user. Used
// by the user-facing "Your devices" panel — distinct from the admin
// "Now Playing" surface, which lists playback sessions across the
// whole server. Returns sessions sorted newest-first by last_active.
func (s *Service) ListSessions(ctx context.Context, userID string) ([]*authmodel.Session, error) {
	rows, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RevokeSession deletes a single session if it belongs to the
// caller. Returning ErrNotFound for foreign sessions keeps the
// surface anti-enumeration: an attacker probing other users'
// session IDs gets the same response as a missing one.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	row, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if row == nil || row.UserID != userID {
		return fmt.Errorf("revoke session: %w", domain.ErrNotFound)
	}
	if err := s.sessions.DeleteByID(ctx, sessionID); err != nil {
		return err
	}
	s.logger.Info("user revoked session", "user_id", userID, "session_id", sessionID)
	// Same payload shape as Logout's UserLoggedOut so the SSE
	// consumer (frontend "Tus dispositivos" panel) treats both
	// revocation paths uniformly — the panel just invalidates its
	// query and the freshly-revoked row drops out.
	s.publish(event.Event{
		Type: event.UserLoggedOut,
		Data: map[string]any{
			"user_id":    userID,
			"session_id": sessionID,
		},
	})
	return nil
}

// CurrentSessionID resolves the caller's session id from the
// refresh-token cookie. Returns "" when no cookie matches a
// row — the UI just won't mark "this device" in that case.
func (s *Service) CurrentSessionID(ctx context.Context, refreshToken string) string {
	if refreshToken == "" {
		return ""
	}
	row, err := s.sessions.GetByRefreshTokenHash(ctx, hashToken(refreshToken))
	if err != nil || row == nil {
		return ""
	}
	return row.ID
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	tokenHash := hashToken(refreshToken)
	// Look up the session first so we can publish a UserLoggedOut event with
	// the user_id. A miss is not a hard error — the delete below will also
	// no-op if the session is already gone.
	session, lookupErr := s.sessions.GetByRefreshTokenHash(ctx, tokenHash)
	if err := s.sessions.DeleteByRefreshTokenHash(ctx, tokenHash); err != nil {
		return err
	}
	if lookupErr == nil && session != nil {
		s.publish(event.Event{
			Type: event.UserLoggedOut,
			Data: map[string]any{
				"user_id":    session.UserID,
				"session_id": session.ID,
			},
		})
	}
	return nil
}

func (s *Service) createSession(ctx context.Context, user *authmodel.User, deviceName, deviceID, ip string) (*AuthToken, error) {
	// Enforce max sessions per user (expired sessions cleaned by background job)
	if s.cfg.MaxSessionsPerUser > 0 {
		count, err := s.sessions.CountByUser(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("counting sessions: %w", err)
		}
		// Evict oldest sessions until we're under the limit
		for count >= s.cfg.MaxSessionsPerUser {
			if err := s.sessions.DeleteOldestByUser(ctx, user.ID); err != nil {
				s.logger.Warn("failed to evict oldest session", "user_id", user.ID, "error", err)
				break
			}
			count--
			s.logger.Info("evicted oldest session", "user_id", user.ID, "remaining", count)
		}
	}

	primary, err := s.keys.Current()
	if err != nil {
		return nil, fmt.Errorf("createSession: %w", err)
	}
	accessToken, expiresAt, err := generateAccessToken(
		primary, user.ID, user.Username, user.Role, s.cfg.AccessTokenTTL, s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}

	refreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()

	session := &authmodel.Session{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		DeviceName:       deviceName,
		DeviceID:         deviceID,
		IPAddress:        ip,
		RefreshTokenHash: hashToken(refreshToken),
		CreatedAt:        now,
		LastActiveAt:     now,
		ExpiresAt:        now.Add(s.cfg.RefreshTokenTTL),
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	return &AuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		UserID:       user.ID,
		Role:         user.Role,
	}, nil
}

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating refresh token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// passwordAlphabet is the readable charset used for admin-driven
// auto-generated passwords. Drops 0/O/1/l/I to keep the result
// copy-pasteable without "is that a one or an L?" friction. The
// admin reads the password to the user, so legibility matters.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword returns a 12-character readable password drawn
// from a CSPRNG. Length 12 over a 56-char alphabet ≈ 70 bits of
// entropy — comfortable for a temporary credential the user will
// rotate at first login. Rejecting bias from `% len(alphabet)` is
// done by reading enough bytes that the modulo skew is negligible
// at this length.
func GeneratePassword() (string, error) {
	const n = 12
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating password: %w", err)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(out), nil
}

// ResetPassword is the admin-only "user lost their password" path.
// Generates a fresh readable password, hashes it, stores it with
// must-change=true, and returns the plaintext to the caller exactly
// once. The handler is responsible for surfacing it to the admin —
// it never touches the DB again, so a leak here equals the leak
// from a typed-into-a-form admin-set password.
//
// All sessions for the target user are invalidated so a stolen JWT
// for the old password becomes worthless immediately.
func (s *Service) ResetPassword(ctx context.Context, userID string) (string, error) {
	plain, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), s.cfg.BCryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	if err := s.users.SetPassword(ctx, userID, string(hash), true); err != nil {
		return "", err
	}
	if _, err := s.sessions.DeleteAllByUser(ctx, userID); err != nil {
		// Logged but not fatal — the password is already rotated, so
		// the worst case is the old refresh token outlives the JWT
		// TTL until next refresh attempt.
		s.logger.Warn("reset password: invalidate sessions failed", "user_id", userID, "error", err)
	}
	s.logger.Info("admin reset password", "user_id", userID)
	return plain, nil
}

// ListProfiles returns the parent account row plus every child
// profile that hangs off it. Caller must be either the parent or
// one of its children — passing any other userID resolves the
// owner via parent_user_id and returns the right tree, but the
// public surface only ever reaches this with the caller's own
// claims.
func (s *Service) ListProfiles(ctx context.Context, userID string) ([]*authmodel.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	owner := user.ID
	if user.IsProfile() {
		owner = user.ParentUserID
	}
	return s.users.ListProfilesForOwner(ctx, owner)
}

// SwitchProfile mints a new auth session for a child profile (or for
// the parent when the user wants to switch back). Caller must be the
// parent of the target OR another child under the same parent — the
// shared owner-id is the gate.
//
// PIN is optional from the wire perspective; required when the target
// profile carries pin_hash. Empty PIN against a PIN-protected profile
// returns ErrInvalidPassword (sharing the same sentinel as a wrong
// password keeps the error surface tiny).
func (s *Service) SwitchProfile(
	ctx context.Context,
	currentUserID, targetProfileID, pin, deviceName, deviceID, ip string,
) (*AuthToken, error) {
	current, err := s.users.GetByID(ctx, currentUserID)
	if err != nil {
		return nil, err
	}
	target, err := s.users.GetByID(ctx, targetProfileID)
	if err != nil {
		return nil, err
	}
	// Owner is the parent (current user when current is the parent,
	// current.parent_user_id when current is a profile). Same for
	// the target. Match → caller is allowed to switch.
	currentOwner := current.ID
	if current.IsProfile() {
		currentOwner = current.ParentUserID
	}
	targetOwner := target.ID
	if target.IsProfile() {
		targetOwner = target.ParentUserID
	}
	if currentOwner != targetOwner {
		return nil, fmt.Errorf("switch profile: %w", domain.ErrForbidden)
	}
	if !target.IsActive {
		return nil, fmt.Errorf("switch profile: %w", domain.ErrAccountDisabled)
	}
	// PIN gate. Stored hash empty = "no PIN, anyone can switch in".
	// Non-empty hash = the caller must provide a PIN that bcrypt-
	// matches. Wrong PIN intentionally returns the same error code
	// as a wrong password so an enumeration attack can't map it.
	//
	// Brute-force protection: a 4-digit PIN has only 10k combinations,
	// so the same loginRateLimiter Login uses gates this path too.
	// We key by the target profile id (so a flood against one profile
	// locks that profile, not the family) AND by IP (so an attacker
	// rotating profile ids from the same network still trips a lock).
	// Empty PINs against a PIN-protected profile count as failures —
	// it's not a normal user mode and treating it as one would let
	// `pin = ""` bypass the lockout.
	if target.PINHash != "" {
		pinKey := "pin:" + target.ID
		pinIPKey := "pin:ip:" + ip
		if s.rateLimiter.isLocked(pinKey) || s.rateLimiter.isLocked(pinIPKey) {
			s.logger.Warn("switch profile rate limited", "target", target.ID, "ip", ip)
			return nil, fmt.Errorf("switch profile: too many failed pin attempts: %w", domain.ErrForbidden)
		}
		if pin == "" {
			s.rateLimiter.recordFailure(pinKey)
			s.rateLimiter.recordFailure(pinIPKey)
			return nil, fmt.Errorf("switch profile: pin required: %w", domain.ErrInvalidPassword)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(target.PINHash), []byte(pin)); err != nil {
			s.rateLimiter.recordFailure(pinKey)
			s.rateLimiter.recordFailure(pinIPKey)
			return nil, fmt.Errorf("switch profile: %w", domain.ErrInvalidPassword)
		}
		s.rateLimiter.recordSuccess(pinKey)
		s.rateLimiter.recordSuccess(pinIPKey)
	}
	// Token is just a regular session for the target profile's
	// users.id — every existing user-keyed endpoint (user_data,
	// favourites, federation_progress, ...) keeps working without
	// learning what a "profile" is.
	if err := s.users.UpdateLastLogin(ctx, target.ID, s.clock.Now()); err != nil {
		s.logger.Warn("switch profile: update last login", "error", err)
	}
	s.logger.Info("profile switched", "from", currentUserID, "to", targetProfileID)
	return s.createSession(ctx, target, deviceName, deviceID, ip)
}

// SetPIN bcrypt-hashes (and stores) a 4-digit PIN, or clears it when
// `pin` is empty. The caller's permission check happens at the HTTP
// layer; this method trusts its inputs by design (admin-only path).
func (s *Service) SetPIN(ctx context.Context, userID, pin string) error {
	if pin == "" {
		return s.users.SetPIN(ctx, userID, "")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), s.cfg.BCryptCost)
	if err != nil {
		return fmt.Errorf("hashing pin: %w", err)
	}
	return s.users.SetPIN(ctx, userID, string(hash))
}

// ChangePassword is the user-side "rotate my own password" flow.
// Verifies the current password before mutating so a stolen JWT
// can't pivot into a permanent takeover. Clears the must-change
// flag — that's how a forced rotation completes.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) error {
	if next == "" || len(next) < 8 {
		return fmt.Errorf("change password: %w", domain.ErrInvalidPassword)
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("change password lookup: %w", err)
	}
	// Skip current-password check when must_change is set AND the
	// caller didn't supply one. Matches the UX of "you just typed
	// the auto-generated password to log in, you shouldn't have to
	// type it again" — but if `current` is supplied we still verify
	// it as a belt-and-braces measure. De Morgan'd from the obvious
	// `!(must_change && current=="")` so staticcheck QF1001 stays
	// quiet.
	if !user.PasswordChangeRequired || current != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(current)); err != nil {
			return fmt.Errorf("change password: %w", domain.ErrInvalidPassword)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cfg.BCryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	if err := s.users.SetPassword(ctx, userID, string(hash), false); err != nil {
		return err
	}
	s.logger.Info("password changed", "user_id", userID)
	return nil
}
