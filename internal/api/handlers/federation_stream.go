package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
	"hubplay/internal/stream"
)

// FederationStreamHandler is the origin-side surface for Phase 5 —
// the handlers a remote peer hits when its user clicks play on
// content from US.
//
// Auth chain: every request below goes through RequirePeerJWT (chi
// group setup in router.go). The middleware stashes the calling
// *Peer in context, which we read for share-permission checks and
// per-peer cap accounting. No user-session auth at this layer; the
// peer's user identity travels in the session-start body.
//
// Why a separate handler from the local StreamHandler:
//   - Different URL space (/peer/stream/...).
//   - Different auth (peer JWT vs user JWT).
//   - Different identity (synthesised "rmt-..." user_id for stream.Manager).
//   - Different access check (federation share scope vs library access).
//
// What it shares with StreamHandler: stream.Manager.StartSession is
// called with the same caps + profile semantics. The decision waterfall
// (DirectPlay / DirectStream / Transcode), HW accel, and idle reaper
// all work identically — federation streams compete for the same
// transcode budget as local users.
type FederationStreamHandler struct {
	mgr     *federation.Manager
	streams StreamManagerService
	items   ItemRepoForStream
	logger  *slog.Logger

	// effectiveBaseURL resolves the "what URL should we put in the
	// master playlist for the variant URLs". Same semantics as
	// StreamHandler — runtime base_url override falls back to config.
	effectiveBaseURL func(ctx context.Context) string
}

// ItemRepoForStream is the thin slice the handler needs from the
// item repository. Declared here to keep the dependency graph
// shallow; production wires the real *db.ItemRepository.
type ItemRepoForStream interface {
	GetByID(ctx context.Context, id string) (*db.Item, error)
}

// NewFederationStreamHandler wires the dependencies. effectiveBaseURL
// is a callback rather than a string so it can read runtime settings
// at request time (the admin can change BaseURL without restarting).
func NewFederationStreamHandler(
	mgr *federation.Manager,
	streams StreamManagerService,
	items ItemRepoForStream,
	effectiveBaseURL func(ctx context.Context) string,
	logger *slog.Logger,
) *FederationStreamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationStreamHandler{
		mgr:              mgr,
		streams:          streams,
		items:            items,
		logger:           logger.With("handler", "federation_stream"),
		effectiveBaseURL: effectiveBaseURL,
	}
}

// peerStreamSessionRequest is the POST body the calling peer sends.
// Mirrors the design doc spec exactly so a future protocol bump is
// additive.
type peerStreamSessionRequest struct {
	RemoteUserID    string             `json:"remote_user_id"`
	UserDisplayName string             `json:"user_display_name,omitempty"`
	Caps            *capabilitiesWire  `json:"client_capabilities,omitempty"`
	Profile         string             `json:"profile,omitempty"`
}

// capabilitiesWire is the JSON shape of stream.Capabilities. Defined
// locally so the wire format is decoupled from the in-process struct;
// adding a field on the Go side without bumping the wire is allowed.
type capabilitiesWire struct {
	Video     []string `json:"video,omitempty"`
	Audio     []string `json:"audio,omitempty"`
	Container []string `json:"container,omitempty"`
}

// toStream converts the wire shape (slices of strings) into the
// stream package's set-shaped struct. The peer-to-peer wire is
// list-based (matches the X-Hubplay-Client-Capabilities header
// format) while internal code uses map[string]bool for O(1)
// membership checks.
func (c *capabilitiesWire) toStream() *stream.Capabilities {
	if c == nil {
		return nil
	}
	asSet := func(items []string) map[string]bool {
		if len(items) == 0 {
			return nil
		}
		out := make(map[string]bool, len(items))
		for _, v := range items {
			out[strings.ToLower(strings.TrimSpace(v))] = true
		}
		return out
	}
	return &stream.Capabilities{
		VideoCodecs: asSet(c.Video),
		AudioCodecs: asSet(c.Audio),
		Containers:  asSet(c.Container),
	}
}

type peerStreamSessionResponse struct {
	SessionID         string `json:"session_id"`
	MasterPlaylistURL string `json:"master_playlist_url"`
	Method            string `json:"method"`
	Container         string `json:"container,omitempty"`
}

