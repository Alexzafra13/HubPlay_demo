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

	// Profile tree fields (migration 034). All four are zero-value
	// safe — pre-existing users get NULL/0 when the migration runs,
	// which means "top-level account, no PIN, no rating cap, no
	// forced password change".
	//
	// ParentUserID identifies the parent account when this row is a
	// child profile (Netflix-style). Empty/NULL marks an account
	// owner — the only kind of user that authenticates with a
	// password. Profiles share the parent's password and use
	// /auth/switch-profile to rotate JWTs.
	ParentUserID string
	// PINHash, when set, gates entry into this profile from the
	// "Who's watching?" selector. bcrypt-hashed; never returned over
	// the wire.
	PINHash string
	// MaxContentRating caps what the profile can browse. Empty =
	// "no restriction". Stored as the rating literal ("PG-13",
	// "TV-MA", ...); the filter consults a ranking table at query
	// time.
	MaxContentRating string
	// PasswordChangeRequired forces the next successful login to
	// land on the change-password screen before any other surface.
	// Set true on admin-driven create / reset; cleared automatically
	// when the user finishes a successful change.
	PasswordChangeRequired bool

	// AccessExpiresAt, when set, marks a temporary-access window.
	// Login + middleware reject after this timestamp; nil = no
	// expiry (permanent access). Lazy enforcement — there's no
	// background job that flips is_active automatically; the JWT
	// TTL bounds how long a stale token can outlive its expiry.
	AccessExpiresAt *time.Time
}

// IsProfile is the canonical readability helper around `ParentUserID`.
// Profiles can't authenticate directly, can't be admins, and can't
// own peers — every gate that matters consults this one method
// instead of duplicating the empty-string check at each callsite.
func (u User) IsProfile() bool { return u.ParentUserID != "" }

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
		ID:                     u.ID,
		Username:               u.Username,
		DisplayName:            u.DisplayName,
		PasswordHash:           u.PasswordHash,
		Role:                   u.Role,
		CreatedAt:              u.CreatedAt,
		ParentUserID:           nullStringFromOptional(u.ParentUserID),
		PasswordChangeRequired: u.PasswordChangeRequired,
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// SetPassword updates the user's password hash and the must-change
// flag in a single shot. The handler decides what `mustChange` should
// be: true after admin-driven reset (forces change on next login);
// false after the user themselves changed it.
func (r *UserRepository) SetPassword(ctx context.Context, id, hash string, mustChange bool) error {
	if err := r.q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:                     id,
		PasswordHash:           hash,
		PasswordChangeRequired: mustChange,
	}); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

// SetPIN stores (or clears, when hash is empty) the profile PIN.
// Plumbed through the auth/switch-profile handler.
func (r *UserRepository) SetPIN(ctx context.Context, id, hash string) error {
	if err := r.q.UpdateUserPIN(ctx, sqlc.UpdateUserPINParams{
		ID:      id,
		PinHash: nullStringFromOptional(hash),
	}); err != nil {
		return fmt.Errorf("set pin: %w", err)
	}
	return nil
}

// SetDisplayName renames the user (label only — username stays put,
// avatar colour derived from username also stays put). Trims to a
// reasonable max so a runaway paste doesn't fill the column.
func (r *UserRepository) SetDisplayName(ctx context.Context, id, name string) error {
	if err := r.q.UpdateUserDisplayName(ctx, sqlc.UpdateUserDisplayNameParams{
		ID:          id,
		DisplayName: name,
	}); err != nil {
		return fmt.Errorf("set display name: %w", err)
	}
	return nil
}

// SetMaxContentRating updates the per-profile rating cap. Empty
// rating clears the cap (= profile sees everything).
func (r *UserRepository) SetMaxContentRating(ctx context.Context, id, rating string) error {
	if err := r.q.UpdateUserMaxContentRating(ctx, sqlc.UpdateUserMaxContentRatingParams{
		ID:               id,
		MaxContentRating: nullStringFromOptional(rating),
	}); err != nil {
		return fmt.Errorf("set content rating: %w", err)
	}
	return nil
}

