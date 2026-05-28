package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/db"
)

// AdminBackupHandler covers the admin "Copia de seguridad" surface:
// download a consistent SQLite snapshot, or upload one to be applied
// at next restart. The two operations are file-shaped (octet-stream
// download, multipart upload) and don't fit the JSON-shaped admin
// endpoints, so they live in their own file rather than in
// system.go.
//
// Design notes:
//
//   - Backup uses `VACUUM INTO`. It's the canonical SQLite live
//     backup: takes a brief shared lock, walks every page into a
//     fresh file, releases. No need to stop the server. We do it
//     into a temp file inside the DB's directory (same FS, atomic
//     rename), stream it to the response, then delete.
//
//   - Restore writes the upload to `<dbdir>/.pending-restore.db`.
//     Swapping the live DB while the process holds open WAL +
//     reader pool would leave SQLite in an undefined state, so we
//     stage the file and the binary applies it on the next boot
//     (see internal/db/restore.go). Operator gets a "restart to
//     apply" hint in the response.
//
//   - Upload size cap is generous (10 GiB) because real catalogues
//     do reach single-digit GBs on metadata + thumbnails. Set on
//     ParseMultipartForm so the body itself is bounded.
//
//   - Postgres: both endpoints return 501 Not Implemented with a
//     message pointing the operator at pg_dump / pg_restore. The
//     application can't safely orchestrate those (they need cluster
//     credentials, network access, and the operator's storage
//     choices) — they live out-of-band.
const maxRestoreUploadBytes = int64(10 * 1024 * 1024 * 1024)

type AdminBackupHandler struct {
	backup db.BackupOperator
	driver string
	dbPath string
	audit  handlers.AuditEmitter
	logger *slog.Logger
}

// NewAdminBackupHandler consume db.BackupOperator (típicamente
// *db.Maintenance) en lugar de `*sql.DB`. Cierra el olor K: el
// handler no debe poder ejecutar SQL arbitrario — sólo VacuumInto
// vía el contrato estrecho. audit nil-safe.
func NewAdminBackupHandler(driver string, backupOp db.BackupOperator, dbPath string, audit handlers.AuditEmitter, logger *slog.Logger) *AdminBackupHandler {
	return &AdminBackupHandler{
		backup: backupOp,
		driver: driver,
		audit:  audit,
		dbPath: dbPath,
		logger: logger,
	}
}

func (h *AdminBackupHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// notImplementedForPostgres centralises the response for the two
// admin backup endpoints when the binary is running against Postgres.
// Returns true (and writes the response) when the request should
// stop; false when the active backend supports the operation and
// the caller should proceed.
func (h *AdminBackupHandler) notImplementedForPostgres(w http.ResponseWriter, r *http.Request, op string) bool {
	if h.driver != db.DriverPostgres {
		return false
	}
	handlers.RespondError(w, r, http.StatusNotImplemented, "POSTGRES_BACKUP_NOT_SUPPORTED",
		fmt.Sprintf("%s is not available when the backend is Postgres — use pg_dump / pg_restore against the cluster directly", op))
	return true
}

// Download streams a fresh consistent snapshot of the SQLite
// database to the client as `application/octet-stream`. Filename
// includes a UTC timestamp so multiple downloads in a session don't
// overwrite each other in the operator's Downloads folder.
func (h *AdminBackupHandler) Download(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	if h.notImplementedForPostgres(w, r, "Backup download") {
		return
	}
	dir := filepath.Dir(h.dbPath)
	stamp := time.Now().UTC().Format("20060102-150405")
	tmpPath := filepath.Join(dir, fmt.Sprintf(".backup-%s.db", stamp))

	// `VACUUM INTO 'path'` is the SQLite-blessed live backup primitive.
	// Delegamos en db.BackupOperator que envuelve la llamada; ese
	// contrato refuse Postgres (devuelve error) y no expone Exec
	// arbitrario al handler.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	if err := h.backup.VacuumInto(ctx, tmpPath); err != nil {
		h.logger.Error("backup VACUUM INTO failed", "error", err, "tmp", tmpPath)
		handlers.RespondError(w, r, http.StatusInternalServerError, "BACKUP_FAILED",
			"failed to create backup snapshot")
		return
	}
	defer func() {
		if rmErr := os.Remove(tmpPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			h.logger.Warn("could not remove backup tmp", "path", tmpPath, "error", rmErr)
		}
	}()

	f, err := os.Open(tmpPath)
	if err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "BACKUP_FAILED",
			"failed to read backup snapshot")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "BACKUP_FAILED",
			"failed to stat backup snapshot")
		return
	}

	filename := fmt.Sprintf("hubplay-backup-%s.db", stamp)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	// Audit ANTES de empezar el stream — si el cliente se desconecta
	// a mitad, el log igual queda: alguien autenticado pidió un dump.
	h.auditEmit().LogBackupDownloaded(r.Context(), r)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		// Connection probably gone; nothing to recover. Log and let
		// the deferred cleanup run.
		h.logger.Warn("backup stream interrupted", "error", err)
	}
}

