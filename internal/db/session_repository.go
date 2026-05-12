package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// Session is the domain shape exposed to auth/. IPAddress is a plain
// string (empty = "unknown"); conversion to/from nullable column
// happens at the adapter boundary.
type Session struct {
	ID                       string
	UserID                   string
	DeviceName               string
	DeviceID                 string
	IPAddress                string
	RefreshTokenHash         string
	PreviousRefreshTokenHash string
	CreatedAt                time.Time
	LastActiveAt             time.Time
	ExpiresAt                time.Time
}

// SessionRepository — Pattern A dual-dialect. db is kept for the two
// raw-SQL holdouts (RotateRefreshToken + GetByPreviousRefreshTokenHash)
// caused by the sqlc 1.31.x parser bug on multi-placeholder UPDATEs.
type SessionRepository struct {
	db *sql.DB
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

func NewSessionRepository(driver string, database *sql.DB) *SessionRepository {
	r := &SessionRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

func (r *SessionRepository) useSQLite() bool { return r.sq != nil }

func (r *SessionRepository) Create(ctx context.Context, s *Session) error {
	if r.useSQLite() {
		if err := r.sq.CreateSession(ctx, sqlc.CreateSessionParams{
			ID:               s.ID,
			UserID:           s.UserID,
			DeviceName:       s.DeviceName,
			DeviceID:         s.DeviceID,
			IpAddress:        nullableString(s.IPAddress),
			RefreshTokenHash: s.RefreshTokenHash,
			CreatedAt:        s.CreatedAt,
			LastActiveAt:     s.LastActiveAt,
			ExpiresAt:        s.ExpiresAt,
		}); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		return nil
	}
	if err := r.pq.CreateSession(ctx, sqlc_pg.CreateSessionParams{
		ID:               s.ID,
		UserID:           s.UserID,
		DeviceName:       s.DeviceName,
		DeviceID:         s.DeviceID,
		IpAddress:        nullableString(s.IPAddress),
		RefreshTokenHash: s.RefreshTokenHash,
		CreatedAt:        s.CreatedAt,
		LastActiveAt:     s.LastActiveAt,
		ExpiresAt:        s.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) GetByRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	if r.useSQLite() {
		row, err := r.sq.GetSessionByRefreshTokenHash(ctx, hash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get session: %w", err)
		}
		s := sessionFromSqliteRow(row)
		return &s, nil
	}
	row, err := r.pq.GetSessionByRefreshTokenHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	s := sessionFromPgRow(row)
	return &s, nil
}

func (r *SessionRepository) GetByID(ctx context.Context, id string) (*Session, error) {
	if r.useSQLite() {
		row, err := r.sq.GetSessionByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get session by id: %w", err)
		}
		s := sessionFromSqliteRow(row)
		return &s, nil
	}
	row, err := r.pq.GetSessionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
	}
	s := sessionFromPgRow(row)
	return &s, nil
}

func (r *SessionRepository) DeleteByID(ctx context.Context, id string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteSession(ctx, id)
	} else {
		err = r.pq.DeleteSession(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteByRefreshTokenHash(ctx context.Context, hash string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteSessionByRefreshTokenHash(ctx, hash)
	} else {
		err = r.pq.DeleteSessionByRefreshTokenHash(ctx, hash)
	}
	if err != nil {
		return fmt.Errorf("delete session by token: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListByUser(ctx context.Context, userID string) ([]*Session, error) {
	if r.useSQLite() {
		rows, err := r.sq.ListSessionsByUser(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		if len(rows) == 0 {
			return nil, nil
		}
		out := make([]*Session, len(rows))
		for i, row := range rows {
			s := sessionFromSqliteRow(row)
			out[i] = &s
		}
		return out, nil
	}
	rows, err := r.pq.ListSessionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*Session, len(rows))
	for i, row := range rows {
		s := sessionFromPgRow(row)
		out[i] = &s
	}
	return out, nil
}

func (r *SessionRepository) CountByUser(ctx context.Context, userID string) (int, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.CountSessionsByUser(ctx, userID)
	} else {
		n, err = r.pq.CountSessionsByUser(ctx, userID)
	}
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return int(n), nil
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteExpiredSessions(ctx)
	} else {
		n, err = r.pq.DeleteExpiredSessions(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return n, nil
}

func (r *SessionRepository) DeleteOldestByUser(ctx context.Context, userID string) error {
	var err error
	if r.useSQLite() {
		err = r.sq.DeleteOldestSessionByUser(ctx, userID)
	} else {
		err = r.pq.DeleteOldestSessionByUser(ctx, userID)
	}
	if err != nil {
		return fmt.Errorf("delete oldest session: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeleteAllByUser(ctx context.Context, userID string) (int64, error) {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteAllSessionsByUser(ctx, userID)
	} else {
		n, err = r.pq.DeleteAllSessionsByUser(ctx, userID)
	}
	if err != nil {
		return 0, fmt.Errorf("delete all user sessions: %w", err)
	}
	return n, nil
}

// RotateRefreshToken — raw SQL holdout (sqlc 1.31.x parser bug on
// UPDATEs with 4+ placeholders). Dialect-aware via rewritePlaceholders.
func (r *SessionRepository) RotateRefreshToken(ctx context.Context, id, newHash string, lastActive, expiresAt time.Time) error {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver,
		`UPDATE sessions SET previous_refresh_token_hash = refresh_token_hash, refresh_token_hash = ?, last_active_at = ?, expires_at = ? WHERE id = ?`)
	if _, err := r.db.ExecContext(ctx, query, newHash, lastActive, expiresAt, id); err != nil {
		return fmt.Errorf("rotate session refresh token: %w", err)
	}
	return nil
}

// GetByPreviousRefreshTokenHash — raw SQL holdout (column added in
// migration 038 post-dating most sqlc queries; matches the
// RotateRefreshToken precedent).
func (r *SessionRepository) GetByPreviousRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	if hash == "" {
		return nil, domain.ErrNotFound
	}
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver,
		`SELECT id, user_id, device_name, device_id, ip_address, refresh_token_hash, previous_refresh_token_hash, created_at, last_active_at, expires_at FROM sessions WHERE previous_refresh_token_hash = ? LIMIT 1`)
	var s Session
	var ip sql.NullString
	err := r.db.QueryRowContext(ctx, query, hash).Scan(
		&s.ID, &s.UserID, &s.DeviceName, &s.DeviceID, &ip,
		&s.RefreshTokenHash, &s.PreviousRefreshTokenHash,
		&s.CreatedAt, &s.LastActiveAt, &s.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup session by previous refresh hash: %w", err)
	}
	s.IPAddress = ip.String
	return &s, nil
}

func (r *SessionRepository) UpdateLastActive(ctx context.Context, id string, t time.Time) error {
	if r.useSQLite() {
		if err := r.sq.UpdateSessionLastActive(ctx, sqlc.UpdateSessionLastActiveParams{
			LastActiveAt: t, ID: id,
		}); err != nil {
			return fmt.Errorf("update session last active: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateSessionLastActive(ctx, sqlc_pg.UpdateSessionLastActiveParams{
		LastActiveAt: t, ID: id,
	}); err != nil {
		return fmt.Errorf("update session last active: %w", err)
	}
	return nil
}

// ── row mapping helpers ─────────────────────────────────────────────────

func sessionFromSqliteRow(r sqlc.Session) Session {
	return Session{
		ID:                       r.ID,
		UserID:                   r.UserID,
		DeviceName:               r.DeviceName,
		DeviceID:                 r.DeviceID,
		IPAddress:                r.IpAddress.String,
		RefreshTokenHash:         r.RefreshTokenHash,
		PreviousRefreshTokenHash: r.PreviousRefreshTokenHash,
		CreatedAt:                r.CreatedAt,
		LastActiveAt:             r.LastActiveAt,
		ExpiresAt:                r.ExpiresAt,
	}
}

func sessionFromPgRow(r sqlc_pg.Session) Session {
	return Session{
		ID:                       r.ID,
		UserID:                   r.UserID,
		DeviceName:               r.DeviceName,
		DeviceID:                 r.DeviceID,
		IPAddress:                r.IpAddress.String,
		RefreshTokenHash:         r.RefreshTokenHash,
		PreviousRefreshTokenHash: r.PreviousRefreshTokenHash,
		CreatedAt:                r.CreatedAt,
		LastActiveAt:             r.LastActiveAt,
		ExpiresAt:                r.ExpiresAt,
	}
}

// nullableString wraps a plain string for storage in a nullable TEXT
// column. Empty string → NULL, anything else → valid.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
