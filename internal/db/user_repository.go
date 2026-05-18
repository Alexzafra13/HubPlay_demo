package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db/sqlc"
	"hubplay/internal/db/sqlc_pg"
	"hubplay/internal/domain"
)

// UserRepository — dual-dialect repo using Pattern A (dual q
// pointers, branching per method). Exactly one of sq / pq is non-nil
// after construction, picked from the driver string.
type UserRepository struct {
	db *sql.DB // kept for ListProfilesForOwner (sqlc 1.31.x parser bug)
	sq *sqlc.Queries
	pq *sqlc_pg.Queries
}

// NewUserRepository wires the repo against the chosen backend.
// "postgres" → sqlc_pg; anything else → sqlc (SQLite default).
func NewUserRepository(driver string, database *sql.DB) *UserRepository {
	r := &UserRepository{db: database}
	if IsPostgres(driver) {
		r.pq = sqlc_pg.New(database)
	} else {
		r.sq = sqlc.New(database)
	}
	return r
}

// useSQLite reports whether the SQLite branch is active. Local
// helper to keep each method's branching one-liner readable.
func (r *UserRepository) useSQLite() bool { return r.sq != nil }

func (r *UserRepository) GetByID(ctx context.Context, id string) (*authmodel.User, error) {
	if r.useSQLite() {
		row, err := r.sq.GetUserByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get user %s: %w", id, err)
		}
		u := userFromSqliteGetRow(row)
		return &u, nil
	}
	row, err := r.pq.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", id, err)
	}
	u := userFromPgGetRow(row)
	return &u, nil
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*authmodel.User, error) {
	if r.useSQLite() {
		row, err := r.sq.GetUserByUsername(ctx, username)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q: %w", username, domain.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get user by username %q: %w", username, err)
		}
		u := userFromSqliteGetByUsernameRow(row)
		return &u, nil
	}
	row, err := r.pq.GetUserByUsername(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("user %q: %w", username, domain.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get user by username %q: %w", username, err)
	}
	u := userFromPgGetByUsernameRow(row)
	return &u, nil
}

func (r *UserRepository) Create(ctx context.Context, u *authmodel.User) error {
	if r.useSQLite() {
		if err := r.sq.CreateUser(ctx, sqlc.CreateUserParams{
			ID:                     u.ID,
			Username:               u.Username,
			DisplayName:            u.DisplayName,
			PasswordHash:           u.PasswordHash,
			Role:                   u.Role,
			CreatedAt:              u.CreatedAt,
			ParentUserID:           nullStringFromOptional(u.ParentUserID),
			PasswordChangeRequired: u.PasswordChangeRequired,
		}); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		return nil
	}
	if err := r.pq.CreateUser(ctx, sqlc_pg.CreateUserParams{
		ID:                     u.ID,
		Username:               u.Username,
		DisplayName:            u.DisplayName,
		PasswordHash:           u.PasswordHash,
		Role:                   u.Role,
		CreatedAt:              u.CreatedAt,
		ParentUserID:           nullStringFromOptional(u.ParentUserID),
		PasswordChangeRequired: u.PasswordChangeRequired,
	}); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *UserRepository) SetPassword(ctx context.Context, id, hash string, mustChange bool) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
			ID:                     id,
			PasswordHash:           hash,
			PasswordChangeRequired: mustChange,
		}); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserPassword(ctx, sqlc_pg.UpdateUserPasswordParams{
		ID:                     id,
		PasswordHash:           hash,
		PasswordChangeRequired: mustChange,
	}); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

func (r *UserRepository) SetPIN(ctx context.Context, id, hash string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserPIN(ctx, sqlc.UpdateUserPINParams{
			ID:      id,
			PinHash: nullStringFromOptional(hash),
		}); err != nil {
			return fmt.Errorf("set pin: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserPIN(ctx, sqlc_pg.UpdateUserPINParams{
		ID:      id,
		PinHash: nullStringFromOptional(hash),
	}); err != nil {
		return fmt.Errorf("set pin: %w", err)
	}
	return nil
}

func (r *UserRepository) SetAvatarColor(ctx context.Context, id, hex string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserAvatarColor(ctx, sqlc.UpdateUserAvatarColorParams{
			ID:          id,
			AvatarColor: nullStringFromOptional(hex),
		}); err != nil {
			return fmt.Errorf("set avatar color: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserAvatarColor(ctx, sqlc_pg.UpdateUserAvatarColorParams{
		ID:          id,
		AvatarColor: nullStringFromOptional(hex),
	}); err != nil {
		return fmt.Errorf("set avatar color: %w", err)
	}
	return nil
}

// SetAvatarPath: ruta relativa al directorio de avatares; el service
// la calcula tras escribir el fichero en disco. Vacío → ClearAvatarPath
// (preferimos esa API explícita para que el caller exprese intención).
func (r *UserRepository) SetAvatarPath(ctx context.Context, id, path string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserAvatarPath(ctx, sqlc.UpdateUserAvatarPathParams{
			ID:         id,
			AvatarPath: nullStringFromOptional(path),
		}); err != nil {
			return fmt.Errorf("set avatar path: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserAvatarPath(ctx, sqlc_pg.UpdateUserAvatarPathParams{
		ID:         id,
		AvatarPath: nullStringFromOptional(path),
	}); err != nil {
		return fmt.Errorf("set avatar path: %w", err)
	}
	return nil
}