// Upload receives a multipart upload of a backup file and stages it
// at `<dbdir>/.pending-restore.db`. The applied swap happens at the
// next process boot (see db.ApplyPendingRestoreIfAny). The response
// always tells the operator to restart — there's no live swap mode.
func (h *AdminBackupHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h.notImplementedForPostgres(w, r, "Backup restore") {
		return
	}
	// Cap the body so a malicious or runaway upload doesn't fill the
	// data volume. ParseMultipartForm reads up to its argument as the
	// in-memory cutoff; the rest spills to a temp file the stdlib
	// manages. We pair the cap with MaxBytesReader so the connection
	// dies past the limit instead of just buffering forever.
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "UPLOAD_TOO_LARGE",
			fmt.Sprintf("upload failed (max %d bytes): %s", maxRestoreUploadBytes, err.Error()))
		return
	}

	file, header, err := r.FormFile("backup")
	if err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
			"missing 'backup' file in multipart form")
		return
	}
	defer file.Close()

	dir := filepath.Dir(h.dbPath)
	tmpPath := filepath.Join(dir, db.PendingRestoreFilename+".tmp")
	pendingPath := filepath.Join(dir, db.PendingRestoreFilename)

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "RESTORE_FAILED",
			"failed to open staging file")
		return
	}
	written, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpPath)
		handlers.RespondError(w, r, http.StatusInternalServerError, "RESTORE_FAILED",
			"failed to write upload to staging file")
		return
	}

	// Sanity-check the uploaded bytes are at least shaped like a
	// SQLite database before we promote them. Same magic check the
	// startup swap uses, but applied early so the operator hears
	// "wrong file" now instead of seeing the server fail to boot.
	if err := verifyUploadIsSQLite(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
			"uploaded file does not look like a SQLite database")
		return
	}

	// Atomic on the same filesystem.
	if err := os.Rename(tmpPath, pendingPath); err != nil {
		_ = os.Remove(tmpPath)
		handlers.RespondError(w, r, http.StatusInternalServerError, "RESTORE_FAILED",
			"failed to stage restore file")
		return
	}

	h.logger.Info("backup staged for restore on next restart",
		"path", pendingPath, "size_bytes", written, "uploaded_filename", header.Filename)
	h.auditEmit().LogBackupRestored(r.Context(), r)

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"staged":            true,
			"size_bytes":        written,
			"uploaded_filename": header.Filename,
			"applies_on":        "next_restart",
		},
	})
}

// verifyUploadIsSQLite checks the magic header without reading the
// whole file. We delegate to the same logic the startup-swap uses
// so the two paths agree on what counts as "valid enough to accept".
// A separate file at the package boundary keeps the magic constant
// internal to db/.
func verifyUploadIsSQLite(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 16)
	if _, err := io.ReadFull(f, buf); err != nil {
		return err
	}
	expected := []byte("SQLite format 3\x00")
	for i, b := range expected {
		if buf[i] != b {
			return fmt.Errorf("magic mismatch at byte %d", i)
		}
	}
	return nil
}
