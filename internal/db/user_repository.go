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

// SetCanUpload flips the permission flag. The decision (who can,
// primary-admin gate, etc.) lives in the service.
//
// Raw SQL via rewritePlaceholders — see the long comment on
// ListProfilesForOwner for the sqlc 1.31.1 parser bug that forces
// hand-rolling the upload-permission mutations.
func (r *UserRepository) SetCanUpload(ctx context.Context, id string, canUpload bool) error {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver, `UPDATE users SET can_upload = ? WHERE id = ?`)
	// SQLite stores BOOLEAN as 0/1 INTEGER; modernc.org/sqlite handles
	// the bool→int marshal natively, so we don't need a coerce here.
	if _, err := r.db.ExecContext(ctx, query, canUpload, id); err != nil {
		return fmt.Errorf("set can upload: %w", err)
	}
	return nil
}

// SetUploadQuota fija el tope absoluto en bytes. El service valida
// >=0 y >= upload_used_bytes (no podemos bajar la cuota por debajo
// de lo ya ocupado — quedaría inconsistente).
func (r *UserRepository) SetUploadQuota(ctx context.Context, id string, quotaBytes int64) error {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	query := rewritePlaceholders(driver, `UPDATE users SET upload_quota_bytes = ? WHERE id = ?`)
	if _, err := r.db.ExecContext(ctx, query, quotaBytes, id); err != nil {
		return fmt.Errorf("set upload quota: %w", err)
	}
	return nil
}

// ReserveUploadBytes incrementa upload_used_bytes en `delta` si y
// solo si la fila cumple can_upload=true y (used+delta) <= quota.
// Devuelve domain.ErrUploadQuotaExceeded cuando la condición falla
// (la query ENFORCA todo en el WHERE — no hay window race entre un
// SELECT y un UPDATE).
//
// Caller responsibility: invocar ReleaseUploadBytes(delta) si el
// upload se aborta o falla downstream, para no dejar reservados
// bytes que nunca se materializaron en disco.
//
// El comparador `can_upload = 1` aquí asume SQLite-style 0/1; en
// Postgres BOOLEAN se compara igual con 1 vía TRUE → 1 via implicit
// cast? No — Postgres es estricto. Branch por driver.
func (r *UserRepository) ReserveUploadBytes(ctx context.Context, id string, delta int64) error {
	var query string
	if r.useSQLite() {
		query = `UPDATE users
			SET upload_used_bytes = upload_used_bytes + ?
			WHERE id = ?
			  AND can_upload = 1
			  AND upload_used_bytes + ? <= upload_quota_bytes`
	} else {
		query = rewritePlaceholders(DriverPostgres, `UPDATE users
			SET upload_used_bytes = upload_used_bytes + ?
			WHERE id = ?
			  AND can_upload = TRUE
			  AND upload_used_bytes + ? <= upload_quota_bytes`)
	}
	res, err := r.db.ExecContext(ctx, query, delta, id, delta)
	if err != nil {
		return fmt.Errorf("reserve upload bytes: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reserve upload bytes (rows affected): %w", err)
	}
	if rows == 0 {
		// Sentinel sin envoltorio rico para que callers que solo
		// quieren distinguir quota-exceeded de errores reales
		// funcionen con errors.Is. El service re-envuelve con
		// NewUploadQuotaExceeded antes de exponerlo al HTTP layer.
		return domain.ErrUploadQuotaExceeded
	}
	return nil
}

// ReleaseUploadBytes decrementa la contabilidad cuando un upload se
// cancela, falla la validación post-bytes, o se borra. El cap a 0
// vive en la query (MAX en SQLite, GREATEST en Postgres) para que
// sea robusto frente a llamadas duplicadas o un drift contable.
func (r *UserRepository) ReleaseUploadBytes(ctx context.Context, id string, delta int64) error {
	if delta <= 0 {
		return nil // no-op: liberar 0 o negativo es contrato silencioso
	}
	var query string
	if r.useSQLite() {
		query = `UPDATE users SET upload_used_bytes = MAX(upload_used_bytes - ?, 0) WHERE id = ?`
	} else {
		query = rewritePlaceholders(DriverPostgres,
			`UPDATE users SET upload_used_bytes = GREATEST(upload_used_bytes - ?, 0) WHERE id = ?`)
	}
	if _, err := r.db.ExecContext(ctx, query, delta, id); err != nil {
		return fmt.Errorf("release upload bytes: %w", err)
	}
	return nil
}

// ─── Admin permissions (migración 055) ──────────────────────────────
//
// Raw SQL por la misma razón que las mutaciones de upload: sqlc 1.31.1
// trunca queries con whitelists largas + WHERE compuestos como el de
// TransferOwnership. Las superficies son pocas y estables, así que el
// coste de mantenimiento es bajo.

// validPermissionColumn: whitelist de columnas que SetPermission puede
// tocar. Definida aquí (en el repo) para que ningún caller pueda meter
// SQL via el campo `permission` — incluso aunque el handler validara,
// la defensa en profundidad es barata.
var validPermissionColumn = map[string]struct{}{
	"can_manage_admins":    {},
	"can_manage_users":     {},
	"can_manage_libraries": {},
	"can_manage_iptv":      {},
	"can_edit_metadata":    {},
	"can_change_artwork":   {},
	"can_view_audit":       {},
	"can_upload":           {},
}

