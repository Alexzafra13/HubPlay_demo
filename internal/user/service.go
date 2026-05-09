package user

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

func (s *Service) GetByID(ctx context.Context, id string) (*db.User, error) {
	return s.users.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*db.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.users.List(ctx, limit, offset)
}

func (s *Service) Update(ctx context.Context, u *db.User) error {
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

// Count returns the total number of users. Used during setup to check if
// the first admin account needs to be created.
func (s *Service) Count(ctx context.Context) (int, error) {
	return s.users.Count(ctx)
}

// SetMaxContentRating updates a user's per-profile content cap.
// Empty string clears the cap (= no restriction).
func (s *Service) SetMaxContentRating(ctx context.Context, id, rating string) error {
	if err := s.users.SetMaxContentRating(ctx, id, rating); err != nil {
		return fmt.Errorf("set max content rating: %w", err)
	}
	s.logger.Info("max content rating set", "user_id", id, "rating", rating)
	return nil
}

// allowedAvatarHexes mirrors the 14-entry palette in
// `web/src/utils/avatarColor.ts`. Service-side validation so a
// stray frontend can't write an arbitrary hex (or worse, a CSS
// expression) into the column. Empty string is also valid — it
// clears the override, falling back to the deterministic helper.
//
// Keep in lock-step with the frontend palette: when one changes,
// the other must too. We accept this slight duplication because
// the alternative (serving the palette from a backend endpoint
// the frontend fetches) adds an HTTP round-trip on every Settings
// load for a list of 14 strings that move twice a year.
var allowedAvatarHexes = map[string]struct{}{
	"#3d5a40": {}, "#7a3d2e": {}, "#1e3252": {}, "#5c3d6e": {},
	"#2e5c5a": {}, "#7a5c2e": {}, "#5a3d3d": {}, "#3d4a5c": {},
	"#6e3d5c": {}, "#3d6e6e": {}, "#5c4a2e": {}, "#4a2e5c": {},
	"#2e4a5c": {}, "#5c5c2e": {},
}

// SetAvatarColor updates the per-user avatar colour override.
// Empty string clears the override → frontend falls back to the
// FNV-1a → palette helper. Non-empty must be one of the 14 known
// palette entries; anything else is a 400.
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

// SetDisplayName renames the user. The username + parent_user_id
// stay put — this only changes the human label that the picker
// and the admin table show.
//
// Validation lives at the boundary (length 1..64, no leading/
// trailing whitespace) instead of at the repo to keep the SQL
// column free of opinions; downstream callers that already have a
// trusted name can write it directly via the repo.
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

// SetRole promotes / demotes a user between "user" and "admin". The
// handler is responsible for the primary-admin gate.
func (s *Service) SetRole(ctx context.Context, id, role string) error {
	if err := s.users.SetRole(ctx, id, role); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	s.logger.Info("user role changed", "user_id", id, "role", role)
	return nil
}

// SetActive flips the is_active flag. False = login rejected, JWT
// middleware rejects subsequent requests; row stays in the DB so
// flipping back true restores everything.
func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	if err := s.users.SetActive(ctx, id, active); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	s.logger.Info("user active state changed", "user_id", id, "active", active)
	return nil
}

// PrimaryAdminID returns the oldest admin's id. Used by the admin
// users table to disable destructive actions on the bootstrap admin.
func (s *Service) PrimaryAdminID(ctx context.Context) (string, error) {
	return s.users.PrimaryAdminID(ctx)
}

// SetAccessExpiresAt sets / clears the temporary-access deadline.
// Pass nil for permanent. The auth service is the consumer: Login
// + middleware reject after this stamp.
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
