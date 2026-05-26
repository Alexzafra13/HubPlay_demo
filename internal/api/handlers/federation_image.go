// Federation image handlers (origin side -- "peer B").
//
// When a paired peer's user opens the catalog, their server hits
// these endpoints with its peer JWT to fetch the bytes of an item's
// poster. We re-verify the calling peer still holds CanBrowse on the
// item's library on every fetch -- a peer that lost a share since
// the catalog was cached cannot keep pulling artwork from us.
//
// Why proxy posters at all (vs. shipping the local image URL to the
// peer): two reasons that mirror Section 8 of docs/architecture/
// federation.md.
//
//   1. Authorisation locality. The peer never holds a token that
//      authenticates against /api/v1/images/file/{id}. Returning
//      a direct URL would mean either dropping that gate (anyone
//      with the URL gets the bytes) or minting an extra signed URL
//      per poster (operational complexity). Proxying re-uses the
//      peer JWT we already issue.
//   2. Privacy + audit. The user clicking a peer's poster never
//      reveals their IP / UA to the origin operator. The audit log
//      sees exactly which posters which peer fetched.

package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/federation"
)

// FederationImageHandler serves the peer-facing image surface. The
// only public endpoint today is the per-item primary poster; backdrop
// and logo proxies will follow when the consumer-side UI surfaces
// them (Phase 5+).
type FederationImageHandler struct {
	mgr      *federation.Manager
	items    ItemRepository
	images   ImageRepository
	imageSrv *ImageHandler // for ServeImageByID — same path-mapping + thumb cache
	logger   *slog.Logger
}

// NewFederationImageHandler constructs a peer-facing image handler.
// `imageSrv` is the same ImageHandler the local /images/file/{id}
// route uses -- the federation handler delegates the actual byte
// serving to it after the auth + share gates pass, which keeps the
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

// ItemPoster serves the primary poster bytes for a peer-visible item.
//
// GET /api/v1/peer/items/{itemId}/poster
//
// ACL pipeline (every step conflates failure into 404 so a peer can't
// distinguish "item doesn't exist" from "you can't see it"):
//
//  1. Item must exist locally.
//  2. Item's library must be shared with the calling peer with
//     CanBrowse=true. CanBrowse is the same gate the catalog browse
//     uses -- a peer that can list the item should be able to render
//     a card for it.
//  3. Item must have a primary image.
//
// The actual file serving (path mapping, thumbnail cache, ETags) is
// delegated to ImageHandler.ServeImageByID so the federation surface
// inherits the same caching behaviour as the local one.
func (h *FederationImageHandler) ItemPoster(w http.ResponseWriter, r *http.Request) {
	peer := requirePeer(w, r)
	if peer == nil {
		return
	}
	itemID := requireParam(w, r, "itemId")
	if itemID == "" {
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
	//    we tolerate by gating posters on browse rather than play
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

	// 3. Find the primary image. We use GetPrimaryURLs (batched API)
	//    because it's the contract handlers already use elsewhere; the
	//    `Path` it returns is `/api/v1/images/file/{id}` from which
	//    we extract the id to feed ServeImageByID.
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

// imageIDFromPath extracts the id segment from `/api/v1/images/file/{id}`.
// Returns "" if the path doesn't match the expected shape -- defensive
// against a future repository change that emits a different URL form,
// in which case the handler falls through to a clean 404 rather than
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
