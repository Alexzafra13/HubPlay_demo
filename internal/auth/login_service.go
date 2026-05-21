package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// LoginService cubre el flujo de autenticación interactiva:
// `Login`, `RefreshToken` y `ValidateToken`. Comparte el
// `loginRateLimiter` con `ProfileService` (PIN de switch profile)
// pero el resto del estado es propio.
type LoginService struct {
	users       *db.UserRepository
	sessions    *db.SessionRepository
	keys        *KeyStore
	cfg         config.AuthConfig
	clock       clock.Clock
	logger      *slog.Logger
	rateLimiter *loginRateLimiter
	issuer      *sessionIssuer
	pub         *publisher
}

func newLoginService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	keys *KeyStore,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
	rl *loginRateLimiter,
	issuer *sessionIssuer,
	pub *publisher,
) *LoginService {
	return &LoginService{
		users:       users,
		sessions:    sessions,
		keys:        keys,
		cfg:         cfg,
		clock:       clk,
		logger:      logger,
		rateLimiter: rl,
		issuer:      issuer,
		pub:         pub,
	}
}

// Login verifica credenciales, gatea por rate-limit (username + IP)
// y mintea un session row vía el `sessionIssuer`.
func (s *LoginService) Login(ctx context.Context, username, password, deviceName, deviceID, ip string) (*AuthToken, error) {
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
	// background job; la comparación ocurre aquí en cada login Y
	// dentro del middleware JWT (así un token ya emitido no sobrevive
	// más de un JWT TTL después del deadline). Sentinel distinto del
	// genérico "cuenta deshabilitada" para que la UI pueda mostrar
	// un mensaje a medida ("contacta al admin para extender acceso").
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

	s.rateLimiter.recordSuccess(username)
	s.rateLimiter.recordSuccess("ip:" + ip)

	token, err := s.issuer.issue(ctx, user, deviceName, deviceID, ip)
	if err != nil {
		return nil, err
	}

	if err := s.users.UpdateLastLogin(ctx, user.ID, s.clock.Now()); err != nil {
		s.logger.Warn("failed to update last login", "user_id", user.ID, "error", err)
	}

	s.logger.Info("user logged in", "user_id", user.ID, "username", user.Username, "device", deviceName)
	s.pub.publish(event.Event{
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

// RefreshToken canjea un refresh token válido por un access nuevo
// + rotación del propio refresh.
//
// Misma surface de brute-force que Login: un refresh token leakeado o
// adivinado puede repetirse indefinidamente sin gate. Reusamos el
// mismo limiter con keys "refresh:" namespaced para que un flood de
// refresh no bloquee el login del usuario y viceversa.
//
// Dos keys porque protegen modelos de atacante distintos:
//   - refresh:ip:<ip>      capa cualquier IP origen indistintamente
//                          del token probado (drag-net guessing).
//   - refresh:tok:<hash>   capa intentos contra UN token específico
//                          aunque rote IPs (defensa de token leakeado).
func (s *LoginService) RefreshToken(ctx context.Context, refreshToken, ip string) (*AuthToken, error) {
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
		// Reuse detection: si no matchea el hash actual, intentamos
		// contra el hash previo guardado tras la última rotación.
		// Si matchea, o bien es un atacante replayando un token
		// robado (el legítimo ya rotó) o el cliente legítimo
		// reintentando tras perder la respuesta. No podemos
		// distinguirlos; la respuesta segura es revocar la sesión
		// entera y forzar login fresco — ambas partes acaban en
		// /auth/login que tiene el mismo rate-limit.
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

	// Temporary-access window: misma lazy enforcement que Login.
	// Sin esta comprobación un user cuya ventana expiró mid-session
	// podría seguir extendiéndose via /auth/refresh y montar tokens
	// indefinidamente (la ruta validate-token la pilla en un TTL
	// pero refresh emite uno fresco, así que el deadline slipearía
	// otro full TTL por llamada).
	if user.AccessExpiresAt != nil && !user.AccessExpiresAt.After(s.clock.Now()) {
		s.rateLimiter.recordFailure(ipKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrAccessExpired)
	}

	if !user.IsActive {
		s.rateLimiter.recordFailure(ipKey)
		return nil, fmt.Errorf("refresh: %w", domain.ErrAccountDisabled)
	}

	s.rateLimiter.recordSuccess(ipKey)
	s.rateLimiter.recordSuccess(tokKey)

	// Access token con la primary actual. Tokens firmados con la
	// previa primary (si hubo rotación mid-session) siguen validando
	// hasta que esa key se prune.
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

	// Rotación del refresh: cada refresh exitoso mintea un secret
	// nuevo y atómicamente sustituye el hash en la fila, así el
	// viejo muere en cuanto la respuesta sale del server. Antes
	// (sin rotación) un refresh leakeado vivía RefreshTokenTTL
	// (30 días) sin detección. La rotación sola no da reuse
	// detection (haría falta una columna `rotated_at`), pero capa
	// la ventana del atacante a "hasta que el cliente legítimo
	// refresca una vez" — típicamente minutos — y es invisible
	// para un cliente bien comportado.
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

// ValidateToken verifica criptográficamente un access token contra
// el keystore y aplica la lazy check de temp-access + account-active.
func (s *LoginService) ValidateToken(ctx context.Context, tokenStr string) (*Claims, error) {
	claims, err := validateAccessToken(func(kid string) (*authmodel.SigningKey, error) {
		return s.keys.Lookup(kid)
	}, tokenStr)
	if err != nil {
		return nil, fmt.Errorf("validate: %w", domain.ErrInvalidToken)
	}
	// Temporary-access window: el JWT puede seguir siendo cripto-
	// válido pero la ventana del user puede haber cerrado desde que
	// se emitió. Lazy check en cada request — staleness máxima =
	// JWT TTL (15 min default), sin background job. Swallow del
	// lookup error porque forzar cada request authenticada a un DB
	// hit tankearía latencia del hot path; si el user desapareció
	// mid-flight el repo call downstream devuelve NotFound igual.
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
