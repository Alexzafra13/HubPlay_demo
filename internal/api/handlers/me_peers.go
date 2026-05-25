package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

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
	respondData(w, http.StatusOK, out)
}

// unifiedLibraryWire pairs a library with the peer it came from in a
// shape friendly to a single React component (no nested object
// indirection on the consumer).
type unifiedLibraryWire struct {
	PeerID          string `json:"peer_id"`
	PeerName        string `json:"peer_name"`
	PeerFingerprint string `json:"peer_fingerprint"`
	LibraryID       string `json:"library_id"`
	LibraryName     string `json:"library_name"`
	ContentType     string `json:"content_type"`
	CanPlay         bool   `json:"can_play"`
	CanDownload     bool   `json:"can_download"`
	CanLiveTV       bool   `json:"can_livetv"`
}

// BrowseAllPeerLibraries returns every shared library across every
// paired peer in one response — drives the unified "/peers" landing
// page. Each row carries enough peer context that the UI can render
// "library X · shared by Pedro · 2 movies" without a second lookup.
func (h *MePeersHandler) BrowseAllPeerLibraries(w http.ResponseWriter, r *http.Request) {
	results, err := h.mgr.BrowseAllPeerLibraries(r.Context())
	if err != nil {
		h.logger.Error("federation: browse all peer libraries", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list libraries")
		return
	}
	out := make([]unifiedLibraryWire, 0, len(results))
	for _, e := range results {
		out = append(out, unifiedLibraryWire{
			PeerID:          e.Peer.ID,
			PeerName:        e.Peer.Name,
			PeerFingerprint: e.Peer.Fingerprint(),
			LibraryID:       e.Library.ID,
			LibraryName:     e.Library.Name,
			ContentType:     e.Library.ContentType,
			CanPlay:         e.Library.Scopes.CanPlay,
			CanDownload:     e.Library.Scopes.CanDownload,
			CanLiveTV:       e.Library.Scopes.CanLiveTV,
		})
	}
	respondData(w, http.StatusOK, out)
}

// BrowsePeerLibraries returns the libraries a peer has shared with us.
// Routed under /me/peers/{peerID}/libraries. Live fetch — small list,
// no cache layer.
func (h *MePeersHandler) BrowsePeerLibraries(w http.ResponseWriter, r *http.Request) {
	peerID := requireParam(w, r, "peerID")
	if peerID == "" {
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
	respondData(w, http.StatusOK, libs)
}

// peerItemWire is the user-facing item shape. Mirrors federation.SharedItem
// with one synthetic addition: `poster_url` is rewritten on this side
// so the user's browser only ever asks our origin (proxied via peer
// JWT). The peer's URL never reaches the client.
//
// BackdropColors carries the peer's pre-extracted dominant swatches so
// PeerItemDetail can paint the aurora on first paint without running
// node-vibrant in the browser. Same shape and consumer path the local
// ItemDetail uses for items in OUR catalog. Older peers that don't
// emit poster_color* leave the field nil → consumer falls back to
// runtime extraction, matching the pre-migration behaviour exactly.
type peerItemWire struct {
	ID             string           `json:"id"`
	Type           string           `json:"type"`
	Title          string           `json:"title"`
	Year           int              `json:"year,omitempty"`
	Overview       string           `json:"overview,omitempty"`
	PosterURL      string           `json:"poster_url,omitempty"`
	BackdropColors *peerItemPalette `json:"backdrop_colors,omitempty"`
}

// peerItemPalette mirrors the local `backdrop_colors` wire shape so
// the same frontend reducer drives the aurora for local AND federated
// items. Either field may be absent — the extractor couldn't classify
// a swatch in that role — and the consumer treats absence the same as
// "no server palette" (drop the corner from the gradient).
type peerItemPalette struct {
	Vibrant string `json:"vibrant,omitempty"`
	Muted   string `json:"muted,omitempty"`
}

// paletteFromShared lifts the two SharedItem color fields into the
// optional wire shape. Returns nil when both swatches are empty so
// omitempty drops the field entirely — keeps the wire payload clean
// for items that pre-date migration 014.
func paletteFromShared(it *federation.SharedItem) *peerItemPalette {
	if it.PosterColor == "" && it.PosterColorMuted == "" {
		return nil
	}
	return &peerItemPalette{Vibrant: it.PosterColor, Muted: it.PosterColorMuted}
}

// BrowsePeerItems returns paginated items in a peer's library. Reads
// through the catalog cache: serves from cache if fresh, otherwise
// fetches live and writes to cache.
//
// Response includes a `from_cache` flag so the UI can show a
// "cached / offline" badge when serving stale data because peer is
// unreachable.
//
// Per-item `poster_url` is synthesized on this side as a same-origin
// path; the user's browser fetches the bytes via our proxy without
// learning the peer's hostname. Items where the peer reported
// has_poster=false get no poster_url (the card falls back to the
// dominant-colour placeholder).
func (h *MePeersHandler) BrowsePeerItems(w http.ResponseWriter, r *http.Request) {
	peerID := requireParam(w, r, "peerID")
	if peerID == "" {
		return
	}
	libraryID := requireParam(w, r, "libraryID")
	if libraryID == "" {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	result, err := h.mgr.BrowsePeerItems(r.Context(), peerID, libraryID, offset, limit)
	if err != nil {
		h.logger.Warn("federation: browse peer items",
			"peer_id", peerID, "library_id", libraryID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}

	out := make([]peerItemWire, 0, len(result.Items))
	for _, it := range result.Items {
		row := peerItemWire{
			ID:             it.ID,
			Type:           it.Type,
			Title:          it.Title,
			Year:           it.Year,
			Overview:       it.Overview,
			BackdropColors: paletteFromShared(it),
		}
		if it.HasPoster {
			row.PosterURL = "/api/v1/me/peers/" + peerID + "/items/" + it.ID + "/poster"
		}
		out = append(out, row)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items":      out,
			"total":      result.Total,
			"from_cache": result.FromCache,
		},
	})
}

// peerSearchHitWire is the user-facing federated-search row. Adds the
// peer attribution that BrowsePeerItems doesn't need (single peer
// is implicit in the path) so the UI can render an origin badge and
// route the click into the right peer's detail view.
//
// BackdropColors mirrors peerItemWire — same rationale, same shape, so
// the rail/grid components don't fork their palette plumbing by surface.
type peerSearchHitWire struct {
	PeerID         string           `json:"peer_id"`
	PeerName       string           `json:"peer_name"`
	LibraryID      string           `json:"library_id,omitempty"`
	ID             string           `json:"id"`
	Type           string           `json:"type"`
	Title          string           `json:"title"`
	Year           int              `json:"year,omitempty"`
	Overview       string           `json:"overview,omitempty"`
	PosterURL      string           `json:"poster_url,omitempty"`
	BackdropColors *peerItemPalette `json:"backdrop_colors,omitempty"`
}

// SearchPeers fans out a query string to every paired peer in
// parallel and aggregates the hits. A peer that is offline / slow /
// errors is silently skipped so a single misbehaving peer cannot
// blank a federated search result page.
//
// GET /api/v1/me/peers/search?q=<query>&limit=<perPeerLimit>
func (h *MePeersHandler) SearchPeers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "q required")
		return
	}
	// Per-peer limit caps how many hits each peer can contribute. A
	// global limit on the aggregated set would let a chatty peer
	// crowd quieter ones out of the results; per-peer fairness gives
	// every paired server a slice of the page.
	perPeerLimit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if perPeerLimit <= 0 || perPeerLimit > 50 {
		perPeerLimit = 10
	}

	hits, err := h.mgr.SearchAllPeers(r.Context(), query, perPeerLimit, 2*time.Second)
	if err != nil {
		h.logger.Warn("federation: search all peers", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	out := make([]peerSearchHitWire, 0, len(hits))
	for _, h := range hits {
		row := peerSearchHitWire{
			PeerID:         h.Peer.ID,
			PeerName:       h.Peer.Name,
			LibraryID:      h.Item.LibraryID,
			ID:             h.Item.ID,
			Type:           h.Item.Type,
			Title:          h.Item.Title,
			Year:           h.Item.Year,
			Overview:       h.Item.Overview,
			BackdropColors: paletteFromShared(h.Item),
		}
		if h.Item.HasPoster {
			row.PosterURL = "/api/v1/me/peers/" + h.Peer.ID + "/items/" + h.Item.ID + "/poster"
		}
		out = append(out, row)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"hits": out,
		},
	})
}