// StartSession opens a stream session for the calling peer's user
// against the requested local item. Validates: item exists, library
// is shared with the peer, share has can_play scope, per-peer
// concurrency cap not exceeded.
func (h *FederationStreamHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	itemID := chi.URLParam(r, "itemId")
	if itemID == "" {
		respondError(w, r, http.StatusBadRequest, "ITEM_ID_REQUIRED", "item id required")
		return
	}

	var req peerStreamSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if strings.TrimSpace(req.RemoteUserID) == "" {
		respondError(w, r, http.StatusBadRequest, "REMOTE_USER_REQUIRED", "remote_user_id required")
		return
	}

	// Verify the item exists locally + it's in a library this peer
	// has a play-scope share for. 404 (not 403) on either failure —
	// don't leak which side of the check failed.
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
			return
		}
		h.logger.Error("federation stream: get item", "err", err, "item_id", itemID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to look up item")
		return
	}
	share, err := h.mgr.GetLibraryShareForPeer(r.Context(), peer.ID, item.LibraryID)
	if err != nil || share == nil || !share.CanPlay {
		h.logger.Warn("federation stream: peer not allowed to play item",
			"peer_id", peer.ID, "item_id", itemID,
			"library_id", item.LibraryID, "share_present", share != nil)
		respondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}

	profile := req.Profile
	if profile == "" {
		profile = "1080p" // sensible default; client can override
	}

	// Per-peer cap gate. Returns existing session for the same
	// (peer, user, item, profile) combination — keeps repeated
	// session-start calls idempotent for the same player.
	sess, ok := h.mgr.OpenPeerStream(peer.ID, req.RemoteUserID, itemID, profile)
	if !ok {
		respondError(w, r, http.StatusTooManyRequests, "PEER_STREAM_CAP",
			"too many concurrent streams from this peer")
		return
	}

	// Synthesise a stream.Manager user-id for this peer-user. Prefix
	// with "rmt-" so it can never collide with a local users.id (which
	// are UUIDs without that prefix). Including the peer.ID guards
	// against two peers sending the same remote_user_id.
	streamUserID := fmt.Sprintf("rmt-%s-%s", peer.ID, req.RemoteUserID)

	caps := req.Caps.toStream()
	ms, err := h.streams.StartSession(r.Context(), streamUserID, itemID, profile, caps, 0)
	if err != nil {
		// Couldn't allocate a session at the stream-manager level
		// (transcode-busy, file gone, etc.). Release the per-peer
		// slot so this peer doesn't burn a count on a phantom session.
		h.mgr.ClosePeerStream(sess.SessionID)
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			respondError(w, r, appErr.HTTPStatus, appErr.Code, appErr.Message)
			return
		}
		h.logger.Error("federation stream: start session", "err", err, "item_id", itemID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to start session")
		return
	}

	base := h.effectiveBaseURL(r.Context())
	masterURL := fmt.Sprintf("%s/api/v1/peer/stream/session/%s/master.m3u8",
		strings.TrimRight(base, "/"), sess.SessionID)

	respondJSON(w, http.StatusOK, peerStreamSessionResponse{
		SessionID:         sess.SessionID,
		MasterPlaylistURL: masterURL,
		Method:            string(ms.Decision.Method),
		Container:         ms.Decision.Container,
	})
}

// MasterPlaylist returns the HLS master playlist for an open peer
// session. The variant URLs inside point at session-scoped paths so
// the calling peer can rewrite them to its own /federated-stream/
// proxy without parsing the inner playlist for itemID.
func (h *FederationStreamHandler) MasterPlaylist(w http.ResponseWriter, r *http.Request) {
	sess := h.requireSession(w, r)
	if sess == nil {
		return
	}
	base := h.effectiveBaseURL(r.Context())
	playlist := generatePeerMasterPlaylist(sess.SessionID, base, []string{"1080p", "720p", "480p", "360p"})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(playlist))
}

