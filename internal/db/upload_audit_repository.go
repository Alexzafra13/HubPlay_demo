package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UploadAuditRepository persists the append-only audit row for each
// finished upload (success or otherwise). Raw SQL — only INSERT +
// query-by-user-and-window, neither big enough to justify a sqlc
// query file (and we already know sqlc 1.31.1 trips on the kind of
// query we'd write here).
type UploadAuditRepository struct {
	db     *sql.DB
	driver string
}

func NewUploadAuditRepository(driver string, database *sql.DB) *UploadAuditRepository {
	return &UploadAuditRepository{db: database, driver: driver}
}

// UploadAuditRow es la representación in-memory de una fila de
// upload_audit. Mapea 1-a-1 con la migración 054.
type UploadAuditRow struct {
	ID           string
	UserID       string
	LibraryID    string // empty when the upload never landed
	OriginalName string
	FinalPath    string // empty when the upload never landed
	Bytes        int64
	SHA256       string
	MimeDetected string
	Outcome      string // accepted | rejected | aborted | error
	ErrorMessage string
	StartedAt    time.Time
	FinishedAt   time.Time
	DurationMs   int64
}

// Insert appends one audit row. The id MUST be unique; the service
// generates it via upload.RandomID before invoking. Time values are
// stored UTC — we coerce here to avoid drift from caller-side mistakes.
func (r *UploadAuditRepository) Insert(ctx context.Context, row UploadAuditRow) error {
	query := rewritePlaceholders(r.driver, `
		INSERT INTO upload_audit (
			id, user_id, library_id, original_name, final_path,
			bytes, sha256, mime_detected, outcome, error_message,
			started_at, finished_at, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	_, err := r.db.ExecContext(ctx, query,
		row.ID,
		row.UserID,
		nullStringFromOptional(row.LibraryID),
		row.OriginalName,
		nullStringFromOptional(row.FinalPath),
		row.Bytes,
		nullStringFromOptional(row.SHA256),
		nullStringFromOptional(row.MimeDetected),
		row.Outcome,
		nullStringFromOptional(row.ErrorMessage),
		row.StartedAt.UTC(),
		row.FinishedAt.UTC(),
		row.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("insert upload audit: %w", err)
	}
	return nil
}

// ListByUser devuelve las últimas N filas del usuario, más recientes
// primero. Usado por el panel admin "uploads de Alex" y por el endpoint
// /api/uploads (lista del propio usuario) cuando el cliente repuebla
// la UI tras un refresh.
func (r *UploadAuditRepository) ListByUser(ctx context.Context, userID string, limit int) ([]UploadAuditRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	query := rewritePlaceholders(r.driver, `
		SELECT id, user_id, library_id, original_name, final_path,
		       bytes, sha256, mime_detected, outcome, error_message,
		       started_at, finished_at, duration_ms
		FROM upload_audit
		WHERE user_id = ?
		ORDER BY started_at DESC
		LIMIT ?`)
	rows, err := r.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list upload audit by user: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []UploadAuditRow
	for rows.Next() {
		var (
			r0                                                         UploadAuditRow
			libraryID, finalPath, sha256, mimeDetected, errorMessage   sql.NullString
		)
		if err := rows.Scan(
			&r0.ID,
			&r0.UserID,
			&libraryID,
			&r0.OriginalName,
			&finalPath,
			&r0.Bytes,
			&sha256,
			&mimeDetected,
			&r0.Outcome,
			&errorMessage,
			&r0.StartedAt,
			&r0.FinishedAt,
			&r0.DurationMs,
		); err != nil {
			return nil, fmt.Errorf("scan upload audit row: %w", err)
		}
		r0.LibraryID = libraryID.String
		r0.FinalPath = finalPath.String
		r0.SHA256 = sha256.String
		r0.MimeDetected = mimeDetected.String
		r0.ErrorMessage = errorMessage.String
		out = append(out, r0)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upload audit rows: %w", err)
	}
	return out, nil
}
