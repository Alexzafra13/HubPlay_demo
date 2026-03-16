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

	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
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
}

type Service struct {
	users       *db.UserRepository
	sessions    *db.SessionRepository
	cfg         config.AuthConfig
	clock       clock.Clock
	logger      *slog.Logger
	stopCh      chan struct{}
	rateLimiter *loginRateLimiter
}

func NewService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
) *Service {
	return &Service{
		users:       users,
		sessions:    sessions,
		cfg:         cfg,
		clock:       clk,
		logger:      logger.With("module", "auth"),
		stopCh:      make(chan struct{}),
		rateLimiter: newLoginRateLimiter(5, 15*time.Minute, 15*time.Minute), // 5 fails in 15min → locked 15min
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

// StopSessionCleaner stops the background session cleanup goroutine.
func (s *Service) StopSessionCleaner() {
	close(s.stopCh)
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*db.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BCryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	role := req.Role
	if role == "" {
		role = "user"
	}

	user := &db.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		DisplayName:  req.DisplayName,
		PasswordHash: string(hash),
		Role:         role,
		IsActive:     true,
		CreatedAt:    s.clock.Now(),
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
	return token, nil
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*AuthToken, error) {
	tokenHash := hashToken(refreshToken)

	session, err := s.sessions.GetByRefreshTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("refresh: %w", domain.ErrInvalidToken)
		}
		return nil, fmt.Errorf("refresh lookup: %w", err)
	}

	if s.clock.Now().After(session.ExpiresAt) {
		if delErr := s.sessions.DeleteByID(ctx, session.ID); delErr != nil {
			s.logger.Warn("failed to delete expired session", "session_id", session.ID, "error", delErr)
		}
		return nil, fmt.Errorf("refresh: %w", domain.ErrTokenExpired)
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("refresh user lookup: %w", err)
	}

	if !user.IsActive {
		return nil, fmt.Errorf("refresh: %w", domain.ErrAccountDisabled)
	}

	// Generate new access token (keep same refresh token + session)
	accessToken, expiresAt, err := generateAccessToken(
		s.cfg.JWTSecret, user.ID, user.Username, user.Role, s.cfg.AccessTokenTTL, s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}

	_ = s.sessions.UpdateLastActive(ctx, session.ID, s.clock.Now())

	return &AuthToken{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		UserID:       user.ID,
		Role:         user.Role,
	}, nil
}

func (s *Service) ValidateToken(ctx context.Context, tokenStr string) (*Claims, error) {
	claims, err := validateAccessToken(s.cfg.JWTSecret, tokenStr)
	if err != nil {
		return nil, fmt.Errorf("validate: %w", domain.ErrInvalidToken)
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

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	tokenHash := hashToken(refreshToken)
	return s.sessions.DeleteByRefreshTokenHash(ctx, tokenHash)
}

func (s *Service) createSession(ctx context.Context, user *db.User, deviceName, deviceID, ip string) (*AuthToken, error) {
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

	accessToken, expiresAt, err := generateAccessToken(
		s.cfg.JWTSecret, user.ID, user.Username, user.Role, s.cfg.AccessTokenTTL, s.clock.Now(),
	)
	if err != nil {
		return nil, err
	}

	refreshToken := generateRefreshToken()
	now := s.clock.Now()

	session := &db.Session{
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

func generateRefreshToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
