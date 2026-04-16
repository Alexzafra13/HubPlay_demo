package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/domain"
)

type User struct {
	ID           string
	Username     string
	DisplayName  string
	PasswordHash string
	AvatarPath   string
	Role         string
	IsActive     bool
	MaxSessions  int
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

type UserRepository struct {
	q *sqlc.Queries
}

func NewUserRepository(database *sql.DB) *UserRepository {
	return &UserRepository{q: sqlc.New(database)}
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", id, err)
	}
	u := userFromGetRow(row)
	return &u, nil
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*User, error) {
	row, err := r.q.GetUserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %q: %w", username, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username %q: %w", username, err)
	}
	u := userFromGetByUsernameRow(row)
	return &u, nil
}

func (r *UserRepository) Create(ctx context.Context, u *User) error {
	err := r.q.CreateUser(ctx, sqlc.CreateUserParams{
		ID:           u.ID,
		Username:     u.Username,
		DisplayName:  u.DisplayName,
		PasswordHash: u.PasswordHash,
		Role:         u.Role,
		CreatedAt:    u.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	err := r.q.UpdateLastLogin(ctx, sqlc.UpdateLastLoginParams{
		LastLoginAt: sql.NullTime{Time: t, Valid: true},
		ID:          id,
	})
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]*User, int, error) {
	cnt, err := r.q.CountUsers(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := r.q.ListUsers(ctx, sqlc.ListUsersParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	return usersFromListRows(rows), int(cnt), nil
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	cnt, err := r.q.CountUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return int(cnt), nil
}

func (r *UserRepository) Update(ctx context.Context, u *User) error {
	err := r.q.UpdateUser(ctx, sqlc.UpdateUserParams{
		DisplayName: u.DisplayName,
		Role:        u.Role,
		IsActive:    u.IsActive,
		ID:          u.ID,
	})
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	n, err := r.q.DeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func nullTimeToPtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

func userFromGetRow(r sqlc.GetUserByIDRow) User {
	return User{
		ID:           r.ID,
		Username:     r.Username,
		DisplayName:  r.DisplayName,
		PasswordHash: r.PasswordHash,
		AvatarPath:   r.AvatarPath,
		Role:         r.Role,
		IsActive:     r.IsActive,
		MaxSessions:  int(r.MaxSessions),
		CreatedAt:    r.CreatedAt,
		LastLoginAt:  nullTimeToPtr(r.LastLoginAt),
	}
}

func userFromGetByUsernameRow(r sqlc.GetUserByUsernameRow) User {
	return User{
		ID:           r.ID,
		Username:     r.Username,
		DisplayName:  r.DisplayName,
		PasswordHash: r.PasswordHash,
		AvatarPath:   r.AvatarPath,
		Role:         r.Role,
		IsActive:     r.IsActive,
		MaxSessions:  int(r.MaxSessions),
		CreatedAt:    r.CreatedAt,
		LastLoginAt:  nullTimeToPtr(r.LastLoginAt),
	}
}

func userFromListRow(r sqlc.ListUsersRow) User {
	return User{
		ID:          r.ID,
		Username:    r.Username,
		DisplayName: r.DisplayName,
		AvatarPath:  r.AvatarPath,
		Role:        r.Role,
		IsActive:    r.IsActive,
		CreatedAt:   r.CreatedAt,
		LastLoginAt: nullTimeToPtr(r.LastLoginAt),
	}
}

func usersFromListRows(rows []sqlc.ListUsersRow) []*User {
	if len(rows) == 0 {
		return nil
	}
	out := make([]*User, len(rows))
	for i, row := range rows {
		u := userFromListRow(row)
		out[i] = &u
	}
	return out
}
