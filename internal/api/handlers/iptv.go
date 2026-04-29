// Package handlers — IPTV handler surface.
//
// The IPTV endpoint set grew large enough that keeping every handler
// in one file made navigation painful. The struct + constructor + the
// shared canAccessLibrary / denyForbidden helpers live here. Each
// sub-domain has its own file:
//
//   iptv_channels.go  — list / get / groups / stream / proxy /
//                       schedule / bulk-schedule + EPG schedule
//                       parsing helpers
//   iptv_admin.go     — M3U / EPG manual refresh + public-IPTV import
//                       + countries listing
//   iptv_favorites.go — channel favorites + continue-watching rail +
//                       watch beacon
//   iptv_epg.go       — EPG-source CRUD + catalog
//   iptv_health.go    — unhealthy / without-EPG / disable / enable +
//                       admin tvg_id patch
//   iptv_playback_failure.go — playback-failure beacon (already
//                       extracted prior to this split)
//
// All files share `package handlers` and attach methods to the
// `*IPTVHandler` defined here, so adding a handler is "create the
// method in the appropriate file" — no new wiring or constructors.

package handlers

import (
	"log/slog"
	"net/http"

	"hubplay/internal/domain"
	"hubplay/internal/iptv"
)

// IPTVHandler handles IPTV channel and EPG endpoints. Methods live in
// the per-sub-domain files listed in the package doc.
type IPTVHandler struct {
	svc       IPTVService
	proxy     IPTVStreamProxyService
	transmux  IPTVTransmuxer  // optional; nil disables MPEG-TS → HLS transmux
	logoCache *iptv.LogoCache // optional; nil falls back to upstream URLs in DTO
	libraries LibraryRepository
	access    LibraryAccessService
	logger    *slog.Logger
}

// NewIPTVHandler creates a new IPTV handler. `transmux` and `logoCache`
// are optional:
//   - nil transmux: non-HLS upstreams fall back to the raw passthrough
//     proxy (browsers can't play raw MPEG-TS, so this is a degraded
//     state — kept for tests + platforms without ffmpeg).
//   - nil logoCache: the /logo endpoint returns 404 unconditionally.
//     The DTO still emits the same-origin proxy path (toChannelDTO →
//     logoProxyURL) so the frontend's <img onError> handler trips on
//     every channel render and falls back to the initials avatar.
//     Functional but wasteful (N×404 per grid paint); the cache is
//     constructed best-effort in main.go and only ends up nil if the
//     cache directory can't be created.
func NewIPTVHandler(svc IPTVService, proxy IPTVStreamProxyService, transmux IPTVTransmuxer, logoCache *iptv.LogoCache, libraries LibraryRepository, access LibraryAccessService, logger *slog.Logger) *IPTVHandler {
	return &IPTVHandler{
		svc:       svc,
		proxy:     proxy,
		transmux:  transmux,
		logoCache: logoCache,
		libraries: libraries,
		access:    access,
		logger:    logger.With("module", "iptv-handler"),
	}
}

// canAccessLibrary delegates to the package-level helper. Thin wrapper
// kept so every iptv_* file can write `h.canAccessLibrary(r, id)`
// without re-importing the helper.
func (h *IPTVHandler) canAccessLibrary(r *http.Request, libraryID string) bool {
	return canAccessLibrary(r, h.access, h.logger, libraryID)
}

// denyForbidden writes a NOT_FOUND response (not 403) so an unauthorised
// user can't distinguish "channel exists but you can't see it" from
// "channel doesn't exist" — same treatment libraries already give.
func (h *IPTVHandler) denyForbidden(w http.ResponseWriter, r *http.Request) {
	respondAppError(w, r.Context(), domain.NewNotFound("channel"))
}
