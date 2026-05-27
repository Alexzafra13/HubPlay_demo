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

// IPTVHandler es el facade que ensambla los sub-handlers del surface
// IPTV. Los sub-handlers con micro-interfaces propias se embeben por
// puntero (misma técnica que ItemHandler). Los métodos que aún no han
// migrado a sub-handler viven como receivers directos del facade.
type IPTVHandler struct {
	// Sub-handlers con micro-interfaces (cierre progresivo de NN).
	*iptvPlaybackFailureHandler
	*iptvEPGHandler
	*iptvPersonalisationHandler
	*iptvAdminOrderHandler

	// Campos del facade — usados por los métodos que aún no han
	// migrado a sub-handler propio.
	svc       IPTVService
	proxy     IPTVStreamProxyService
	transmux  IPTVTransmuxer
	logoCache *iptv.LogoCache
	imageDir  string
	libraries LibraryRepository
	access    LibraryAccessService
	audit     AuditEmitter
	bus       EventBusPublisher
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
func NewIPTVHandler(svc IPTVService, proxy IPTVStreamProxyService, transmux IPTVTransmuxer, logoCache *iptv.LogoCache, imageDir string, libraries LibraryRepository, access LibraryAccessService, audit AuditEmitter, bus EventBusPublisher, logger *slog.Logger) *IPTVHandler {
	lg := logger.With("module", "iptv-handler")
	return &IPTVHandler{
		iptvPlaybackFailureHandler: &iptvPlaybackFailureHandler{
			svc: svc, access: access, logger: lg,
		},
		iptvEPGHandler: &iptvEPGHandler{
			svc: svc, access: access, logger: lg,
		},
		iptvPersonalisationHandler: &iptvPersonalisationHandler{
			svc: svc, access: access, bus: bus, logger: lg,
		},
		iptvAdminOrderHandler: &iptvAdminOrderHandler{
			svc: svc, logger: lg,
		},
		svc:       svc,
		proxy:     proxy,
		transmux:  transmux,
		logoCache: logoCache,
		imageDir:  imageDir,
		libraries: libraries,
		access:    access,
		audit:     audit,
		bus:       bus,
		logger:    lg,
	}
}


func (h *IPTVHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
}

// canAccessLibrary delegates to the package-level helper. Thin wrapper
// kept so every iptv_* file can write `h.canAccessLibrary(r, id)`
// without re-importing the helper.
func (h *IPTVHandler) canAccessLibrary(r *http.Request, libraryID string) bool {
	return canAccessLibrary(r, h.access, h.logger, libraryID)
}

// iptvDenyForbidden writes a NOT_FOUND response (not 403) so an
// unauthorised user can't distinguish "channel exists but you can't
// see it" from "channel doesn't exist".
func iptvDenyForbidden(w http.ResponseWriter, r *http.Request) {
	respondAppError(w, r.Context(), domain.NewNotFound("channel"))
}

// denyForbidden thin wrapper para mantener compatibilidad con los
// ficheros que aún usan receiver. Se eliminará al completar el split.
func (h *IPTVHandler) denyForbidden(w http.ResponseWriter, r *http.Request) {
	iptvDenyForbidden(w, r)
}
