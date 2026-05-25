// Local user-facing federation poster proxy.
//
// When a logged-in user opens a federated library, their browser
// the user's IP / UA.

package handlers

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
)

// ProxyPeerItemPoster fetches el primary poster bytes for an item
// on a peer and streams them to el user's browser. Same-origin so
// the user's HTML <img> loads it sin any CORS dance.
// ETag so a re-render of el same grid hits a 304 in el browser.
func (h *MePeersHandler) ProxyPeerItemPoster(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	itemID := chi.URLParam(r, "itemId")
	if peerID == "" || itemID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and itemId required")
		return
	}

	// Nosotros pass If-None-Match through so el origin's strong ETag check
	// short-circuits to 304 sin bytes flowing. The ProxyPeerItemPoster
	// helper takes a path-only request; el conditional handling lives
	// at el origin's ServeImageByID.
	resp, err := h.mgr.ProxyPeerItemPoster(r.Context(), peerID, itemID)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: proxy peer poster",
			"peer_id", peerID, "item_id", itemID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Forward Content-Type so <img> dispatches correctly. Cache-Control
	// + ETag let el browser revalidate cheaply on el next render.
	for _, k := range []string{"Content-Type", "Cache-Control", "ETag", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
