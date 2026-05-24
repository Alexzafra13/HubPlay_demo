// Federation stream handlers (origin side -- "peer B").
//
// When a paired peer's user clicks play on one of OUR items, that
// peer's server hits these endpoints with its peer JWT. We verify
// the item is in a library shared with the peer with the `can_play`
// scope, spawn (or attach to) a stream.Manager session keyed on the
// peer rather than a local user, and serve the resulting HLS
// manifest + segments through endpoints that key on a freshly-minted
// session UUID rather than (userID, itemID).
//
// The session UUID is what the requesting peer hands its own client.
// We never expose the underlying stream.Manager session key (which
// embeds the peer ID) outside this server.
//
// ACL: every endpoint is gated by federation.RequirePeerJWT (mounted
// in router.go) and additionally checks share.CanPlay. The session
// UUID alone is NOT an authorisation token -- the peer JWT must
// match s.PeerID on every subsequent manifest/segment request.

package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// FederationStreamHandler serves the peer-facing streaming surface.
type FederationStreamHandler struct {
	mgr          *federation.Manager
	streams      StreamManagerService
	items        ItemRepository
	mediaStreams MediaStreamRepository
	logger       *slog.Logger
}

func NewFederationStreamHandler(
	mgr *federation.Manager,
	streams StreamManagerService,
	items ItemRepository,
	mediaStreams MediaStreamRepository,
	logger *slog.Logger,
) *FederationStreamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationStreamHandler{
		mgr:          mgr,
		streams:      streams,
		items:        items,
		mediaStreams: mediaStreams,
		logger:       logger.With("handler", "federation_stream"),
	}
}

// peerStreamSessionRequestWire is the JSON body. Mirrors
// federation.PeerStreamSessionRequest -- duplicated here so the
// handlers package doesn't depend on the federation type for
// just-decoding-JSON.
type peerStreamSessionRequestWire struct {
	Profile      string `json:"profile,omitempty"`
	Capabilities *struct {
		Video     []string `json:"video,omitempty"`
		Audio     []string `json:"audio,omitempty"`
		Container []string `json:"container,omitempty"`
	} `json:"client_capabilities,omitempty"`
}

// peerUserPrefix namespaces the stream.Manager userID for federation
// sessions. Format: "peer:{peerID}". Picked so that a peer-spawned
// session NEVER collides with a local user (no local user_id is
// prefixed with "peer:") and so the operator can spot federation
// sessions in logs.
const peerUserPrefix = "peer:"

// StartSession spawns (or attaches to) a stream session for one of
// our items, on behalf of the calling peer. Returns the session UUID
// + the HLS master path the peer should fetch next.
//
// POST /api/v1/peer/stream/{itemId}/session
//
//	body: { profile?: "1080p", client_capabilities?: { video, audio, container } }
//	→ 200 { session_id, method, master_path }
func (h *FederationStreamHandler) StartSession(w http.ResponseWriter, r *http.Request) {
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

	var body peerStreamSessionRequestWire
	if r.ContentLength > 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return
		}
	}

	// ACL: item must exist AND its library must be shared with the
	// calling peer with can_play=true. Both lookups serve the same
	// 404 ("not found"), conflating "item doesn't exist" with "you're
	// not allowed to see it" so a peer can't enumerate item ids.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}
	share, err := h.mgr.GetLibraryShare(r.Context(), peer.ID, item.LibraryID)
	if err != nil {
		h.logger.Error("federation: get share for stream", "err", err, "peer_id", peer.ID, "library_id", item.LibraryID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "share lookup failed")
		return
	}
	if share == nil || !share.CanPlay {
		respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}

	// Profile defaulting + capability shaping. Empty profile is fine
	// -- stream.Decide() will pick the best variant for the caps. We
	// still pass a default ("1080p") so the cache key is stable across
	// retries within the same peer session (if the peer omits the
	// field we don't want each retry to spawn a fresh session).
	profile := body.Profile
	if profile == "" {
		profile = "1080p"
	}
	caps := capabilitiesFromWire(body.Capabilities)

	peerUserID := peerUserPrefix + peer.ID
	ms, err := h.streams.StartSession(r.Context(), stream.StartSessionRequest{
		UserID:           peerUserID,
		ItemID:           itemID,
		ProfileName:      profile,
		Caps:             caps,
		AudioStreamIndex: -1,
		BurnSubIndex:     -1,
	})
	if err != nil {
		h.logger.Warn("federation: start stream session failed", "err", err, "peer_id", peer.ID, "item_id", itemID)
		handleServiceError(w, r, err)
		return
	}

	// Register the session UUID -> (peer, item, profile) mapping. The
	// requesting peer only ever sees this UUID; the underlying
	// stream.Manager key never leaves this process.
	sess := h.mgr.RegisterPeerStreamSession(peer.ID, itemID, profile)

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id":  sess.ID,
		"method":      string(ms.Decision.Method),
		"master_path": fmt.Sprintf("/api/v1/peer/stream/session/%s/master.m3u8", sess.ID),
	})
}

