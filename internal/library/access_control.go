package library

import (
	"context"
	"log/slog"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
)

// AccessControl aísla las operaciones ACL de library.Service.
// La autoridad real vive en db.LibraryRepository (regla "owner OR grant OR primary admin").
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

func (a *AccessControl) ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error) {
	return a.libraries.ListForUser(ctx, userID)
}

func (a *AccessControl) UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error) {
	return a.libraries.UserHasAccess(ctx, userID, libraryID)
}

// GrantAccess añade acceso (userID, libraryID). userID DEBE ser top-level (ADR-014).
// Idempotente: re-grant es no-op.
func (a *AccessControl) GrantAccess(ctx context.Context, userID, libraryID string) error {
	if err := a.libraries.GrantAccess(ctx, userID, libraryID); err != nil {
		return err
	}
	a.logger.Info("library access granted", "user_id", userID, "library_id", libraryID)
	return nil
}

// RevokeAccess elimina el grant para (userID, libraryID). userID DEBE ser top-level.
// Profiles bajo ese user pierden acceso vía el COALESCE del predicate.
func (a *AccessControl) RevokeAccess(ctx context.Context, userID, libraryID string) error {
	if err := a.libraries.RevokeAccess(ctx, userID, libraryID); err != nil {
		return err
	}
	a.logger.Info("library access revoked", "user_id", userID, "library_id", libraryID)
	return nil
}

// ListAccessByUser devuelve los library_ids con grants explícitos del user.
// Solo admin-level: alimenta la matriz per-user del admin UI.
// Un profile id devuelve slice vacía porque grants siempre apuntan al parent.
func (a *AccessControl) ListAccessByUser(ctx context.Context, userID string) ([]string, error) {
	return a.libraries.ListAccessByUser(ctx, userID)
}

// ReplaceAccess sobrescribe los grants del user con libraryIDs en un diff
// transaccional. El handler resuelve al id top-level y valida existencia;
// aquí solo se de-duplica input. Lista vacía limpia todos los grants.
func (a *AccessControl) ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error {
	if err := a.libraries.ReplaceAccess(ctx, userID, libraryIDs); err != nil {
		return err
	}
	a.logger.Info("library access replaced", "user_id", userID, "count", len(libraryIDs))
	return nil
}
