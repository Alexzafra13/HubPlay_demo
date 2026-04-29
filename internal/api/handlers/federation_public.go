package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// FederationPublicHandler exposes the unauthenticated peer-facing
// surface: GET /api/v1/federation/info and POST /api/v1/peer/handshake.
//
// Neither endpoint requires a session — the handshake is authenticated
// by the (random, single-use, time-bounded) invite code carried in the
// request body. After pairing, all subsequent peer-to-peer calls use
// Ed25519-signed JWTs (Phase 2, separate middleware).
//
// Response shapes are deliberately UN-wrapped (no {"data": ...}
// envelope) because peer-to-peer consumers expect the bare object.
// Admin handlers in the same package wrap; this surface does not.
type FederationPublicHandler struct {
	mgr    *federation.Manager
	logger *slog.Logger
}

func NewFederationPublicHandler(mgr *federation.Manager, logger *slog.Logger) *FederationPublicHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationPublicHandler{mgr: mgr, logger: logger.With("handler", "federation_public")}
}

// ServerInfo returns this server's identity surface. Public — anyone
// who can reach the URL can read it. The pubkey + fingerprint are
// non-secret by design.
func (h *FederationPublicHandler) ServerInfo(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, infoToWire(h.mgr.PublicServerInfo()))
}

// Ping is the canonical authenticated peer-to-peer endpoint. A peer
// presenting a valid Ed25519-signed JWT (validated by RequirePeerJWT
// middleware) hits this to verify the link is alive end-to-end.
//
// Response is the minimum signal the peer needs: OUR server_uuid + a
// timestamp. The peer compares the server_uuid against what it pinned
// at handshake — divergence means the peer is talking to a different
// server than the one it paired with.
func (h *FederationPublicHandler) Ping(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		// Should be impossible — middleware sets this. Fail closed.
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"server_uuid":      h.mgr.PublicServerInfo().ServerUUID,
		"now":              h.mgr.NowUTC().Format("2006-01-02T15:04:05Z"),
		"acknowledged_to":  peer.ServerUUID,
	})
}

// handshakeRequestWire mirrors the manager's request shape but uses
// the JSON-serialisable infoWire so base64 pubkeys round-trip.
type handshakeRequestWire struct {
	Code       string   `json:"code"`
	RemoteInfo infoWire `json:"remote_info"`
}

// Handshake is called BY a remote server when their admin pasted our
// invite code into their UI. We validate the code, persist them as a
// paired peer, mark the invite consumed, and return our own ServerInfo
// so they can persist us.
func (h *FederationPublicHandler) Handshake(w http.ResponseWriter, r *http.Request) {
	var req handshakeRequestWire
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.Code == "" {
		respondError(w, r, http.StatusBadRequest, "FEDERATION_CODE_REQUIRED", "code required")
		return
	}
	remote, err := wireToInfo(req.RemoteInfo)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "FEDERATION_REMOTE_INFO_INVALID", "invalid remote_info: "+err.Error())
		return
	}

	_, ours, err := h.mgr.HandleInboundHandshake(r.Context(), req.Code, remote)
	if err != nil {
		status, code := http.StatusInternalServerError, "INTERNAL_ERROR"
		switch {
		case errors.Is(err, domain.ErrInviteInvalidFormat):
			status, code = http.StatusBadRequest, "INVITE_INVALID_FORMAT"
		case errors.Is(err, domain.ErrValidation):
			status, code = http.StatusBadRequest, "VALIDATION_FAILED"
		case errors.Is(err, domain.ErrInviteNotFound):
			status, code = http.StatusForbidden, "INVITE_NOT_FOUND"
		case errors.Is(err, domain.ErrInviteExpired):
			status, code = http.StatusForbidden, "INVITE_EXPIRED"
		case errors.Is(err, domain.ErrInviteAlreadyUsed):
			status, code = http.StatusForbidden, "INVITE_ALREADY_USED"
		case errors.Is(err, domain.ErrAlreadyExists):
			status, code = http.StatusConflict, "PEER_ALREADY_PAIRED"
		}
		userMsg := "handshake failed"
		if status >= 400 && status < 500 {
			userMsg = err.Error()
		}
		h.logger.Warn("federation: inbound handshake failed", "status", status, "err", err)
		respondError(w, r, status, code, userMsg)
		return
	}
	// Bare ServerInfo (no envelope) for peer-to-peer compatibility.
	respondJSON(w, http.StatusOK, infoToWire(ours))
}
