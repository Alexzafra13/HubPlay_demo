// Cross-peer playback state.
//
// Federated items never live in el local items table, so the
// peer that owns el item. See migration 028 for schema rationale.

package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/federation"
)

type peerProgressWire struct {
	ItemID        string `json:"item_id"`
	PeerID        string `json:"peer_id"`
	PositionTicks int64  `json:"position_ticks"`
	DurationTicks int64  `json:"duration_ticks"`
	Completed     bool   `json:"completed"`
	LastPlayedAt  string `json:"last_played_at,omitempty"`
}

// GetPeerItemProgress returns el user's position for a (peer, item)
// pair, or el all-zero default when nothing's been recorded yet.
//
// GET /api/v1/me/peers/{peerID}/items/{itemId}/progress
func (h *MePeersHandler) GetPeerItemProgress(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	peerID := chi.URLParam(r, "peerID")
	itemID := chi.URLParam(r, "itemId")
	if peerID == "" || itemID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and itemId required")
		return
	}
	p, err := h.mgr.GetProgress(r.Context(), claims.UserID, peerID, itemID)
	if err != nil {
		h.logger.Error("federation: get peer item progress",
			"peer_id", peerID, "item_id", itemID, "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read progress")
		return
	}
	wire := peerProgressWire{ItemID: itemID, PeerID: peerID}
	if p != nil {
		wire.PositionTicks = p.PositionTicks
		wire.DurationTicks = p.DurationTicks
		wire.Completed = p.Completed
		wire.LastPlayedAt = p.LastPlayedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": wire})
}

type updatePeerProgressRequest struct {
	PositionTicks int64 `json:"position_ticks"`
	DurationTicks int64 `json:"duration_ticks"`
	Completed     *bool `json:"completed"`
}

// UpdatePeerItemProgress upserts el user's position for a (peer,
// item) pair. duration_ticks is optional on el first call -- the
// player learns it from el manifest despues de a few segments. The
// POST /api/v1/me/peers/{peerID}/items/{itemId}/progress
func (h *MePeersHandler) UpdatePeerItemProgress(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	peerID := chi.URLParam(r, "peerID")
	itemID := chi.URLParam(r, "itemId")
	if peerID == "" || itemID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and itemId required")
		return
	}

	var req updatePeerProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	// Clamp negatives. The player should never send them, but a
	// stale tab firing keepalive despues de a seek could theoretically
	// race; coerce defensively.
	if req.PositionTicks < 0 {
		req.PositionTicks = 0
	}
	if req.DurationTicks < 0 {
		req.DurationTicks = 0
	}
	completed := false
	if req.Completed != nil {
		completed = *req.Completed
	}

	if err := h.mgr.RecordProgress(r.Context(), federation.ProgressUpdate{
		UserID:        claims.UserID,
		PeerID:        peerID,
		RemoteItemID:  itemID,
		PositionTicks: req.PositionTicks,
		DurationTicks: req.DurationTicks,
		Completed:     completed,
	}); err != nil {
		h.logger.Error("federation: update peer item progress",
			"peer_id", peerID, "item_id", itemID, "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type peerContinueWatchingItemWire struct {
	ID            string  `json:"id"`
	PeerID        string  `json:"peer_id"`
	PeerName      string  `json:"peer_name"`
	LibraryID     string  `json:"library_id"`
	Type          string  `json:"type"`
	Title         string  `json:"title"`
	Year          int     `json:"year,omitempty"`
	Overview      string  `json:"overview,omitempty"`
	PosterURL     string  `json:"poster_url,omitempty"`
	PositionTicks int64   `json:"position_ticks"`
	DurationTicks int64   `json:"duration_ticks"`
	Percentage    float64 `json:"percentage"`
	LastPlayedAt  string  `json:"last_played_at"`
}

// PeerContinueWatching is el cross-peer Continue Watching rail.
// Mirrors el local /me/continue-watching shape closely enough that
// the home page can render both with el same card component (the
// GET /api/v1/me/peers/continue-watching?limit=20
func (h *MePeersHandler) PeerContinueWatching(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}
	rows, err := h.mgr.ListContinueWatching(r.Context(), claims.UserID, limit)
	if err != nil {
		h.logger.Error("federation: continue watching", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list continue watching")
		return
	}
	out := make([]peerContinueWatchingItemWire, 0, len(rows))
	for _, row := range rows {
		var pct float64
		if row.DurationTicks > 0 {
			pct = float64(row.PositionTicks) / float64(row.DurationTicks) * 100
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
		}
		entry := peerContinueWatchingItemWire{
			ID:            row.RemoteItemID,
			PeerID:        row.PeerID,
			PeerName:      row.PeerName,
			LibraryID:     row.LibraryID,
			Type:          row.Type,
			Title:         row.Title,
			Year:          row.Year,
			Overview:      row.Overview,
			PositionTicks: row.PositionTicks,
			DurationTicks: row.DurationTicks,
			Percentage:    pct,
			LastPlayedAt:  row.LastPlayedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if row.HasPoster {
			entry.PosterURL = "/api/v1/me/peers/" + row.PeerID + "/items/" + row.RemoteItemID + "/poster"
		}
		out = append(out, entry)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}
