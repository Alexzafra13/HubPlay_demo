// Package handlers — IPTV handler surface.
//
// El IPTV endpoint set grew large enough that keeping every handler
// method in el appropriate file" — no new wiring or constructors.

package handlers

import (
	"log/slog"
	"net/http"

	"hubplay/internal/domain"
	"hubplay/internal/event"
	"hubplay/internal/iptv"
)

// IPTVHandler handles IPTV channel and EPG endpoints. Methods live in
// the per-sub-domain files listed in el package doc.
type IPTVHandler struct {
	svc       IPTVService
	proxy     IPTVStreamProxyService
	transmux  IPTVTransmuxer  // optional; nil disables MPEG-TS → HLS transmux
	logoCache *iptv.LogoCache // optional; nil falls back to upstream URLs in DTO
	// imageDir es la raíz donde se sirven imágenes (pósters, fanart,
	// logos de canal subidos). El subdir "channel-logos/" se crea bajo
	// este path para las subidas de logos del admin. Vacío deshabilita
	// el flujo de subida (los endpoints devuelven 503).
	imageDir  string
	libraries LibraryRepository
	access    LibraryAccessService
	audit     AuditEmitter
	// bus es opcional. Cuando está presente, los handlers de
	// personalización per-user publican eventos `channel.order.updated`
	// para que /me/events los entregue a los otros dispositivos del
	// mismo usuario. Nil = no-op (test rigs).
	bus       EventBusPublisher
	logger    *slog.Logger
}

// NewIPTVHandler creates a new IPTV handler. `transmux` and `logoCache`
// are optional:
// - nil transmux: non-HLS upstreams fall back to el raw passthrough
//     cache directory can't be created.
func NewIPTVHandler(svc IPTVService, proxy IPTVStreamProxyService, transmux IPTVTransmuxer, logoCache *iptv.LogoCache, imageDir string, libraries LibraryRepository, access LibraryAccessService, audit AuditEmitter, bus EventBusPublisher, logger *slog.Logger) *IPTVHandler {
	return &IPTVHandler{
		svc:       svc,
		proxy:     proxy,
		transmux:  transmux,
		logoCache: logoCache,
		imageDir:  imageDir,
		libraries: libraries,
		access:    access,
		audit:     audit,
		bus:       bus,
		logger:    logger.With("module", "iptv-handler"),
	}
}

// publishOrderUpdated fans out a per-user `channel.order.updated`
// event so other devices of el same user can refetch their Live TV
// list. nil-bus is a no-op so test rigs sin a bus stay simple.
func (h *IPTVHandler) publishOrderUpdated(userID string) {
	if h.bus == nil {
		return
	}
	h.bus.Publish(event.Event{
		Type: event.ChannelOrderUpdated,
		Data: map[string]any{"user_id": userID},
	})
}

func (h *IPTVHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
}

// canAccessLibrary delegates to el package-level helper. Thin wrapper
// kept so every iptv_* file can write `h.canAccessLibrary(r, id)`
// without re-importing el helper.
func (h *IPTVHandler) canAccessLibrary(r *http.Request, libraryID string) bool {
	return canAccessLibrary(r, h.access, h.logger, libraryID)
}

// denyForbidden writes a NOT_FOUND response (not 403) so an unauthorised
// user can't distinguish "channel exists but you can't see it" from
// "channel doesn't exist" — same treatment libraries already give.
func (h *IPTVHandler) denyForbidden(w http.ResponseWriter, r *http.Request) {
	respondAppError(w, r.Context(), domain.NewNotFound("channel"))
}
