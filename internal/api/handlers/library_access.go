package handlers

import (
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
)

// CanAccessLibrary gates per-library reads for the authenticated caller.
// Admins pass unconditionally, unauthenticated calls fail closed, and
// ACL lookup errors are logged and treated as deny so a transient DB
// hiccup never widens access.
//
// This is the single canonical implementation of the "same ACL as the
// rest of the surface" check. The IPTV, media (VOD/stream) and library
// handlers all route through it so the gate can't drift between
// surfaces — the exact failure mode that left local on-demand playback
// ungated while IPTV and federation enforced the ACL.
//
// The 404-vs-403 decision (don't leak existence to unauthorised
// callers) belongs to the caller — this function only answers yes / no.
func CanAccessLibrary(r *http.Request, access LibraryAccessService, logger *slog.Logger, libraryID string) bool {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	ok, err := access.UserHasAccess(r.Context(), claims.UserID, libraryID)
	if err != nil {
		if logger != nil {
			logger.Error("library access check failed",
				"user", claims.UserID, "library", libraryID, "error", err)
		}
		return false
	}
	return ok
}
