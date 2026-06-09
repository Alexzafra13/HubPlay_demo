package media

import (
	"context"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	librarymodel "hubplay/internal/library/model"
)

// LibraryACL is the per-library access surface the item handlers need: a
// single-library check (item-detail / recommendations) plus the caller's
// full accessible set (cross-library list / search). The concrete
// library.Service satisfies it; nil disables the gate in minimal test
// builds.
type LibraryACL interface {
	UserHasAccess(ctx context.Context, userID, libraryID string) (bool, error)
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
}

// itemLibraryAuthorized reports whether the caller may access an item
// that lives in libraryID. A nil access service (minimal test builds)
// passes through; otherwise it delegates to handlers.CanAccessLibrary
// (admins pass, unauthenticated and ACL-lookup errors fail closed).
//
// Centralises the per-library ACL gate shared by the item-detail and
// recommendations handlers so the local VOD metadata surface enforces
// the same library_access rule as streaming, IPTV and federation.
func itemLibraryAuthorized(r *http.Request, access handlers.LibraryAccessService, logger *slog.Logger, libraryID string) bool {
	if access == nil {
		return true
	}
	return handlers.CanAccessLibrary(r, access, logger, libraryID)
}

// accessibleLibraryIDs returns the library IDs the caller may read, and
// unrestricted=true when no filtering should apply (nil ACL test rig,
// claim-less request, or admin). A non-nil empty slice with
// unrestricted=false means "no access to anything" — the caller should
// short-circuit to an empty result rather than run an unscoped query.
func accessibleLibraryIDs(r *http.Request, access LibraryACL, logger *slog.Logger) (ids []string, unrestricted bool) {
	if access == nil {
		return nil, true
	}
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		return nil, true
	}
	if claims.Role == "admin" {
		return nil, true
	}
	libs, err := access.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		if logger != nil {
			logger.Error("list accessible libraries failed", "user", claims.UserID, "error", err)
		}
		return []string{}, false
	}
	ids = make([]string, len(libs))
	for i, l := range libs {
		ids[i] = l.ID
	}
	return ids, false
}
