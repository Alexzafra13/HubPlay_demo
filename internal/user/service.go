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
