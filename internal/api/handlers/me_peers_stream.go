package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// MePeersStreamHandler exposes the viewer-side streaming surface for
// federated content. The user clicks play on a peer's item; their
// browser hits these endpoints; the server proxies to the origin
// peer with a peer-JWT the user never sees.
//
// Why a separate handler from MePeersHandler: the streaming surface
// has an extra dependency (effectiveBaseURL resolver for URL
// rewriting in the master playlist) and slightly different error-
// handling shape (HLS fetch failures need to surface as the right
// status to the player).
type MePeersStreamHandler struct {
	mgr              *federation.Manager
	effectiveBaseURL func(ctx context.Context) string
	logger           *slog.Logger
}

func NewMePeersStreamHandler(
	mgr *federation.Manager,
	effectiveBaseURL func(ctx context.Context) string,
	logger *slog.Logger,
) *MePeersStreamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MePeersStreamHandler{
		mgr:              mgr,
		effectiveBaseURL: effectiveBaseURL,
		logger:           logger.With("handler", "me_peers_stream"),
	}
}

type startPeerStreamWire struct {
	SessionID         string `json:"session_id"`
	MasterPlaylistURL string `json:"master_playlist_url"`
	Method            string `json:"method"`
	Container         string `json:"container,omitempty"`
}

// StartSession opens a stream session at the origin peer and returns
// session info with the master playlist URL REWRITTEN to point at our
// local proxy. The user's player then talks only to us.
//
// The user's client capabilities (X-Hubplay-Client-Capabilities header)
// travel forward in the request body — the origin's stream.Decide()
// receives the same caps it would on a local request, so DirectPlay
// works through federation when the codec story aligns.
func (h *MePeersStreamHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "unauthenticated")
		return
	}
	peerID := chi.URLParam(r, "peerID")
	itemID := chi.URLParam(r, "itemID")
	if peerID == "" || itemID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "peer id and item id required")
		return
	}

	caps := capabilitiesFromRequestToWire(r)
	profile := r.URL.Query().Get("profile") // optional; origin defaults to 1080p

	result, err := h.mgr.RequestPeerStream(r.Context(), peerID, claims.UserID, itemID, profile, caps)
	if err != nil {
		h.logger.Warn("start peer stream", "err", err, "peer_id", peerID, "item_id", itemID)
		respondError(w, r, http.StatusBadGateway, "PEER_STREAM_FAILED", "failed to start stream on peer: "+err.Error())
		return
	}

	// Rewrite the master URL to a local-proxy URL so the user's player
	// only ever talks to our server. Preserves the session id so our
	// proxy can route follow-up variant/segment requests.
	localBase := strings.TrimRight(h.effectiveBaseURL(r.Context()), "/")
	localMaster := localBase + "/api/v1/me/peers/" + peerID + "/stream/session/" + result.SessionID + "/master.m3u8"

	respondJSON(w, http.StatusOK, map[string]any{
		"data": startPeerStreamWire{
			SessionID:         result.SessionID,
			MasterPlaylistURL: localMaster,
			Method:            result.Method,
			Container:         result.Container,
		},
	})
}

// MasterPlaylist fetches the origin's master.m3u8 for the session,
// rewrites every variant URL to our local proxy path, and serves
// the rewritten playlist. The player thinks it's talking to one
// server (us) end-to-end.
func (h *MePeersStreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	sessionID := chi.URLParam(r, "sessionID")
	if peerID == "" || sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "peer id and session id required")
		return
	}
	body, err := h.mgr.FetchPeerMasterPlaylist(r.Context(), peerID, sessionID)
	if err != nil {
		h.logger.Warn("fetch peer master", "err", err, "peer_id", peerID, "session", sessionID)
		respondError(w, r, http.StatusBadGateway, "PEER_PLAYLIST_FAILED", "failed to fetch master playlist")
		return
	}
	rewritten := federation.RewritePeerMasterPlaylist(body, peerID, h.effectiveBaseURL(r.Context()))
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(rewritten))
}

// QualityPlaylist proxies the variant playlist by URL. The body of a
// variant manifest contains relative segment URLs (e.g. `segment00001.ts`)
// — those resolve relative to the request URL, which is OUR proxy URL,
// so segment fetches naturally come back to us without any rewriting
// needed.
func (h *MePeersStreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	sessionID := chi.URLParam(r, "sessionID")
	quality := chi.URLParam(r, "quality")
	if peerID == "" || sessionID == "" || quality == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "peer/session/quality required")
		return
	}
	subPath := "/peer/stream/session/" + sessionID + "/" + quality + "/index.m3u8"
	if err := h.mgr.ProxyPeerStreamRequest(r.Context(), peerID, subPath, w); err != nil {
		h.logger.Warn("proxy variant playlist", "err", err, "peer_id", peerID)
		// Headers may already be partially written; safe minimum is to
		// log and let the response close.
	}
}

// Segment proxies one HLS segment as raw bytes. No rewriting needed
// because segments are binary.
func (h *MePeersStreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	sessionID := chi.URLParam(r, "sessionID")
	quality := chi.URLParam(r, "quality")
	segmentFile := chi.URLParam(r, "segment")
	if peerID == "" || sessionID == "" || quality == "" || segmentFile == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "peer/session/quality/segment required")
		return
	}
	subPath := "/peer/stream/session/" + sessionID + "/" + quality + "/" + segmentFile
	if err := h.mgr.ProxyPeerStreamRequest(r.Context(), peerID, subPath, w); err != nil {
		h.logger.Warn("proxy segment", "err", err, "peer_id", peerID)
	}
}

// StopSession forwards the close to the origin peer so the per-peer
// cap there is released. Best-effort — errors are logged but not
// surfaced; the origin's idle sweep would reap eventually.
func (h *MePeersStreamHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	sessionID := chi.URLParam(r, "sessionID")
	if peerID == "" || sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "peer id and session id required")
		return
	}
	if err := h.mgr.StopPeerStream(r.Context(), peerID, sessionID); err != nil && !errors.Is(err, context.Canceled) {
		h.logger.Warn("stop peer stream", "err", err, "peer_id", peerID, "session", sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// capabilitiesFromRequestToWire reads the X-Hubplay-Client-Capabilities
// header (parsed by the stream package) and returns its wire-shape
// representation for the federation peer body. nil when the header
// is absent — the origin will fall back to defaults.
func capabilitiesFromRequestToWire(r *http.Request) *federation.PeerStreamCaps {
	caps := stream.CapabilitiesFromRequest(r)
	if caps == nil {
		return nil
	}
	asList := func(set map[string]bool) []string {
		if len(set) == 0 {
			return nil
		}
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		return out
	}
	return &federation.PeerStreamCaps{
		Video:     asList(caps.VideoCodecs),
		Audio:     asList(caps.AudioCodecs),
		Container: asList(caps.Containers),
	}
}
