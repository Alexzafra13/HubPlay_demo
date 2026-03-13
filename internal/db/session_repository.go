package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hubplay/internal/domain"
)

type Session struct {
	ID               string
	UserID           string
	DeviceName       string
	DeviceID         string
	IPAddress        string
	RefreshTokenHash string
	CreatedAt        time.Time
	LastActiveAt     time.Time
	ExpiresAt        time.Time
}

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(database *sql.DB) *SessionRepository {
	return &SessionRepository{db: database}
}

func (r *SessionRepository) Create(ctx context.Context, s *Session) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, device_name, device_id, ip_address,
		 refresh_token_hash, created_at, last_active_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.DeviceName, s.DeviceID, s.IPAddress,
		s.RefreshTokenHash, s.CreatedAt, s.LastActiveAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) GetByRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	s := &Session{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, device_name, device_id, ip_address,
		 refresh_token_hash, created_at, last_active_at, expires_at
		 FROM sessions WHERE refresh_token_hash = ?`, hash,
	).Scan(&s.ID, &s.UserID, &s.DeviceName, &s.DeviceID, &s.IPAddress,
		&s.RefreshTokenHash, &s.CreatedAt, &s.LastActiveAt, &s.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

func (r *SessionRepository) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteByRefreshTokenHash(ctx context.Context, hash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE refresh_token_hash = ?`, hash)
	if err != nil {
		return fmt.Errorf("delete session by token: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListByUser(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, device_name, device_id, ip_address,
		 refresh_token_hash, created_at, last_active_at, expires_at
		 FROM sessions WHERE user_id = ? ORDER BY last_active_at DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.DeviceName, &s.DeviceID, &s.IPAddress,
			&s.RefreshTokenHash, &s.CreatedAt, &s.LastActiveAt, &s.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (r *SessionRepository) CountByUser(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return count, nil
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

func (r *SessionRepository) UpdateLastActive(ctx context.Context, id string, t time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET last_active_at = ? WHERE id = ?`, t, id)
	if err != nil {
		return fmt.Errorf("update session last active: %w", err)
	}
	return nil
}
