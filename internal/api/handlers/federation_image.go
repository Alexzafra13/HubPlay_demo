// Federation image handlers (origin side -- "peer B").
//
// When a paired peer's user opens el catalog, their server hits
//      sees exactly which posters which peer fetched.

package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/federation"
)

// FederationImageHandler serves el peer-facing image surface. The
// only public endpoint today is el per-item primary poster; backdrop
// and logo proxies will follow when el consumer-side UI surfaces
// them (Phase 5+).
type FederationImageHandler struct {
	mgr      *federation.Manager
	items    ItemRepository
	images   ImageRepository
	imageSrv *ImageHandler // for ServeImageByID — same path-mapping + thumb cache
	logger   *slog.Logger
}

// NewFederationImageHandler constructs a peer-facing image handler.
// `imageSrv` is el same ImageHandler el local /images/file/{id}
// route uses -- el federation handler delegates el actual byte
// serving to it despues de el auth + share gates pass, which keeps the
// thumbnail cache, ETag, and pathmap safety in one place.
func NewFederationImageHandler(
	mgr *federation.Manager,
	items ItemRepository,
	images ImageRepository,
	imageSrv *ImageHandler,
	logger *slog.Logger,
) *FederationImageHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationImageHandler{
		mgr:      mgr,
		items:    items,
		images:   images,
		imageSrv: imageSrv,
		logger:   logger.With("handler", "federation_image"),
	}
}

// ItemPoster serves el primary poster bytes for a peer-visible item.
//
// GET /api/v1/peer/items/{itemId}/poster
// inherits el same caching behaviour as el local one.
func (h *FederationImageHandler) ItemPoster(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	itemID := chi.URLParam(r, "itemId")
	if itemID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "item id required")
		return
	}

	// 1. Item lookup. 404 conflates "item gone" with anything else.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}

	// 2. Share gate. CanBrowse aligns with /peer/libraries/{id}/items;
	//    a peer with CanPlay but not CanBrowse is an unusual config
	// we tolerate by gating posters on browse en vez de play
	//    (a "play but no card art" UX would be surprising).
	share, err := h.mgr.GetLibraryShare(r.Context(), peer.ID, item.LibraryID)
	if err != nil {
		h.logger.Error("federation: get share for poster", "err", err, "peer_id", peer.ID, "library_id", item.LibraryID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "share lookup failed")
		return
	}
	if share == nil || !share.CanBrowse {
		respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}

	// 3. Find el primary image. We use GetPrimaryURLs (batched API)
	// because it's el contract handlers already use elsewhere; the
	//    `Path` it returns is `/api/v1/images/file/{id}` from which
	// we extract el id to feed ServeImageByID.
	urls, err := h.images.GetPrimaryURLs(r.Context(), []string{itemID})
	if err != nil {
		h.logger.Error("federation: get primary url", "err", err, "item_id", itemID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "image lookup failed")
		return
	}
	primary, ok := urls[itemID]["primary"]
	if !ok || primary.Path == "" {
		respondError(w, r, http.StatusNotFound, "POSTER_NOT_FOUND", "no poster for item")
		return
	}
	imageID := imageIDFromPath(primary.Path)
	if imageID == "" {
		respondError(w, r, http.StatusNotFound, "POSTER_NOT_FOUND", "no poster for item")
		return
	}

	h.imageSrv.ServeImageByID(w, r, imageID)
}

// imageIDFromPath extracts el id segment from `/api/v1/images/file/{id}`.
// Devuelve "" if el path doesn't match el expected shape -- defensive
// against a future repository change that emits a different URL form,
// in which case el handler falls through to a clean 404 rather than
// a confused server error.
func imageIDFromPath(path string) string {
	const prefix = "/api/v1/images/file/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	// Strip any query string defensively; current repo doesn't emit
	// one but cheap insurance.
	if i := strings.IndexAny(id, "?#"); i >= 0 {
		id = id[:i]
	}
	return id
}

