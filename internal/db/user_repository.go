package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	db *sql.DB
}

func NewUserRepository(database *sql.DB) *UserRepository {
	return &UserRepository{db: database}
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	u := &User{}
	var lastLogin sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, COALESCE(avatar_path,''),
		        role, is_active, max_sessions, created_at, last_login_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarPath,
		&u.Role, &u.IsActive, &u.MaxSessions, &u.CreatedAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", id, err)
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return u, nil
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	var lastLogin sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, COALESCE(avatar_path,''),
		        role, is_active, max_sessions, created_at, last_login_at
		 FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarPath,
		&u.Role, &u.IsActive, &u.MaxSessions, &u.CreatedAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user %q: %w", username, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username %q: %w", username, err)
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return u, nil
}

func (r *UserRepository) Create(ctx context.Context, u *User) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, username, display_name, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.PasswordHash, u.Role, u.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, t, id)
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]*User, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, display_name, COALESCE(avatar_path,''), role, is_active, created_at, last_login_at
		 FROM users ORDER BY username LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		var lastLogin sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarPath,
			&u.Role, &u.IsActive, &u.CreatedAt, &lastLogin); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		if lastLogin.Valid {
			u.LastLoginAt = &lastLogin.Time
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (r *UserRepository) Update(ctx context.Context, u *User) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET display_name = ?, role = ?, is_active = ? WHERE id = ?`,
		u.DisplayName, u.Role, u.IsActive, u.ID,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	return nil
}
