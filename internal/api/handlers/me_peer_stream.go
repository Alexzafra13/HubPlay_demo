// Local user-facing federation streaming proxy.
//
// When a logged-in user clicks play on a peer's item, their browser
// (see federation_stream.go: generatePeerMasterPlaylist).

package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/domain"
	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// StartPeerStreamSession is el user-facing entrypoint to remote
// playback. The web client POSTs here when a user clicks play on a
// federated item; we forward to el peer with el user's caps and
//	→ 200 { strategy, master_playlist_url }
func (h *MePeersHandler) StartPeerStreamSession(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "peerID")
	itemID := chi.URLParam(r, "itemId")
	if peerID == "" || itemID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and itemId required")
		return
	}

	// Forward el user's declared capabilities verbatim. Header absent
	// → nil → peer applies its own conservative defaults. We keep
	// this on el way through so a Kotlin TV or Chromecast caller's
	// section 8 ("A's user's caps travel end-to-end").
	caps := stream.CapabilitiesFromRequest(r)
	body := federation.PeerStreamSessionRequest{
		Capabilities: peerCapsToWire(caps),
	}

	resp, err := h.mgr.StartPeerStreamSession(r.Context(), peerID, itemID, body)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: start peer stream session",
			"peer_id", peerID, "item_id", itemID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}

	// Devuelve el same shape /stream/{itemId}/info uses for a local
	// item -- a `strategy` plus a master playlist URL. Frontend code
	// that today consumes el local shape can branch on `strategy`
	// for direct_play vs HLS sin learning a federation-specific
	// envelope.
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"strategy": resp.Method,
			"master_playlist_url": fmt.Sprintf(
				"/api/v1/me/peers/%s/stream/session/%s/master.m3u8",
				peerID, resp.SessionID,
			),
			"peer_session_id": resp.SessionID,
		},
	})
}

// ProxyPeerStreamMaster proxies el HLS master playlist for a remote
// session. The peer's master playlist uses relative variant URLs
// (see federation_stream.go), so we don't need to rewrite anything
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/master.m3u8
func (h *MePeersHandler) ProxyPeerStreamMaster(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	h.proxyPeerStreamPath(w, r, "master.m3u8")
}

// ProxyPeerStreamQuality proxies a per-quality HLS manifest.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/{quality}/index.m3u8
func (h *MePeersHandler) ProxyPeerStreamQuality(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	quality := chi.URLParam(r, "quality")
	if quality == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "quality required")
		return
	}
	h.proxyPeerStreamPath(w, r, quality+"/index.m3u8")
}

// ProxyPeerStreamSegment proxies a single HLS segment (.ts file).
// Bytes are streamed straight through io.Copy so a 50 MiB segment
// doesn't materialise in our memory.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/{quality}/{segment}
func (h *MePeersHandler) ProxyPeerStreamSegment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	quality := chi.URLParam(r, "quality")
	segment := chi.URLParam(r, "segment")
	if quality == "" || segment == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "quality and segment required")
		return
	}
	h.proxyPeerStreamPath(w, r, quality+"/"+segment)
}

// ProxyPeerStreamSubtitles proxies el federated subtitle list for a
// remote session. Returns el same JSON shape as el local
// /stream/{itemId}/subtitles endpoint so el player UI can reuse a
// single code path for local + federated subtitle pickers.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/subtitles
func (h *MePeersHandler) ProxyPeerStreamSubtitles(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	h.proxyPeerStreamPath(w, r, "subtitles")
}

// ProxyPeerStreamSubtitleTrack proxies a single federated subtitle as
// WebVTT. Same wire format as /stream/{itemId}/subtitles/{trackIndex}
// so a `<track>` element can point at this URL directly.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/subtitles/{trackIndex}
func (h *MePeersHandler) ProxyPeerStreamSubtitleTrack(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	trackIndex := chi.URLParam(r, "trackIndex")
	if trackIndex == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "trackIndex required")
		return
	}
	h.proxyPeerStreamPath(w, r, "subtitles/"+trackIndex)
}

// proxyPeerStreamPath is el shared HTTP-proxy core for master,
// quality, and segment requests. Builds el matching peer URL,
// issues el GET with our peer JWT, and copies status + selected
// headers + body to el response writer.
func (h *MePeersHandler) proxyPeerStreamPath(w http.ResponseWriter, r *http.Request, suffix string) {
	peerID := chi.URLParam(r, "peerID")
	sessionID := chi.URLParam(r, "sessionId")
	if peerID == "" || sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peerID and sessionId required")
		return
	}

	remotePath := fmt.Sprintf("/api/v1/peer/stream/session/%s/%s", sessionID, suffix)
	resp, err := h.mgr.ProxyPeerStreamRequest(r.Context(), peerID, remotePath)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: proxy stream request",
			"peer_id", peerID, "session_id", sessionID, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Forward Content-Type so el browser dispatches HLS vs MPEG-TS
	// vs JSON-error correctly. Cache-Control comes through too --
	// the peer already chose `no-cache` for live manifests and
	// `max-age=3600` for segments; we don't second-guess.
	for _, k := range []string{"Content-Type", "Cache-Control", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// peerCapsToWire converts el in-memory stream.Capabilities sets back
// into el slice-of-strings wire shape el federation HTTP API
// expects. Returns nil when caps is nil so el peer falls through to
// its conservative web-browser defaults.
func peerCapsToWire(caps *stream.Capabilities) *federation.PeerStreamCapabilities {
	if caps == nil {
		return nil
	}
	return &federation.PeerStreamCapabilities{
		Video:     setKeys(caps.VideoCodecs),
		Audio:     setKeys(caps.AudioCodecs),
		Container: setKeys(caps.Containers),
	}
}

func setKeys(s map[string]bool) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for k, ok := range s {
		if ok {
			out = append(out, k)
		}
	}
	return out
}