// SetRole flips the user between "user" and "admin". The handler
// gate stops the primary admin from being demoted; this method
// trusts the caller, by design.
func (r *UserRepository) SetRole(ctx context.Context, id, role string) error {
	if err := r.q.UpdateUserRole(ctx, sqlc.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	return nil
}

// SetActive soft-disables / re-enables a user. Login + middleware
// reject on is_active=false. The row and every per-user table stays
// intact — re-enabling restores access without a recovery flow.
func (r *UserRepository) SetActive(ctx context.Context, id string, active bool) error {
	if err := r.q.UpdateUserActive(ctx, sqlc.UpdateUserActiveParams{
		ID:       id,
		IsActive: active,
	}); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	return nil
}

// SetAccessExpiresAt updates the temporary-access deadline. Pass nil
// to clear (= permanent access). Lazy enforcement: Login +
// middleware compare against time.Now() so we never need a job to
// flip is_active automatically.
func (r *UserRepository) SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error {
	var nt sql.NullTime
	if expiresAt != nil {
		nt = sql.NullTime{Time: expiresAt.UTC(), Valid: true}
	}
	if err := r.q.UpdateUserAccessExpiresAt(ctx, sqlc.UpdateUserAccessExpiresAtParams{
		ID:              id,
		AccessExpiresAt: nt,
	}); err != nil {
		return fmt.Errorf("set access expires at: %w", err)
	}
	return nil
}

// PrimaryAdminID returns the oldest admin's user_id. Used to gate
// destructive actions on the primary admin row from the admin
// table. Empty string + nil error when no admin exists yet (cold-
// start, before setup wizard runs).
func (r *UserRepository) PrimaryAdminID(ctx context.Context) (string, error) {
	id, err := r.q.GetPrimaryAdminID(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("primary admin id: %w", err)
	}
	return id, nil
}

// ListProfilesForOwner returns the parent account row plus all child
// profiles in a single query. The handler maps each row to a User
// and the parent comes first by virtue of the `parent_user_id IS NOT
// NULL` ORDER BY trick in the SQL.
func (r *UserRepository) ListProfilesForOwner(ctx context.Context, ownerID string) ([]*User, error) {
	rows, err := r.q.ListProfilesForOwner(ctx, sqlc.ListProfilesForOwnerParams{
		ID:           ownerID,
		ParentUserID: sql.NullString{String: ownerID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles for owner %s: %w", ownerID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*User, len(rows))
	for i, row := range rows {
		u := userFromProfilesRow(row)
		out[i] = &u
	}
	return out, nil
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

// nullStringFromOptional bridges Go's "" sentinel for absent string
// fields to sqlc's sql.NullString. Empty string → invalid (NULL),
// any other value → valid. Keeps the call sites readable and
// matches how the rest of the repo already coerces optional strings.
func nullStringFromOptional(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func userFromGetRow(r sqlc.GetUserByIDRow) User {
	return User{
		ID:                     r.ID,
		Username:               r.Username,
		DisplayName:            r.DisplayName,
		PasswordHash:           r.PasswordHash,
		AvatarPath:             r.AvatarPath,
		Role:                   r.Role,
		IsActive:               r.IsActive,
		MaxSessions:            int(r.MaxSessions),
		CreatedAt:              r.CreatedAt,
		LastLoginAt:            nullTimeToPtr(r.LastLoginAt),
		ParentUserID:           r.ParentUserID.String,
		PINHash:                r.PinHash.String,
		MaxContentRating:       r.MaxContentRating.String,
		PasswordChangeRequired: r.PasswordChangeRequired,
		AccessExpiresAt:        nullTimeToPtr(r.AccessExpiresAt),
	}
}

func userFromGetByUsernameRow(r sqlc.GetUserByUsernameRow) User {
	return User{
		ID:                     r.ID,
		Username:               r.Username,
		DisplayName:            r.DisplayName,
		PasswordHash:           r.PasswordHash,
		AvatarPath:             r.AvatarPath,
		Role:                   r.Role,
		IsActive:               r.IsActive,
		MaxSessions:            int(r.MaxSessions),
		CreatedAt:              r.CreatedAt,
		LastLoginAt:            nullTimeToPtr(r.LastLoginAt),
		ParentUserID:           r.ParentUserID.String,
		PINHash:                r.PinHash.String,
		MaxContentRating:       r.MaxContentRating.String,
		PasswordChangeRequired: r.PasswordChangeRequired,
		AccessExpiresAt:        nullTimeToPtr(r.AccessExpiresAt),
	}
}

func userFromListRow(r sqlc.ListUsersRow) User {
	return User{
		ID:                     r.ID,
		Username:               r.Username,
		DisplayName:            r.DisplayName,
		AvatarPath:             r.AvatarPath,
		Role:                   r.Role,
		IsActive:               r.IsActive,
		CreatedAt:              r.CreatedAt,
		LastLoginAt:            nullTimeToPtr(r.LastLoginAt),
		ParentUserID:           r.ParentUserID.String,
		PINHash:                r.PinHash.String,
		MaxContentRating:       r.MaxContentRating.String,
		PasswordChangeRequired: r.PasswordChangeRequired,
		AccessExpiresAt:        nullTimeToPtr(r.AccessExpiresAt),
	}
}

func userFromProfilesRow(r sqlc.ListProfilesForOwnerRow) User {
	return User{
		ID:                     r.ID,
		Username:               r.Username,
		DisplayName:            r.DisplayName,
		AvatarPath:             r.AvatarPath,
		Role:                   r.Role,
		IsActive:               r.IsActive,
		CreatedAt:              r.CreatedAt,
		LastLoginAt:            nullTimeToPtr(r.LastLoginAt),
		ParentUserID:           r.ParentUserID.String,
		PINHash:                r.PinHash.String,
		MaxContentRating:       r.MaxContentRating.String,
		PasswordChangeRequired: r.PasswordChangeRequired,
		AccessExpiresAt:        nullTimeToPtr(r.AccessExpiresAt),
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
