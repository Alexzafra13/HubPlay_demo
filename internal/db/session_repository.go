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

// Session is the domain shape exposed to auth/. It keeps IPAddress as a plain
// string (empty means "unknown") while the underlying column is nullable —
// the conversion happens at the adapter boundary below.
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
	q *sqlc.Queries
}

func NewSessionRepository(database *sql.DB) *SessionRepository {
	return &SessionRepository{q: sqlc.New(database)}
}

func (r *SessionRepository) Create(ctx context.Context, s *Session) error {
	err := r.q.CreateSession(ctx, sqlc.CreateSessionParams{
		ID:               s.ID,
		UserID:           s.UserID,
		DeviceName:       s.DeviceName,
		DeviceID:         s.DeviceID,
		IpAddress:        nullableString(s.IPAddress),
		RefreshTokenHash: s.RefreshTokenHash,
		CreatedAt:        s.CreatedAt,
		LastActiveAt:     s.LastActiveAt,
		ExpiresAt:        s.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) GetByRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	row, err := r.q.GetSessionByRefreshTokenHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	s := sessionFromRow(row)
	return &s, nil
}

func (r *SessionRepository) DeleteByID(ctx context.Context, id string) error {
	if err := r.q.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteByRefreshTokenHash(ctx context.Context, hash string) error {
	if err := r.q.DeleteSessionByRefreshTokenHash(ctx, hash); err != nil {
		return fmt.Errorf("delete session by token: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListByUser(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := r.q.ListSessionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*Session, len(rows))
	for i, row := range rows {
		s := sessionFromRow(row)
		out[i] = &s
	}
	return out, nil
}

func (r *SessionRepository) CountByUser(ctx context.Context, userID string) (int, error) {
	n, err := r.q.CountSessionsByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return int(n), nil
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	n, err := r.q.DeleteExpiredSessions(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return n, nil
}

// DeleteOldestByUser deletes the single oldest session for a user (by
// last_active_at, then created_at). No-op if the user has no sessions.
func (r *SessionRepository) DeleteOldestByUser(ctx context.Context, userID string) error {
	if err := r.q.DeleteOldestSessionByUser(ctx, userID); err != nil {
		return fmt.Errorf("delete oldest session: %w", err)
	}
	return nil
}

// DeleteAllByUser removes all sessions for a user (e.g. on password change).
func (r *SessionRepository) DeleteAllByUser(ctx context.Context, userID string) (int64, error) {
	n, err := r.q.DeleteAllSessionsByUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("delete all user sessions: %w", err)
	}
	return n, nil
}

func (r *SessionRepository) UpdateLastActive(ctx context.Context, id string, t time.Time) error {
	err := r.q.UpdateSessionLastActive(ctx, sqlc.UpdateSessionLastActiveParams{
		LastActiveAt: t,
		ID:           id,
	})
	if err != nil {
		return fmt.Errorf("update session last active: %w", err)
	}
	return nil
}

// sessionFromRow maps the sqlc row (IpAddress sql.NullString) to the domain
// Session (IPAddress string). Invalid → "".
func sessionFromRow(r sqlc.Session) Session {
	return Session{
		ID:               r.ID,
		UserID:           r.UserID,
		DeviceName:       r.DeviceName,
		DeviceID:         r.DeviceID,
		IPAddress:        r.IpAddress.String,
		RefreshTokenHash: r.RefreshTokenHash,
		CreatedAt:        r.CreatedAt,
		LastActiveAt:     r.LastActiveAt,
		ExpiresAt:        r.ExpiresAt,
	}
}

// nullableString wraps a plain string for storage in a nullable TEXT column.
// An empty string is stored as NULL, matching the column's nullable semantics
// ("" means "unknown" in the domain, and NULL is its SQL equivalent).
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