// RecentPeers fans out a "what's new?" request to every paired peer
// and aggregates the hits with origin attribution. Powers the home
// page's "Recently added on peers" rail. Wire shape mirrors the
// federated-search response so the frontend reuses the same hit
// type: a peer that times out / errors is silently skipped.
//
// GET /api/v1/me/peers/recent?limit=<perPeerLimit>
func (h *MePeersHandler) RecentPeers(w http.ResponseWriter, r *http.Request) {
	perPeerLimit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if perPeerLimit <= 0 || perPeerLimit > 50 {
		perPeerLimit = 12
	}

	hits, err := h.mgr.RecentFromAllPeers(r.Context(), perPeerLimit, 2*time.Second)
	if err != nil {
		h.logger.Warn("federation: recent all peers", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	out := make([]peerSearchHitWire, 0, len(hits))
	for _, h := range hits {
		row := peerSearchHitWire{
			PeerID:         h.Peer.ID,
			PeerName:       h.Peer.Name,
			LibraryID:      h.Item.LibraryID,
			ID:             h.Item.ID,
			Type:           h.Item.Type,
			Title:          h.Item.Title,
			Year:           h.Item.Year,
			Overview:       h.Item.Overview,
			BackdropColors: paletteFromShared(h.Item),
		}
		if h.Item.HasPoster {
			row.PosterURL = "/api/v1/me/peers/" + h.Peer.ID + "/items/" + h.Item.ID + "/poster"
		}
		out = append(out, row)
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"hits": out,
		},
	})
}

// RefreshPeerLibrary purges the cache for (peer, library) so the next
// browse forces a live re-fetch. Wired to a "Refresh" button in the
// peer-library UI.
func (h *MePeersHandler) RefreshPeerLibrary(w http.ResponseWriter, r *http.Request) {
	peerID := requireParam(w, r, "peerID")
	if peerID == "" {
		return
	}
	libraryID := requireParam(w, r, "libraryID")
	if libraryID == "" {
		return
	}
	if err := h.mgr.PurgeCache(r.Context(), peerID, libraryID); err != nil {
		h.logger.Error("federation: purge cache", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to purge cache")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