// ClearAvatarPath: pone avatar_path = NULL. El service borra el
// fichero de disco antes (best-effort) y luego llama aquí.
func (r *UserRepository) ClearAvatarPath(ctx context.Context, id string) error {
	if r.useSQLite() {
		if err := r.sq.ClearUserAvatarPath(ctx, id); err != nil {
			return fmt.Errorf("clear avatar path: %w", err)
		}
		return nil
	}
	if err := r.pq.ClearUserAvatarPath(ctx, id); err != nil {
		return fmt.Errorf("clear avatar path: %w", err)
	}
	return nil
}

func (r *UserRepository) SetDisplayName(ctx context.Context, id, name string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserDisplayName(ctx, sqlc.UpdateUserDisplayNameParams{
			ID:          id,
			DisplayName: name,
		}); err != nil {
			return fmt.Errorf("set display name: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserDisplayName(ctx, sqlc_pg.UpdateUserDisplayNameParams{
		ID:          id,
		DisplayName: name,
	}); err != nil {
		return fmt.Errorf("set display name: %w", err)
	}
	return nil
}

func (r *UserRepository) SetMaxContentRating(ctx context.Context, id, rating string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserMaxContentRating(ctx, sqlc.UpdateUserMaxContentRatingParams{
			ID:               id,
			MaxContentRating: nullStringFromOptional(rating),
		}); err != nil {
			return fmt.Errorf("set content rating: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserMaxContentRating(ctx, sqlc_pg.UpdateUserMaxContentRatingParams{
		ID:               id,
		MaxContentRating: nullStringFromOptional(rating),
	}); err != nil {
		return fmt.Errorf("set content rating: %w", err)
	}
	return nil
}

func (r *UserRepository) SetRole(ctx context.Context, id, role string) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserRole(ctx, sqlc.UpdateUserRoleParams{
			ID:   id,
			Role: role,
		}); err != nil {
			return fmt.Errorf("set role: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserRole(ctx, sqlc_pg.UpdateUserRoleParams{
		ID:   id,
		Role: role,
	}); err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	return nil
}

func (r *UserRepository) SetActive(ctx context.Context, id string, active bool) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUserActive(ctx, sqlc.UpdateUserActiveParams{
			ID:       id,
			IsActive: active,
		}); err != nil {
			return fmt.Errorf("set active: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserActive(ctx, sqlc_pg.UpdateUserActiveParams{
		ID:       id,
		IsActive: active,
	}); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	return nil
}

func (r *UserRepository) SetAccessExpiresAt(ctx context.Context, id string, expiresAt *time.Time) error {
	var nt sql.NullTime
	if expiresAt != nil {
		nt = sql.NullTime{Time: expiresAt.UTC(), Valid: true}
	}
	if r.useSQLite() {
		if err := r.sq.UpdateUserAccessExpiresAt(ctx, sqlc.UpdateUserAccessExpiresAtParams{
			ID:              id,
			AccessExpiresAt: nt,
		}); err != nil {
			return fmt.Errorf("set access expires at: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUserAccessExpiresAt(ctx, sqlc_pg.UpdateUserAccessExpiresAtParams{
		ID:              id,
		AccessExpiresAt: nt,
	}); err != nil {
		return fmt.Errorf("set access expires at: %w", err)
	}
	return nil
}

func (r *UserRepository) PrimaryAdminID(ctx context.Context) (string, error) {
	var (
		id  string
		err error
	)
	if r.useSQLite() {
		id, err = r.sq.GetPrimaryAdminID(ctx)
	} else {
		id, err = r.pq.GetPrimaryAdminID(ctx)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("primary admin id: %w", err)
	}
	return id, nil
}

// ListAdminIDs devuelve los IDs de todos los admins activos (role=admin
// y sin parent_user_id, es decir titulares de hogar, no perfiles). Lo
// usa el servicio de notificaciones para hacer fan-out a todos los
// admins cuando una notificacion es admin-target (e.g. ha entrado una
// pairing request federation).
func (r *UserRepository) ListAdminIDs(ctx context.Context) ([]string, error) {
	if r.useSQLite() {
		ids, err := r.sq.ListAdminIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("list admin ids: %w", err)
		}
		return ids, nil
	}
	ids, err := r.pq.ListAdminIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin ids: %w", err)
	}
	return ids, nil
}