// SetPermission flipea un flag granular. column DEBE estar en el
// whitelist; cualquier otro valor devuelve error (defense in depth
// — el handler valida también, pero queremos doble candado).
//
// is_owner NO es modificable por esta función — la transferencia va
// por TransferOwnership que es atómica y respeta la unicidad.
func (r *UserRepository) SetPermission(ctx context.Context, id, column string, value bool) error {
	if _, ok := validPermissionColumn[column]; !ok {
		return fmt.Errorf("invalid permission column %q", column)
	}
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	// Safe: column ya pasó el whitelist; no hay inyección posible.
	q := rewritePlaceholders(driver, fmt.Sprintf("UPDATE users SET %s = ? WHERE id = ?", column))
	if _, err := r.db.ExecContext(ctx, q, value, id); err != nil {
		return fmt.Errorf("set permission %s: %w", column, err)
	}
	return nil
}

// GetOwnerID devuelve el id del usuario con is_owner=true. Vacío si
// no hay owner aún (instalación fresca antes del setup wizard).
func (r *UserRepository) GetOwnerID(ctx context.Context) (string, error) {
	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	q := rewritePlaceholders(driver, `SELECT id FROM users WHERE is_owner = 1 LIMIT 1`)
	if driver == DriverPostgres {
		q = `SELECT id FROM users WHERE is_owner = TRUE LIMIT 1`
	}
	var id string
	err := r.db.QueryRowContext(ctx, q).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get owner id: %w", err)
	}
	return id, nil
}

// TransferOwnership mueve el flag is_owner=true de currentOwnerID a
// newOwnerID en UNA transacción. Validaciones:
//   - currentOwnerID DEBE ser actualmente el owner (anti-race).
//   - newOwnerID DEBE existir y ser activo y NO ser un profile.
//   - newOwnerID no puede coincidir con currentOwnerID (no-op).
// Si cualquiera falla, la TX revierte y nada cambia. La unicidad del
// owner la garantiza el índice parcial UNIQUE WHERE is_owner=1.
func (r *UserRepository) TransferOwnership(ctx context.Context, currentOwnerID, newOwnerID string) error {
	if currentOwnerID == newOwnerID {
		return fmt.Errorf("cannot transfer ownership to self")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	driver := DriverSQLite
	if !r.useSQLite() {
		driver = DriverPostgres
	}
	trueVal := "1"
	falseVal := "0"
	if driver == DriverPostgres {
		trueVal = "TRUE"
		falseVal = "FALSE"
	}

	// 1. Confirmar que currentOwnerID es el owner actual.
	q1 := rewritePlaceholders(driver, fmt.Sprintf(
		"SELECT 1 FROM users WHERE id = ? AND is_owner = %s", trueVal))
	var n int
	if err := tx.QueryRowContext(ctx, q1, currentOwnerID).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user %s is not the current owner", currentOwnerID)
		}
		return fmt.Errorf("verify current owner: %w", err)
	}

	// 2. Confirmar que newOwnerID es elegible (existe, activo, no
	//    profile, role admin).
	q2 := rewritePlaceholders(driver, fmt.Sprintf(
		"SELECT 1 FROM users WHERE id = ? AND is_active = %s AND parent_user_id IS NULL AND role = 'admin'", trueVal))
	if err := tx.QueryRowContext(ctx, q2, newOwnerID).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user %s is not eligible (must be active admin, household head)", newOwnerID)
		}
		return fmt.Errorf("verify new owner: %w", err)
	}

	// 3. Clear old, set new — el índice UNIQUE WHERE is_owner=1
	//    permite estas dos statements consecutivas dentro de la TX.
	q3 := rewritePlaceholders(driver, fmt.Sprintf(
		"UPDATE users SET is_owner = %s WHERE id = ?", falseVal))
	if _, err := tx.ExecContext(ctx, q3, currentOwnerID); err != nil {
		return fmt.Errorf("clear old owner: %w", err)
	}
	q4 := rewritePlaceholders(driver, fmt.Sprintf(
		"UPDATE users SET is_owner = %s, can_manage_admins = %s, can_manage_users = %s, can_manage_libraries = %s, can_manage_iptv = %s, can_edit_metadata = %s, can_change_artwork = %s, can_view_audit = %s WHERE id = ?",
		trueVal, trueVal, trueVal, trueVal, trueVal, trueVal, trueVal, trueVal))
	if _, err := tx.ExecContext(ctx, q4, newOwnerID); err != nil {
		return fmt.Errorf("set new owner: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ownership transfer: %w", err)
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
       access_expires_at, avatar_color,
       can_upload, upload_quota_bytes, upload_used_bytes
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
			&u.CanUpload,
			&u.UploadQuotaBytes,
			&u.UploadUsedBytes,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
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
		CanUpload:              r.CanUpload,
		UploadQuotaBytes:       r.UploadQuotaBytes,
		UploadUsedBytes:        r.UploadUsedBytes,
		IsOwner:                r.IsOwner,
		CanManageAdmins:        r.CanManageAdmins,
		CanManageUsers:         r.CanManageUsers,
		CanManageLibraries:     r.CanManageLibraries,
		CanManageIPTV:          r.CanManageIptv,
		CanEditMetadata:        r.CanEditMetadata,
		CanChangeArtwork:       r.CanChangeArtwork,
		CanViewAudit:           r.CanViewAudit,
	}
}