// MasterPlaylist returns the HLS master playlist for a peer-spawned
// session. Variants reference paths under the same /peer/stream/session/{id}
// scope so the requesting peer's HLS client (HLS.js, AVPlayer, ExoPlayer)
// resolves them as relative URLs against the master URL it loaded -- no
// hostname rewriting required on the proxy side.
//
// GET /api/v1/peer/stream/session/{sessionId}/master.m3u8
func (h *FederationStreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	sess := h.lookupPeerSession(w, r, peer.ID)
	if sess == nil {
		return
	}

	// Verify the underlying item still exists and we can still read it
	// (defensive -- the share/item could have been revoked between
	// StartSession and the manifest fetch).
	if _, err := h.items.GetByID(r.Context(), sess.ItemID); err != nil {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "session no longer valid")
		return
	}

	// Build a peer-flavoured master playlist whose variants are
	// relative to /peer/stream/session/{sid}/. We can't use the local
	// stream.GenerateMasterPlaylist because it hardcodes the local
	// /api/v1/stream/{itemId}/{quality}/index.m3u8 path.
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	_, _ = fmt.Fprint(w, generatePeerMasterPlaylist(sess.ID))
}

// QualityPlaylist serves the per-quality manifest produced by
// stream.Manager. Same as the local handler's wait-for-file behaviour
// (ffmpeg may still be spinning up on a cold session) so the
// requesting peer's player gets the manifest as soon as it's available.
//
// GET /api/v1/peer/stream/session/{sessionId}/{quality}/index.m3u8
func (h *FederationStreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	sess := h.lookupPeerSession(w, r, peer.ID)
	if sess == nil {
		return
	}
	quality := chi.URLParam(r, "quality")
	if quality == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "quality required")
		return
	}

	// Reuse the existing stream.Manager session for this peer/item/quality.
	// StartSession is idempotent (returns the existing session if one
	// matches) so this is safe to call on every manifest request --
	// no extra ffmpeg processes spawn. caps=nil here is acceptable
	// because the session was created with the right caps in
	// StartSession; this call just hashes back to the same key.
	peerUserID := peerUserPrefix + peer.ID
	ms, err := h.streams.StartSession(r.Context(), stream.StartSessionRequest{
		UserID:           peerUserID,
		ItemID:           sess.ItemID,
		ProfileName:      quality,
		AudioStreamIndex: -1,
		BurnSubIndex:     -1,
	})
	if err != nil {
		h.logger.Warn("federation: quality playlist start session", "err", err, "peer", peer.ID, "session", sess.ID)
		handleServiceError(w, r, err)
		return
	}
	if ms.Decision.Method == stream.MethodDirectPlay {
		// DirectPlay isn't representable as an HLS manifest. The
		// initial StartSession already returned the right method to
		// the peer; if we're here it means the caller's client asked
		// for HLS anyway. 409 -- the requesting peer should follow
		// the original method response.
		respondError(w, r, http.StatusConflict, "WRONG_METHOD", "session decided direct_play; use the direct path")
		return
	}

	manifestPath := ms.ManifestPath()
	if err := waitForFile(manifestPath, 10*time.Second); err != nil {
		respondError(w, r, http.StatusServiceUnavailable, "MANIFEST_NOT_READY", "manifest still being generated")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	http.ServeFile(w, r, manifestPath)
}

// Segment serves an HLS segment .ts file for a peer-spawned session.
//
// GET /api/v1/peer/stream/session/{sessionId}/{quality}/{segment}
func (h *FederationStreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	sess := h.lookupPeerSession(w, r, peer.ID)
	if sess == nil {
		return
	}
	quality := chi.URLParam(r, "quality")
	segmentFile := chi.URLParam(r, "segment")
	if quality == "" || segmentFile == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "quality and segment required")
		return
	}
	if !validSegmentName.MatchString(segmentFile) {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment filename")
		return
	}

	peerUserID := peerUserPrefix + peer.ID
	// Federated sessions are always started with audioStreamIndex=-1
	// (see QualityPlaylist below); use the canonical key helper so
	// the format matches what Manager.StartSession registered. The
	// hand-rolled `user:item:quality` format silently misses the
	// session — every segment 404'd before this fix.
	// Federation sessions never burn-in subs (peer-to-peer streams
	// don't surface subtitle pickers cross-server today), so pass -1
	// for the burn-sub slot too.
	key := stream.SessionKey(peerUserID, sess.ItemID, quality, -1, -1)
	ms, ok := h.streams.GetSession(key)
	if !ok {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "no active transcode session")
		return
	}

	segmentPath := filepath.Join(ms.OutputDir, segmentFile)
	if filepath.Dir(segmentPath) != ms.OutputDir {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment path")
		return
	}
	if err := waitForFile(segmentPath, 30*time.Second); err != nil {
		respondError(w, r, http.StatusNotFound, "SEGMENT_NOT_FOUND", "segment not available")
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", CacheControlHourly)
	http.ServeFile(w, r, segmentPath)
}

