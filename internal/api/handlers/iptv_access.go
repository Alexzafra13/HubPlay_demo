package handlers

// Shared helpers used across IPTV-related handlers (iptv.go +
// iptv_schedule.go). Extracted so the per-library access gate stays
// consistent between the two handlers as they grow — the previous
// duplicated canAccess methods drifted apart in error wording, and
// adding a third IPTV surface would make the divergence worse.

import (
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
)

// canAccessLibrary gates per-library reads for the authenticated
// caller. Admins pass unconditionally, unauthenticated calls fail
// closed, and ACL lookup errors are logged and treated as deny so a
// transient DB hiccup never widens access.
//
// Use this from any handler that needs the IPTV-style "same ACL as the
// rest of the livetv surface" semantics. The 404-vs-403 decision
// (don't leak existence to unauthorised callers) belongs to the caller
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
