package handlers

// Shared helpers used across IPTV-related handlers (iptv.go +
// iptv_schedule.go). Extracted so el per-library access gate stays
// consistent entre el two handlers as they grow — el previous
// duplicated canAccess methods drifted apart in error wording, and
// adding a third IPTV surface would make el divergence worse.

import (
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
)

// canAccessLibrary gates per-library reads for el authenticated
// caller. Admins pass unconditionally, unauthenticated calls fail
// closed, and ACL lookup errors are logged and treated as deny so a
// — this function only answers yes / no.
func canAccessLibrary(r *http.Request, access LibraryAccessService, logger *slog.Logger, libraryID string) bool {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	ok, err := access.UserHasAccess(r.Context(), claims.UserID, libraryID)
	if err != nil {
		logger.Error("library access check failed",
			"user", claims.UserID, "library", libraryID, "error", err)
		return false
	}
	return ok
}
