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
	// PreviousRefreshTokenHash is the previous-generation hash kept
	// for one-step refresh-token reuse detection. A refresh request
	// that hits this column instead of RefreshTokenHash means an
	// already-rotated token came back through the door — either an
	// attacker is replaying a leak or a legitimate client lost its
	// last response. Either way the safe response is to revoke the
	// whole session and force a fresh login. Empty string when no
	// rotation has happened yet (first refresh after login).
	PreviousRefreshTokenHash string
	CreatedAt                time.Time
	LastActiveAt             time.Time
	ExpiresAt                time.Time
}

type SessionRepository struct {
	db *sql.DB // kept for RotateRefreshToken (sqlc 1.31.x parser bug)
	q  *sqlc.Queries
}

func NewSessionRepository(database *sql.DB) *SessionRepository {
	return &SessionRepository{db: database, q: sqlc.New(database)}
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

func (r *SessionRepository) GetByID(ctx context.Context, id string) (*Session, error) {
	row, err := r.q.GetSessionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session: %w", domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session by id: %w", err)
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

// RotateRefreshToken atomically replaces the stored refresh-token
// hash, copies the previous hash into previous_refresh_token_hash
// for one-step reuse detection, bumps last_active_at, and pushes
// expires_at out by another full TTL. Caller is responsible for
// ensuring `newHash` was minted from a freshly generated refresh
// token.
//
// The `previous_refresh_token_hash = refresh_token_hash` clause runs
// in the same UPDATE as the new hash assignment so the atomicity
// holds: there's no window where a refresh request can see one
// column rotated and the other stale.
//
// Hand-rolled (not sqlc-generated) because sqlc 1.31.x truncates
// UPDATEs with 4+ placeholders. See the same dodge for
// ListProfilesForOwner.
func (r *SessionRepository) RotateRefreshToken(ctx context.Context, id, newHash string, lastActive, expiresAt time.Time) error {
	const query = `UPDATE sessions SET previous_refresh_token_hash = refresh_token_hash, refresh_token_hash = ?, last_active_at = ?, expires_at = ? WHERE id = ?`
	if _, err := r.db.ExecContext(ctx, query, newHash, lastActive, expiresAt, id); err != nil {
		return fmt.Errorf("rotate session refresh token: %w", err)
	}
	return nil
}

// GetByPreviousRefreshTokenHash returns the session whose previous
// (i.e. just-rotated) refresh-token hash matches. Used by /auth/refresh
// to spot the "already rotated, but the old token came back" case
// that signals reuse.
//
// Hand-rolled because the previous_refresh_token_hash column was
// added in migration 038 and pulling it through sqlc would require a
// new query that risks tripping the same parser bug that already
// forced the other holdouts off codegen. The lookup is a single
// indexed seek (idx_sessions_previous_refresh_hash).
//
// Returns domain.ErrNotFound when no row matches; the caller treats
// that as "this token was never anybody's previous-generation hash"
// — i.e. a normal invalid token.
func (r *SessionRepository) GetByPreviousRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	if hash == "" {
		// Empty string is the column default for never-rotated rows;
		// matching against it would return every session that has
		// not yet been refreshed. That's never a reuse signal.
		return nil, domain.ErrNotFound
	}
	const query = `SELECT id, user_id, device_name, device_id, ip_address, refresh_token_hash, previous_refresh_token_hash, created_at, last_active_at, expires_at FROM sessions WHERE previous_refresh_token_hash = ? LIMIT 1`
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

// nullableString wraps a plain string for storage in a nullable TEXT column.
// An empty string is stored as NULL, matching the column's nullable semantics
// ("" means "unknown" in the domain, and NULL is its SQL equivalent).
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
