package user

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

type Service struct {
	users  *db.UserRepository
	logger *slog.Logger
}

func NewService(users *db.UserRepository, logger *slog.Logger) *Service {
	return &Service{
		users:  users,
		logger: logger.With("module", "user"),
	}
}

func (s *Service) GetByID(ctx context.Context, id string) (*authmodel.User, error) {
	return s.users.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.users.List(ctx, limit, offset)
}

func (s *Service) Update(ctx context.Context, u *authmodel.User) error {
	if err := s.users.Update(ctx, u); err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	s.logger.Info("user updated", "user_id", u.ID)
	return nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.users.Delete(ctx, id); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	s.logger.Info("user deleted", "user_id", id)
	return nil
}

// Count: lo usa el wizard de setup para saber si toca crear el primer admin.
func (s *Service) Count(ctx context.Context) (int, error) {
	return s.users.Count(ctx)
}

// SetMaxContentRating: cap de contenido por perfil. "" = sin restricción.
func (s *Service) SetMaxContentRating(ctx context.Context, id, rating string) error {
	if err := s.users.SetMaxContentRating(ctx, id, rating); err != nil {
		return fmt.Errorf("set max content rating: %w", err)
	}
	s.logger.Info("max content rating set", "user_id", id, "rating", rating)
	return nil
}

// allowedAvatarHexes: paleta de 8 entradas replicada en web/src/utils/avatarColor.ts.
// Validación server-side para que un frontend rogue no escriba hex arbitrario.
// Reducida desde 14 a 8 colores claramente distintos (antes había pares casi
// idénticos como moss/olive, terracotta/garnet, navy/slate/petrol) para que el
// picker ofrezca opciones que se distinguen de un vistazo en lugar de variantes
// del mismo tono. Mantener en lock-step con el frontend.
var allowedAvatarHexes = map[string]struct{}{
	"#b91c1c": {}, // rojo
	"#c2410c": {}, // naranja
	"#a16207": {}, // ámbar
	"#15803d": {}, // verde
	"#0f766e": {}, // turquesa
	"#1d4ed8": {}, // azul
	"#6d28d9": {}, // violeta
	"#be185d": {}, // rosa
}

// SetAvatarColor: "" = limpia override (frontend cae al helper FNV-1a → paleta).
// Cualquier hex fuera de las 14 entradas conocidas es 400.
func (s *Service) SetAvatarColor(ctx context.Context, id, hex string) error {
	trimmed := strings.TrimSpace(strings.ToLower(hex))
	if trimmed != "" {
		if _, ok := allowedAvatarHexes[trimmed]; !ok {
			return domain.NewValidationError(map[string]string{
				"avatar_color": "must be empty or one of the known palette colours",
			})
		}
	}
	if err := s.users.SetAvatarColor(ctx, id, trimmed); err != nil {
		return fmt.Errorf("set avatar color: %w", err)
	}
	s.logger.Info("avatar color updated", "user_id", id, "color", trimmed)
	return nil
}

// SetDisplayName: sólo cambia la etiqueta humana; username + parent_user_id intactos.
// Validación en el service (1..64 sin whitespace) y no en el repo, así callers
// confiables pueden escribir directo sin pasar por la validación.
func (s *Service) SetDisplayName(ctx context.Context, id, name string) error {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) == 0 || len(trimmed) > 64 {
		return domain.NewValidationError(map[string]string{
			"display_name": "must be 1-64 characters",
		})
	}
	if err := s.users.SetDisplayName(ctx, id, trimmed); err != nil {
		return fmt.Errorf("set display name: %w", err)
	}
	s.logger.Info("display name updated", "user_id", id)
	return nil
}

// SetRole: promueve/degrada entre "user" y "admin". El gate del primary-admin
// vive en el handler, no aquí.
func (s *Service) SetRole(ctx context.Context, id, role string) error {
	if err := s.users.SetRole(ctx, id, role); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	s.logger.Info("user role changed", "user_id", id, "role", role)
	return nil
}

// SetActive: false → login rechazado y middleware JWT rechaza requests siguientes.
// Row no se borra: re-activar restaura todo.
func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	if err := s.users.SetActive(ctx, id, active); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	s.logger.Info("user active state changed", "user_id", id, "active", active)
	return nil
}

// PrimaryAdminID: id del admin más antiguo. El admin UI lo usa para bloquear
// acciones destructivas contra el bootstrap admin.
func (s *Service) PrimaryAdminID(ctx context.Context) (string, error) {
	return s.users.PrimaryAdminID(ctx)
}

// SetAccessExpiresAt: nil = acceso permanente. Login + middleware rechazan
// tras este stamp.
func (s *Service) SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error {
	if err := s.users.SetAccessExpiresAt(ctx, id, expiresAt); err != nil {
		return fmt.Errorf("set access expires at: %w", err)
	}
	if expiresAt == nil {
		s.logger.Info("user access set to permanent", "user_id", id)
	} else {
		s.logger.Info("user access window set", "user_id", id, "expires_at", *expiresAt)
	}
	return nil
}
