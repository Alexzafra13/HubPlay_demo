// Local user-facing federation poster proxy.
//
// When a logged-in user opens a federated library, their browser
// renders <img src="/api/v1/me/peers/{peerID}/items/{itemId}/poster">
// for every card. We translate each request into a server-to-server
// peer call (with our peer JWT) and stream the bytes back. The user's
// browser only ever talks to OUR server -- the peer's hostname, peer
// JWT, and CORS posture stay invisible to the client.
//
// Same rationale as me_peer_stream.go's HLS proxy: keeping all
// federation traffic same-origin lets the operator lock
// `connect-src 'self'` without exceptions, and the peer never sees
// the user's IP / UA.

package me

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
)

// ProxyPeerItemPoster fetches the primary poster bytes for an item
// on a peer and streams them to the user's browser. Same-origin so
// the user's HTML <img> loads it without any CORS dance.
//
// GET /api/v1/me/peers/{peerID}/items/{itemId}/poster
//
// Cache-Control + ETag headers from the origin pass through verbatim.
// The origin's ImageHandler already emits a strong content-addressed
// ETag so a re-render of the same grid hits a 304 in the browser.
func (h *MePeersHandler) ProxyPeerItemPoster(w http.ResponseWriter, r *http.Request) {
	peerID := handlers.RequireParam(w, r, "peerID")
	if peerID == "" {
		return
	}
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	// `?w=N` (1..4095) se reenvía al amigo, que sirve una miniatura de su
	// caché: las tarjetas de la TV piden ~400 px en vez del original.
	// Cualquier otro valor se ignora (póster original, como antes).
	width := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("w")); err == nil && v > 0 && v < maxPosterWidth {
		width = v
	}
	resp, err := h.mgr.ProxyPeerItemPoster(r.Context(), peerID, itemID, width)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			handlers.RespondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: proxy peer poster",
			"peer_id", peerID, "item_id", itemID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Forward Content-Type so <img> dispatches correctly. Cache-Control
	// + ETag let the browser revalidate cheaply on the next render.
	for _, k := range []string{"Content-Type", "Cache-Control", "ETag", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// maxPosterWidth es el mismo tope que aplica el endpoint de imágenes del
// amigo a `?w=` (0 < w < 4096).
const maxPosterWidth = 4096
