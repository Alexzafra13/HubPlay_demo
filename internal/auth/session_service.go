package auth

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// SessionService cubre el ciclo de vida de las sesiones del usuario:
// listar, revocar, logout, invalidación bulk y la goroutine background
// que purga sesiones expiradas. El logout/revoke publican
// `UserLoggedOut` para que el frontend invalide su panel "Tus
// dispositivos".
type SessionService struct {
	users    *db.UserRepository
	sessions *db.SessionRepository
	logger   *slog.Logger
	stopCh   chan struct{}
	pub      *publisher
}

func newSessionService(
	users *db.UserRepository,
	sessions *db.SessionRepository,
	logger *slog.Logger,
	pub *publisher,
) *SessionService {
	return &SessionService{
		users:    users,
		sessions: sessions,
		logger:   logger,
		stopCh:   make(chan struct{}),
		pub:      pub,
	}
}

// StartSessionCleaner arranca una goroutine background que cada hora
// purga sesiones expiradas. Idempotente sobre múltiples llamadas (la
// más reciente gana — la anterior sigue corriendo hasta su próximo
// tick pero igualmente termina al `close(stopCh)`).
func (s *SessionService) StartSessionCleaner(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		// Una pasada inmediata al arranque para que un restart
		// no deje sesiones zombies durante la primera hora.
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

// StopSessionCleaner detiene la goroutine del cleaner. NO toca el
// rate-limiter — vive en LoginService y se cierra desde `Service.Close`
// (que coordina ambos shutdowns).
func (s *SessionService) StopSessionCleaner() {
	close(s.stopCh)
}

// InvalidateUserSessions borra TODAS las sesiones de un usuario.
// Llamado en password change, account disable o force-logout admin.
func (s *SessionService) InvalidateUserSessions(ctx context.Context, userID string) error {
	count, err := s.sessions.DeleteAllByUser(ctx, userID)
	if err != nil {
		return err
	}
	s.logger.Info("invalidated all user sessions", "user_id", userID, "count", count)
	return nil
}

// ListSessions devuelve las sesiones activas de UN solo usuario.
// Usado por el panel user-facing "Tus dispositivos" — distinto del
// surface admin "Now Playing" que lista playback sessions del server
// entero. Sorted newest-first por last_active.
func (s *SessionService) ListSessions(ctx context.Context, userID string) ([]*authmodel.Session, error) {
	rows, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RevokeSession borra una sesión sola si pertenece al caller.
// Devolver ErrNotFound para sesiones ajenas mantiene la surface
// anti-enumeration: un atacante probando session IDs de otros users
// obtiene la misma respuesta que para un ID que no existe.
func (s *SessionService) RevokeSession(ctx context.Context, userID, sessionID string) error {
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
	// Mismo payload shape que Logout para que el consumer SSE
	// (panel "Tus dispositivos") trate ambos paths uniformemente —
	// el panel invalida su query y la fila recién revocada cae.
	s.pub.publish(event.Event{
		Type: event.UserLoggedOut,
		Data: map[string]any{
			"user_id":    userID,
			"session_id": sessionID,
		},
	})
	return nil
}

// CurrentSessionID resuelve el session id del caller desde el
// refresh-token cookie. Devuelve "" cuando no matchea ningún row —
// la UI simplemente no marca "este dispositivo" en ese caso.
func (s *SessionService) CurrentSessionID(ctx context.Context, refreshToken string) string {
	if refreshToken == "" {
		return ""
	}
	row, err := s.sessions.GetByRefreshTokenHash(ctx, hashToken(refreshToken))
	if err != nil || row == nil {
		return ""
	}
	return row.ID
}

// Logout invalida una sesión por su refresh token. La búsqueda previa
// del row es para poder publicar `UserLoggedOut` con el user_id; un
// miss en la búsqueda no es fatal (el delete abajo es no-op si la
// sesión ya estaba borrada).
func (s *SessionService) Logout(ctx context.Context, refreshToken string) error {
	tokenHash := hashToken(refreshToken)
	session, lookupErr := s.sessions.GetByRefreshTokenHash(ctx, tokenHash)
	if err := s.sessions.DeleteByRefreshTokenHash(ctx, tokenHash); err != nil {
		return err
	}
	if lookupErr == nil && session != nil {
		s.pub.publish(event.Event{
			Type: event.UserLoggedOut,
			Data: map[string]any{
				"user_id":    session.UserID,
				"session_id": session.ID,
			},
		})
	}
	return nil
}
