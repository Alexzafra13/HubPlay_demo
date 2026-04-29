package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// MePeersHandler is the user-facing surface for browsing federated
// catalogs. Sits under /api/v1/me/peers — requires a normal user
// session (deps.Auth.Middleware), NOT admin. Any authenticated user
// can browse what the admin has shared with paired peers.
//
// The handlers here translate user requests into outbound peer JWT
// calls via Manager.BrowsePeerLibraries / BrowsePeerItems. The user
// never directly handles peer JWTs — they belong to the server.
type MePeersHandler struct {
	mgr    *federation.Manager
	logger *slog.Logger
}

func NewMePeersHandler(mgr *federation.Manager, logger *slog.Logger) *MePeersHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MePeersHandler{mgr: mgr, logger: logger.With("handler", "me_peers")}
}

// listPeerWire is a slim peer summary for the user-facing list.
// Subset of peerWire (admin) — no audit / health detail, no public
// key (the user has no use for the bytes; admins do).
type listPeerWire struct {
	ID          string `json:"id"`
	ServerUUID  string `json:"server_uuid"`
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint"`
}

// ListMyPeers returns the peers visible to the user — paired peers
// only (pending and revoked are admin-only concerns). Empty array is
// the legitimate "no servers connected yet" case.
func (h *MePeersHandler) ListMyPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := h.mgr.ListPeers(r.Context())
	if err != nil {
		h.logger.Error("federation: list peers for me", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list peers")
		return
	}
	out := make([]listPeerWire, 0, len(peers))
	for _, p := range peers {
		if p.Status != federation.PeerPaired {
			continue
		}
		out = append(out, listPeerWire{
			ID:          p.ID,
			ServerUUID:  p.ServerUUID,
			Name:        p.Name,
			BaseURL:     p.BaseURL,
			Status:      string(p.Status),
			Fingerprint: p.Fingerprint(),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// BrowsePeerLibraries returns the libraries a peer has shared with us.
// Routed under /me/peers/{peerID}/libraries. Live fetch — small list,
// no cache layer.
func (h *MePeersHandler) BrowsePeerLibraries(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	if peerID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID required")
		return
	}
	libs, err := h.mgr.BrowsePeerLibraries(r.Context(), peerID)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: browse peer libraries", "peer_id", peerID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": libs})
}

// BrowsePeerItems returns paginated items in a peer's library. Reads
// through the catalog cache: serves from cache if fresh, otherwise
// fetches live and writes to cache.
//
// Response includes a `from_cache` flag so the UI can show a
// "cached / offline" badge when serving stale data because peer is
// unreachable.
func (h *MePeersHandler) BrowsePeerItems(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	libraryID := chi.URLParam(r, "libraryID")
	if peerID == "" || libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and libraryID required")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, total, fromCache, err := h.mgr.BrowsePeerItems(r.Context(), peerID, libraryID, offset, limit)
	if err != nil {
		h.logger.Warn("federation: browse peer items",
			"peer_id", peerID, "library_id", libraryID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items":      items,
			"total":      total,
			"from_cache": fromCache,
		},
	})
}

// RefreshPeerLibrary purges the cache for (peer, library) so the next
// browse forces a live re-fetch. Wired to a "Refresh" button in the
// peer-library UI.
func (h *MePeersHandler) RefreshPeerLibrary(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	libraryID := chi.URLParam(r, "libraryID")
	if peerID == "" || libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and libraryID required")
		return
	}
	if err := h.mgr.PurgeCache(r.Context(), peerID, libraryID); err != nil {
		h.logger.Error("federation: purge cache", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to purge cache")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
