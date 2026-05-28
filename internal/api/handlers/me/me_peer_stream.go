// Local user-facing federation streaming proxy.
//
// When a logged-in user clicks play on a peer's item, their browser
// hits these endpoints (under /api/v1/me/peers/{peerID}/stream/...).
// We translate each request into a server-to-server peer call (with
// our peer JWT) and stream the bytes back. The user's browser only
// ever talks to OUR server -- the peer's hostname, peer JWT, and
// CORS posture stay invisible to the client.
//
// Why this proxy and not a redirect? Two practical reasons:
//
//   1. Auth: the user's browser only carries OUR session cookie. The
//      peer JWT is server-side state. A redirect would mean the
//      browser tries to load the peer URL with no usable
//      Authorization header.
//   2. CSP / CORS / privacy: keeping all traffic same-origin lets
//      the operator lock `connect-src 'self'` without exceptions
//      per peer, and the peer never sees the user's IP / UA.
//
// HLS manifests use relative URLs, so once the master playlist is
// loaded from /api/v1/me/peers/{peerID}/stream/session/{sid}/master.m3u8
// the player resolves all subsequent requests (quality manifests,
// segments) against that prefix and they come back through this
// proxy automatically. No URL rewriting required on our side -- the
// peer's master playlist is shaped to use relative variants too
// (see federation_stream.go: generatePeerMasterPlaylist).

package me

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// StartPeerStreamSession is the user-facing entrypoint to remote
// playback. The web client POSTs here when a user clicks play on a
// federated item; we forward to the peer with the user's caps and
// return a same-origin master playlist URL the player can then load.
//
// POST /api/v1/me/peers/{peerID}/stream/{itemId}/session
//
//	header: X-Hubplay-Client-Capabilities (optional)
//	→ 200 { strategy, master_playlist_url }
func (h *MePeersHandler) StartPeerStreamSession(w http.ResponseWriter, r *http.Request) {
	peerID := handlers.RequireParam(w, r, "peerID")
	if peerID == "" {
		return
	}
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}

	// Forward the user's declared capabilities verbatim. Header absent
	// → nil → peer applies its own conservative defaults. We keep
	// this on the way through so a Kotlin TV or Chromecast caller's
	// caps reach the peer's stream.Decide() unchanged, mirroring the
	// guarantee documented in docs/architecture/federation.md
	// section 8 ("A's user's caps travel end-to-end").
	caps := stream.CapabilitiesFromRequest(r)
	body := federation.PeerStreamSessionRequest{
		Capabilities: peerCapsToWire(caps),
	}

	resp, err := h.mgr.StartPeerStreamSession(r.Context(), peerID, itemID, body)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			handlers.RespondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: start peer stream session",
			"peer_id", peerID, "item_id", itemID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}

	// Return the same shape /stream/{itemId}/info uses for a local
	// item -- a `strategy` plus a master playlist URL. Frontend code
	// that today consumes the local shape can branch on `strategy`
	// for direct_play vs HLS without learning a federation-specific
	// envelope.
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
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

// ProxyPeerStreamMaster proxies the HLS master playlist for a remote
// session. The peer's master playlist uses relative variant URLs
// (see federation_stream.go), so we don't need to rewrite anything
// -- pass the bytes through and the player resolves variants against
// THIS endpoint's URL, which keeps subsequent fetches same-origin.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/master.m3u8
func (h *MePeersHandler) ProxyPeerStreamMaster(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	h.proxyPeerStreamPath(w, r, "master.m3u8")
}

// ProxyPeerStreamQuality proxies a per-quality HLS manifest.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/{quality}/index.m3u8
func (h *MePeersHandler) ProxyPeerStreamQuality(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
	quality := handlers.RequireParam(w, r, "quality")
	if quality == "" {
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
	_ = handlers.DisableWriteDeadline(w)
	quality := handlers.RequireParam(w, r, "quality")
	if quality == "" {
		return
	}
	segment := handlers.RequireParam(w, r, "segment")
	if segment == "" {
		return
	}
	h.proxyPeerStreamPath(w, r, quality+"/"+segment)
}

// ProxyPeerStreamSubtitles proxies the federated subtitle list for a
// remote session. Returns the same JSON shape as the local
// /stream/{itemId}/subtitles endpoint so the player UI can reuse a
// single code path for local + federated subtitle pickers.
//
// GET /api/v1/me/peers/{peerID}/stream/session/{sessionId}/subtitles
func (h *MePeersHandler) ProxyPeerStreamSubtitles(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = handlers.DisableWriteDeadline(w)
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
	_ = handlers.DisableWriteDeadline(w)
	trackIndex := handlers.RequireParam(w, r, "trackIndex")
	if trackIndex == "" {
		return
	}
	h.proxyPeerStreamPath(w, r, "subtitles/"+trackIndex)
}

// proxyPeerStreamPath is the shared HTTP-proxy core for master,
// quality, and segment requests. Builds the matching peer URL,
// issues the GET with our peer JWT, and copies status + selected
// headers + body to the response writer.
func (h *MePeersHandler) proxyPeerStreamPath(w http.ResponseWriter, r *http.Request, suffix string) {
	peerID := handlers.RequireParam(w, r, "peerID")
	if peerID == "" {
		return
	}
	sessionID := handlers.RequireParam(w, r, "sessionId")
	if sessionID == "" {
		return
	}

	remotePath := fmt.Sprintf("/api/v1/peer/stream/session/%s/%s", sessionID, suffix)
	resp, err := h.mgr.ProxyPeerStreamRequest(r.Context(), peerID, remotePath)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			handlers.RespondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Warn("federation: proxy stream request",
			"peer_id", peerID, "session_id", sessionID, "error", err)
		handlers.RespondError(w, r, http.StatusBadGateway, "PEER_UNREACHABLE", err.Error())
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	// Forward Content-Type so the browser dispatches HLS vs MPEG-TS
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

// peerCapsToWire converts the in-memory stream.Capabilities sets back
// into the slice-of-strings wire shape the federation HTTP API
// expects. Returns nil when caps is nil so the peer falls through to
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
