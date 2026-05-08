package user

import (
	"context"
	"fmt"
	"log/slog"

	"hubplay/internal/db"
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
