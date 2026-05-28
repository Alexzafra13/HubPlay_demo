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

package iptvhandler

import (
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	"hubplay/internal/iptv"
)

// IPTVHandler es el facade que ensambla los sub-handlers del surface
// IPTV. Los sub-handlers con micro-interfaces propias se embeben por
// puntero (misma técnica que ItemHandler). Los métodos que aún no han
// migrado a sub-handler viven como receivers directos del facade.
type IPTVHandler struct {
	// Sub-handlers con micro-interfaces. Cada uno define su contrato
	// mínimo del iptv.Service (~2-11 métodos cada uno, de ~50 total).
	// Cierra NN (interfaces de servicio gigantes).
	*iptvChannelHandler
	*iptvPlaybackFailureHandler
	*iptvEPGHandler
	*iptvPersonalisationHandler
	*iptvAdminOrderHandler
	*iptvAdminHandler
	*iptvHealthHandler
	*iptvFavoritesHandler
	*iptvLogoHandler
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
//
// iptvOps es la unión de las micro-interfaces de los 9 sub-handlers.
// *iptv.Service la satisface.
type iptvOps interface {
	channelBrowseOps
	playbackFailureReporter
	epgManager
	channelPersonaliser
	adminChannelOrderManager
	iptvAdminOps
	channelHealthOps
	channelFavoritesOps
	channelLogoOps
}

func NewIPTVHandler(svc iptvOps, proxy handlers.IPTVStreamProxyService, transmux handlers.IPTVTransmuxer, logoCache *iptv.LogoCache, imageDir string, libraries handlers.LibraryRepository, access handlers.LibraryAccessService, audit handlers.AuditEmitter, bus handlers.EventBusPublisher, logger *slog.Logger) *IPTVHandler {
	lg := logger.With("module", "iptv-handler")
	return &IPTVHandler{
		iptvChannelHandler: &iptvChannelHandler{
			svc: svc, proxy: proxy, transmux: transmux,
			logoCache: logoCache, imageDir: imageDir,
			access: access, logger: lg,
		},
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
		iptvAdminHandler: &iptvAdminHandler{
			svc: svc, libraries: libraries, access: access, audit: audit, logger: lg,
		},
		iptvHealthHandler: &iptvHealthHandler{
			svc: svc, access: access, audit: audit, logger: lg,
		},
		iptvFavoritesHandler: &iptvFavoritesHandler{
			svc: svc, libraries: libraries, access: access, logger: lg,
		},
		iptvLogoHandler: &iptvLogoHandler{
			svc: svc, imageDir: imageDir, logger: lg,
		},
	}
}

// iptvDenyForbidden writes a NOT_FOUND response (not 403) so an
// unauthorised user can't distinguish "channel exists but you can't
// see it" from "channel doesn't exist".
func iptvDenyForbidden(w http.ResponseWriter, r *http.Request) {
	handlers.RespondAppError(w, r.Context(), domain.NewNotFound("channel"))
}
