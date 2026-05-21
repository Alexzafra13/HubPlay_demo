package library

import (
	"context"
	"log/slog"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
)

// AccessControl aísla las 6 operaciones ACL del olor Z del audit
// 2026-05-14 (library.Service god-service, 27 métodos, 6
// responsabilidades). El estado es 1 dep compartida (repo libraries
// vía pointer — el mismo que el Service usa para CRUD/scan).
//
// La autoridad ACL real vive en `db.LibraryRepository` (la regla
// "owner OR explicit grant OR primary admin"). Este sub-service es
// un thin orchestration que loggea las mutaciones para audit + lee
// el resultado del COALESCE para queries read-only.
type AccessControl struct {
	libraries *db.LibraryRepository
	logger    *slog.Logger
}

func newAccessControl(libraries *db.LibraryRepository, logger *slog.Logger) *AccessControl {
	return &AccessControl{
		libraries: libraries,
		logger:    logger,
	}
}

// ListForUser devuelve las libraries que el user puede acceder.
// Delegamos al repo — ver su doc comment para la regla ACL.
func (a *AccessControl) ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error) {
	return a.libraries.ListForUser(ctx, userID)
}

// UserHasAccess reporta si el user está allowed a acceder una library.
// Delegamos al repo — ver su doc comment para la regla ACL.
func (a *AccessControl) UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error) {
	return a.libraries.UserHasAccess(ctx, userID, libraryID)
}

// GrantAccess añade una fila library_access para (userID, libraryID).
// userID DEBE ser un top-level user (ADR-014); resolución del profile
// es trabajo del caller. Idempotente: re-grantear una fila existente
// es no-op.
func (a *AccessControl) GrantAccess(ctx context.Context, userID, libraryID string) error {
	if err := a.libraries.GrantAccess(ctx, userID, libraryID); err != nil {
		return err
	}
	a.logger.Info("library access granted", "user_id", userID, "library_id", libraryID)
	return nil
}

// RevokeAccess remueve el grant para (userID, libraryID). userID DEBE
// ser un top-level user. Profiles bajo ese user pierden acceso en la
// misma operación a través del predicate COALESCE.
func (a *AccessControl) RevokeAccess(ctx context.Context, userID, libraryID string) error {
	if err := a.libraries.RevokeAccess(ctx, userID, libraryID); err != nil {
		return err
	}
	a.logger.Info("library access revoked", "user_id", userID, "library_id", libraryID)
	return nil
}

// ListAccessByUser devuelve los library_ids para los que el user
// tiene grants explícitos. Surface admin-only: powers la matriz
// per-user del admin UI. El caller debe pasar un id top-level user;
// un profile id devuelve la slice vacía porque los grants siempre
// targetean al parent.
func (a *AccessControl) ListAccessByUser(ctx context.Context, userID string) ([]string, error) {
	return a.libraries.ListAccessByUser(ctx, userID)
}

// ReplaceAccess sobrescribe el grant set del user con libraryIDs en
// un diff transactional: grants missing se insertan, extras se
// revocan. El handler es responsable de resolver el user al id
// top-level Y de validar que cada libraryID realmente existe;
// ReplaceAccess sólo de-duplica el input. Devuelve nil en success
// incluso cuando el caller pasó una lista vacía (limpia todos los
// grants).
func (a *AccessControl) ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error {
	if err := a.libraries.ReplaceAccess(ctx, userID, libraryIDs); err != nil {
		return err
	}
	a.logger.Info("library access replaced", "user_id", userID, "count", len(libraryIDs))
	return nil
}