// QualityPlaylist serves the per-quality manifest from disk —
// stream.Manager already wrote it under the synthesised user id when
// StartSession ran. Re-runs StartSession for the SAME (user, item,
// profile) key are idempotent (the manager returns the existing
// session) so a peer requesting variant before master is also OK.
func (h *FederationStreamHandler) QualityPlaylist(w http.ResponseWriter, r *http.Request) {
	sess := h.requireSession(w, r)
	if sess == nil {
		return
	}
	quality := chi.URLParam(r, "quality")
	if quality == "" {
		respondError(w, r, http.StatusBadRequest, "QUALITY_REQUIRED", "quality required")
		return
	}

	streamUserID := fmt.Sprintf("rmt-%s-%s", sess.PeerID, sess.RemoteUserID)
	ms, err := h.streams.StartSession(r.Context(), streamUserID, sess.ItemID, quality, nil, 0)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if err := waitForFile(ms.ManifestPath(), 10*time.Second); err != nil {
		handleServiceError(w, r, domain.NewTranscodePending())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, ms.ManifestPath())
}

// Segment serves a single HLS segment from the session's work dir.
// Mirrors the local stream handler's logic: validates the segment
// filename, joins it under the session's OutputDir (path traversal
// guarded by the regex AND a re-check against OutputDir), and waits
// up to 30s for ffmpeg to produce it before returning 404.
func (h *FederationStreamHandler) Segment(w http.ResponseWriter, r *http.Request) {
	sess := h.requireSession(w, r)
	if sess == nil {
		return
	}
	quality := chi.URLParam(r, "quality")
	segmentFile := chi.URLParam(r, "segment")
	if quality == "" || segmentFile == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_PARAMS", "quality and segment required")
		return
	}
	if !validSegmentName.MatchString(segmentFile) {
		respondError(w, r, http.StatusBadRequest, "INVALID_SEGMENT", "invalid segment name")
		return
	}

	streamUserID := fmt.Sprintf("rmt-%s-%s", sess.PeerID, sess.RemoteUserID)
	ms, err := h.streams.StartSession(r.Context(), streamUserID, sess.ItemID, quality, nil, 0)
	if err != nil {
		handleServiceError(w, r, err)
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
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeFile(w, r, segmentPath)
}

// StopSession releases the per-peer slot. Calling this when the session
// doesn't exist is a no-op (200) — same idempotent contract as the
// local stream stop.
func (h *FederationStreamHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "SESSION_REQUIRED", "session id required")
		return
	}
	h.mgr.ClosePeerStream(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// requireSession resolves the session by URL param and 404s when
// unknown. Centralised because four endpoints share the lookup.
func (h *FederationStreamHandler) requireSession(w http.ResponseWriter, r *http.Request) *federation.PeerStreamSession {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "SESSION_REQUIRED", "session id required")
		return nil
	}
	sess := h.mgr.GetPeerStream(sessionID)
	if sess == nil {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "stream session not found")
		return nil
	}
	// Defence in depth: the session belongs to ONE peer; reject any
	// peer that didn't open it. Without this, peer A could DELETE
	// peer B's session by guessing the session id.
	peer := federation.PeerFromContext(r.Context())
	if peer == nil || peer.ID != sess.PeerID {
		respondError(w, r, http.StatusNotFound, "SESSION_NOT_FOUND", "stream session not found")
		return nil
	}
	return sess
}

// generatePeerMasterPlaylist emits the HLS master playlist with
// variant URLs scoped to the peer-session path. Mirrors the local
// stream.GenerateMasterPlaylist but with sessionID replacing itemID
// and a different path prefix.
func generatePeerMasterPlaylist(sessionID, baseURL string, profiles []string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	base := strings.TrimRight(baseURL, "/")
	for _, name := range profiles {
		p, ok := stream.Profiles[name]
		if !ok || name == "original" {
			continue
		}
		bw := stream.ParseBitrate(p.VideoBitrate) + stream.ParseBitrate(p.AudioBitrate)
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,FRAME-RATE=%d,NAME=%q\n",
			bw, p.Width, p.Height, p.MaxFrameRate, name)
		fmt.Fprintf(&b, "%s/api/v1/peer/stream/session/%s/%s/index.m3u8\n", base, sessionID, name)
	}
	return b.String()
}