// Subtitles lists the embedded subtitle tracks of the item backing
// a peer-spawned session. Same shape as StreamHandler.Subtitles so
// the requesting peer's frontend can reuse the local-streaming UI
// for federated playback.
//
// GET /api/v1/peer/stream/session/{sessionId}/subtitles
func (h *FederationStreamHandler) Subtitles(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	sess := h.lookupPeerSession(w, r, peer.ID)
	if sess == nil {
		return
	}
	if h.mediaStreams == nil {
		respondJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}})
		return
	}

	mediaStreams, err := h.mediaStreams.ListByItem(r.Context(), sess.ItemID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	subs := make([]map[string]any, 0, len(mediaStreams))
	for _, s := range mediaStreams {
		if s.StreamType != "subtitle" {
			continue
		}
		subs = append(subs, map[string]any{
			"index":    s.StreamIndex,
			"codec":    s.Codec,
			"language": s.Language,
			"title":    s.Title,
			"forced":   s.IsForced,
			"default":  s.IsDefault,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": subs})
}

// SubtitleTrack extracts and serves a single subtitle track as
// WebVTT for a peer-spawned session. Mirrors StreamHandler.SubtitleTrack
// so the proxy on the requesting peer can be a byte pass-through.
//
// GET /api/v1/peer/stream/session/{sessionId}/subtitles/{trackIndex}
func (h *FederationStreamHandler) SubtitleTrack(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	sess := h.lookupPeerSession(w, r, peer.ID)
	if sess == nil {
		return
	}
	trackIndex, err := strconv.Atoi(chi.URLParam(r, "trackIndex"))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "trackIndex must be an integer")
		return
	}

	item, err := h.items.GetByID(r.Context(), sess.ItemID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "session no longer valid")
		return
	}
	if item.Path == "" {
		respondError(w, r, http.StatusNotFound, "FILE_NOT_FOUND", "media file not available")
		return
	}

	vttData, err := stream.ExtractSubtitleVTT(r.Context(), item.Path, trackIndex)
	if err != nil {
		h.logger.Error("federation: subtitle extraction failed", "error", err, "peer", peer.ID, "session", sess.ID, "track", trackIndex)
		respondError(w, r, http.StatusInternalServerError, "SUBTITLE_ERROR", "failed to extract subtitle")
		return
	}

	w.Header().Set("Content-Type", "text/vtt")
	w.Header().Set("Cache-Control", CacheControlDailyOpaque)
	_, _ = io.Copy(w, vttData)
}

// lookupPeerSession resolves the session UUID from the URL and
// asserts it belongs to the calling peer. Returns nil and writes a
// 404 on any mismatch -- session not found, expired, or owned by a
// different peer all conflate to "session not found" so a malicious
// peer can't enumerate other peers' session UUIDs.
func (h *FederationStreamHandler) lookupPeerSession(w http.ResponseWriter, r *http.Request, peerID string) *federation.PeerStreamSession {
	sid := chi.URLParam(r, "sessionId")
	if sid == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "session id required")
		return nil
	}
	s := h.mgr.LookupPeerStreamSession(sid)
	if s == nil || s.PeerID != peerID {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "session not found")
		return nil
	}
	return s
}

// generatePeerMasterPlaylist mirrors stream.GenerateMasterPlaylist
// but emits relative variant URLs scoped to the federation session.
// Kept inline (rather than parametrising the stream package) because
// the scope is small and the federation surface is the only consumer.
func generatePeerMasterPlaylist(sessionID string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, name := range []string{"1080p", "720p", "480p", "360p"} {
		p, ok := stream.Profiles[name]
		if !ok {
			continue
		}
		bandwidth := stream.ParseBitrate(p.VideoBitrate) + stream.ParseBitrate(p.AudioBitrate)
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%d,NAME=\"%s\"\n",
			bandwidth, p.Width, p.Height, p.MaxFrameRate, p.Name)
		// Relative URL: the peer's HLS client resolves it against the
		// master URL it loaded (which already encodes the session id).
		fmt.Fprintf(&b, "%s/index.m3u8\n", name)
	}
	return b.String()
}

// capabilitiesFromWire maps the JSON wire shape into the
// map[string]bool sets stream.Capabilities exposes for the decoder
// lookup hot path. nil-safe: a missing block falls through as nil
// and stream.Decide() applies its conservative web-browser defaults.
func capabilitiesFromWire(c *struct {
	Video     []string `json:"video,omitempty"`
	Audio     []string `json:"audio,omitempty"`
	Container []string `json:"container,omitempty"`
}) *stream.Capabilities {
	if c == nil {
		return nil
	}
	return &stream.Capabilities{
		VideoCodecs: sliceToSet(c.Video),
		AudioCodecs: sliceToSet(c.Audio),
		Containers:  sliceToSet(c.Container),
	}
}

func sliceToSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for _, v := range in {
		out[strings.ToLower(strings.TrimSpace(v))] = true
	}
	return out
}