// ListProfilesForOwner — raw SQL holdout. See the original SQLite-only
// version's long comment about the sqlc 1.31.x parser bug. The query
// is dialect-aware via rewritePlaceholders.
func (r *UserRepository) ListProfilesForOwner(ctx context.Context, ownerID string) ([]*authmodel.User, error) {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver, `
SELECT id, username, display_name, COALESCE(avatar_path, '') AS avatar_path,
       role, is_active, created_at, last_login_at,
       parent_user_id, pin_hash, max_content_rating, password_change_required,
       access_expires_at, avatar_color
FROM users
WHERE id = ? OR parent_user_id = ?
ORDER BY parent_user_id IS NOT NULL, LOWER(display_name)`)

	rows, err := r.db.QueryContext(ctx, query, ownerID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list profiles for owner %s: %w", ownerID, err)
	}
	defer rows.Close() //nolint:errcheck

	var out []*authmodel.User
	for rows.Next() {
		var u authmodel.User
		var avatarPath string
		var lastLoginAt, accessExpiresAt sql.NullTime
		var parentUserID, pinHash, maxContentRating, avatarColor sql.NullString
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.DisplayName,
			&avatarPath,
			&u.Role,
			&u.IsActive,
			&u.CreatedAt,
			&lastLoginAt,
			&parentUserID,
			&pinHash,
			&maxContentRating,
			&u.PasswordChangeRequired,
			&accessExpiresAt,
			&avatarColor,
		); err != nil {
			return nil, fmt.Errorf("scan profile row: %w", err)
		}
		u.AvatarPath = avatarPath
		u.LastLoginAt = nullTimeToPtr(lastLoginAt)
		u.ParentUserID = parentUserID.String
		u.PINHash = pinHash.String
		u.MaxContentRating = maxContentRating.String
		u.AccessExpiresAt = nullTimeToPtr(accessExpiresAt)
		u.AvatarColor = avatarColor.String
		out = append(out, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile rows: %w", err)
	}
	return out, nil
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	nt := sql.NullTime{Time: t, Valid: true}
	if r.useSQLite() {
		if err := r.sq.UpdateLastLogin(ctx, sqlc.UpdateLastLoginParams{
			LastLoginAt: nt, ID: id,
		}); err != nil {
			return fmt.Errorf("update last login: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateLastLogin(ctx, sqlc_pg.UpdateLastLoginParams{
		LastLoginAt: nt, ID: id,
	}); err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]*authmodel.User, int, error) {
	if r.useSQLite() {
		cnt, err := r.sq.CountUsers(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("count users: %w", err)
		}
		rows, err := r.sq.ListUsers(ctx, sqlc.ListUsersParams{
			Limit: int64(limit), Offset: int64(offset),
		})
		if err != nil {
			return nil, 0, fmt.Errorf("list users: %w", err)
		}
		out := make([]*authmodel.User, len(rows))
		for i, row := range rows {
			u := userFromSqliteListRow(row)
			out[i] = &u
		}
		return out, int(cnt), nil
	}
	cnt, err := r.pq.CountUsers(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	// sqlc generates int32 for Postgres LIMIT/OFFSET (vs int64 for
	// SQLite) — the underlying SQL standard maps Postgres BIGINT
	// differently. Cast at the call site rather than trying to
	// override the type globally in sqlc.yaml.
	rows, err := r.pq.ListUsers(ctx, sqlc_pg.ListUsersParams{
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	out := make([]*authmodel.User, len(rows))
	for i, row := range rows {
		u := userFromPgListRow(row)
		out[i] = &u
	}
	return out, int(cnt), nil
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var (
		cnt int64
		err error
	)
	if r.useSQLite() {
		cnt, err = r.sq.CountUsers(ctx)
	} else {
		cnt, err = r.pq.CountUsers(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return int(cnt), nil
}

func (r *UserRepository) Update(ctx context.Context, u *authmodel.User) error {
	if r.useSQLite() {
		if err := r.sq.UpdateUser(ctx, sqlc.UpdateUserParams{
			DisplayName: u.DisplayName,
			Role:        u.Role,
			IsActive:    u.IsActive,
			ID:          u.ID,
		}); err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		return nil
	}
	if err := r.pq.UpdateUser(ctx, sqlc_pg.UpdateUserParams{
		DisplayName: u.DisplayName,
		Role:        u.Role,
		IsActive:    u.IsActive,
		ID:          u.ID,
	}); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	var (
		n   int64
		err error
	)
	if r.useSQLite() {
		n, err = r.sq.DeleteUser(ctx, id)
	} else {
		n, err = r.pq.DeleteUser(ctx, id)
	}
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
// any other value → valid.
func nullStringFromOptional(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func userFromSqliteGetRow(r sqlc.GetUserByIDRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}

func userFromSqliteGetByUsernameRow(r sqlc.GetUserByUsernameRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}

func userFromSqliteListRow(r sqlc.ListUsersRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}

func userFromPgGetRow(r sqlc_pg.GetUserByIDRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}

func userFromPgGetByUsernameRow(r sqlc_pg.GetUserByUsernameRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}

func userFromPgListRow(r sqlc_pg.ListUsersRow) authmodel.User {
	return authmodel.User{
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
		AvatarColor:            r.AvatarColor.String,
	}
}
