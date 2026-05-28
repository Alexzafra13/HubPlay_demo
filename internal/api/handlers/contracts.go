package handlers

import (
	"context"
	"net/http"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/updates"
)

// AuditEmitter es la mínima superficie del audit service que los
// handlers usan. nil-safe: cuando el binario arranca sin audit
// cableado (tests, deploys legacy), todas las llamadas son no-op.
type AuditEmitter interface {
	LogAuthLogin(ctx context.Context, r *http.Request, actorUserID, username string)
	LogAuthLoginFailed(ctx context.Context, r *http.Request, attemptedUsername, reason string)
	LogAuthLogout(ctx context.Context, r *http.Request, actorUserID, sessionID string)
	LogPermissionChanged(ctx context.Context, r *http.Request, targetUserID string, changes map[string]bool)
	LogRoleChanged(ctx context.Context, r *http.Request, targetUserID, oldRole, newRole string)
	LogUserCreated(ctx context.Context, r *http.Request, newUserID, username, role string)
	LogUserDeleted(ctx context.Context, r *http.Request, deletedUserID, deletedUsername string)
	LogUserActiveChanged(ctx context.Context, r *http.Request, targetUserID string, active bool)
	LogPasswordReset(ctx context.Context, r *http.Request, targetUserID string)
	LogLibraryCreated(ctx context.Context, r *http.Request, libraryID, name, contentType string)
	LogLibraryDeleted(ctx context.Context, r *http.Request, libraryID, name string)
	LogLibraryScanStarted(ctx context.Context, r *http.Request, libraryID string)
	LogMetadataEdited(ctx context.Context, r *http.Request, itemID, kind string)
	LogArtworkChanged(ctx context.Context, r *http.Request, targetType, targetID, kind string)
	LogIPTVImported(ctx context.Context, r *http.Request, libraryID string, channelCount int)
	LogChannelDisabled(ctx context.Context, r *http.Request, channelID string)
	LogChannelEnabled(ctx context.Context, r *http.Request, channelID string)
	LogCorsOriginAdded(ctx context.Context, r *http.Request, origin, note string)
	LogCorsOriginRemoved(ctx context.Context, r *http.Request, origin string)
	LogBackupDownloaded(ctx context.Context, r *http.Request)
	LogBackupRestored(ctx context.Context, r *http.Request)
	LogSystemRestart(ctx context.Context, r *http.Request, reason string)
	LogDBSwap(ctx context.Context, r *http.Request, oldDriver, newDriver string)
}

// NoopAudit es un sink no-op para AuditEmitter. Usado cuando el
// handler no recibe un audit service cableado.
type NoopAudit struct{}

func (NoopAudit) LogAuthLogin(_ context.Context, _ *http.Request, _, _ string)       {}
func (NoopAudit) LogAuthLoginFailed(_ context.Context, _ *http.Request, _, _ string) {}
func (NoopAudit) LogAuthLogout(_ context.Context, _ *http.Request, _, _ string)      {}
func (NoopAudit) LogPermissionChanged(_ context.Context, _ *http.Request, _ string, _ map[string]bool) {
}
func (NoopAudit) LogRoleChanged(_ context.Context, _ *http.Request, _, _, _ string)         {}
func (NoopAudit) LogUserCreated(_ context.Context, _ *http.Request, _, _, _ string)         {}
func (NoopAudit) LogUserDeleted(_ context.Context, _ *http.Request, _, _ string)            {}
func (NoopAudit) LogUserActiveChanged(_ context.Context, _ *http.Request, _ string, _ bool) {}
func (NoopAudit) LogPasswordReset(_ context.Context, _ *http.Request, _ string)             {}
func (NoopAudit) LogLibraryCreated(_ context.Context, _ *http.Request, _, _, _ string)      {}
func (NoopAudit) LogLibraryDeleted(_ context.Context, _ *http.Request, _, _ string)         {}
func (NoopAudit) LogLibraryScanStarted(_ context.Context, _ *http.Request, _ string)        {}
func (NoopAudit) LogMetadataEdited(_ context.Context, _ *http.Request, _, _ string)         {}
func (NoopAudit) LogArtworkChanged(_ context.Context, _ *http.Request, _, _, _ string)      {}
func (NoopAudit) LogIPTVImported(_ context.Context, _ *http.Request, _ string, _ int)       {}
func (NoopAudit) LogChannelDisabled(_ context.Context, _ *http.Request, _ string)           {}
func (NoopAudit) LogChannelEnabled(_ context.Context, _ *http.Request, _ string)            {}
func (NoopAudit) LogCorsOriginAdded(_ context.Context, _ *http.Request, _, _ string)        {}
func (NoopAudit) LogCorsOriginRemoved(_ context.Context, _ *http.Request, _ string)         {}
func (NoopAudit) LogBackupDownloaded(_ context.Context, _ *http.Request)                    {}
func (NoopAudit) LogBackupRestored(_ context.Context, _ *http.Request)                      {}
func (NoopAudit) LogSystemRestart(_ context.Context, _ *http.Request, _ string)             {}
func (NoopAudit) LogDBSwap(_ context.Context, _ *http.Request, _, _ string)                 {}

// PermissionsStore defines user permission operations for Dependencies.
type PermissionsStore interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
	SetPermission(ctx context.Context, id, column string, value bool) error
}

// CorsOriginStore defines CORS origin operations for Dependencies.
type CorsOriginStore interface {
	List(ctx context.Context) ([]db.CorsOriginRow, error)
	Insert(ctx context.Context, row db.CorsOriginRow) error
	Delete(ctx context.Context, origin string) error
	ListOrigins(ctx context.Context) ([]string, error)
}

// AuditLogStore defines audit log query operations for Dependencies.
type AuditLogStore interface {
	Query(ctx context.Context, q db.AuditQuery) ([]db.AuditLogRow, int64, error)
	DistinctEventTypes(ctx context.Context) ([]string, error)
}

// UpdatesProvider defines update status operations for Dependencies.
type UpdatesProvider interface {
	Status() updates.Status
	Check(ctx context.Context) error
}
