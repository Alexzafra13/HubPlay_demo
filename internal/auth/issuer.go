package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
)

// sessionIssuer concentra la lógica compartida entre LoginService.Login
// y ProfileService.SwitchProfile: ambos necesitan minteo de un session
// row + un par de tokens (access JWT + refresh hex) para un usuario.
// Comparte el mismo punto de inyección de límite "MaxSessionsPerUser"
// (eviction del más antiguo cuando se supera el quota).
//
// Vive como tipo independiente para que el split de Service en 4
// sub-services no tenga que duplicar 60+ líneas — `Service.createSession`
// del fichero original era el helper más golpeado del paquete (8 deps).
type sessionIssuer struct {
	users    *db.UserRepository
	sessions *db.SessionRepository
	keys     *KeyStore
	cfg      config.AuthConfig
	clock    clock.Clock
	logger   *slog.Logger
}

func newSessionIssuer(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	keys *KeyStore,
	cfg config.AuthConfig,
	clk clock.Clock,
	logger *slog.Logger,
) *sessionIssuer {
	return &sessionIssuer{
		users:    users,
		sessions: sessions,
		keys:     keys,
		cfg:      cfg,
		clock:    clk,
		logger:   logger,
	}
}

// issue mintea un session row + par de tokens para el usuario dado.
// Cuando `MaxSessionsPerUser` > 0, evictea las sesiones más antiguas
// hasta caer bajo el quota (FIFO por created_at). Idéntico al
// `createSession` original — sólo cambia el receiver.
func (i *sessionIssuer) issue(
	ctx context.Context,
	user *authmodel.User,
	deviceName, deviceID, ip string,
) (*AuthToken, error) {
	if i.cfg.MaxSessionsPerUser > 0 {
		count, err := i.sessions.CountByUser(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("counting sessions: %w", err)
		}
		for count >= i.cfg.MaxSessionsPerUser {
			if err := i.sessions.DeleteOldestByUser(ctx, user.ID); err != nil {
				i.logger.Warn("failed to evict oldest session", "user_id", user.ID, "error", err)
				break
			}
			count--
			i.logger.Info("evicted oldest session", "user_id", user.ID, "remaining", count)
		}
	}

	primary, err := i.keys.Current()
	if err != nil {
		return nil, fmt.Errorf("issue session: %w", err)
	}
	accessToken, expiresAt, err := generateAccessToken(
		primary, user.ID, user.Username, user.Role, i.cfg.AccessTokenTTL, i.clock.Now(),
	)
	if err != nil {
		return nil, err
	}

	refreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, err
	}
	now := i.clock.Now()

	session := &authmodel.Session{
		ID:               uuid.New().String(),
		UserID:           user.ID,
		DeviceName:       deviceName,
		DeviceID:         deviceID,
		IPAddress:        ip,
		RefreshTokenHash: hashToken(refreshToken),
		CreatedAt:        now,
		LastActiveAt:     now,
		ExpiresAt:        now.Add(i.cfg.RefreshTokenTTL),
	}

	if err := i.sessions.Create(ctx, session); err != nil {
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

// generateRefreshToken devuelve un refresh token aleatorio
// representado como 32 bytes en hex (64 chars). Vive aquí (no en
// service.go) porque el issuer es el dueño funcional — RefreshToken
// también lo usa para rotación pero pasa por el mismo helper.
func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating refresh token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// publisher es el contenedor compartido del *event.Bus opcional. Los
// sub-services que publican (LoginService al login, SessionService al
// logout/revoke) tienen un puntero al mismo `*publisher`, así un único
// `Service.SetEventBus(bus)` mutate el campo de ambos a la vez sin
// que cada sub-service tenga que exponer su propio setter.
type publisher struct {
	bus *event.Bus
}

func (p *publisher) publish(e event.Event) {
	if p == nil || p.bus == nil {
		return
	}
	p.bus.Publish(e)
}

func (p *publisher) setBus(bus *event.Bus) {
	if p == nil {
		return
	}
	p.bus = bus
}
