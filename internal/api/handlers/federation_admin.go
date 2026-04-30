package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/domain"
	"hubplay/internal/federation"
)

// FederationAdminHandler exposes the admin-side of federation: invite
// generation, peer pairing, peer listing, peer revocation. Every
// endpoint requires the request to come from an authenticated admin
// session — the surface here lives under the existing /api/v1/admin
// chi middleware stack.
type FederationAdminHandler struct {
	mgr    *federation.Manager
	logger *slog.Logger
}

func NewFederationAdminHandler(mgr *federation.Manager, logger *slog.Logger) *FederationAdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationAdminHandler{mgr: mgr, logger: logger.With("handler", "federation_admin")}
}

// GetServerIdentity returns this server's public-facing ServerInfo so
// the admin can read their own fingerprint to a remote admin out-of-band
// during handshake confirmation. Plug-and-play AdvertisedURL — derived
// from the admin's session if not explicitly configured.
func (h *FederationAdminHandler) GetServerIdentity(w http.ResponseWriter, r *http.Request) {
	info := h.mgr.PublicServerInfo()
	if info.AdvertisedURL == "" {
		info.AdvertisedURL = deriveURLFromRequest(r)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": infoToWire(info)})
}

// ─── Invites ────────────────────────────────────────────────────────

type inviteWire struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

// GenerateInvite mints a fresh single-use invite. The returned `code`
// is what the admin shares out-of-band with the remote admin.
func (h *FederationAdminHandler) GenerateInvite(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "unauthenticated")
		return
	}
	inv, err := h.mgr.GenerateInvite(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("federation: generate invite", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to generate invite")
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": inviteWire{
		ID:        inv.ID,
		Code:      inv.Code,
		ExpiresAt: inv.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	}})
}

// ListActiveInvites returns codes that are still usable.
func (h *FederationAdminHandler) ListActiveInvites(w http.ResponseWriter, r *http.Request) {
	invs, err := h.mgr.ListActiveInvites(r.Context())
	if err != nil {
		h.logger.Error("federation: list invites", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list invites")
		return
	}
	out := make([]inviteWire, 0, len(invs))
	for _, inv := range invs {
		out = append(out, inviteWire{
			ID:        inv.ID,
			Code:      inv.Code,
			ExpiresAt: inv.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ─── Peer pairing (we received an invite from the remote admin) ─────

type probePeerRequest struct {
	BaseURL string `json:"base_url"`
}

// ProbePeer fetches the remote's /federation/info so the admin can
// see the fingerprint before committing. Read-only on both sides.
func (h *FederationAdminHandler) ProbePeer(w http.ResponseWriter, r *http.Request) {
	var req probePeerRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	if req.BaseURL == "" {
		respondError(w, r, http.StatusBadRequest, "FEDERATION_BASE_URL_REQUIRED", "base_url required")
		return
	}
	info, err := h.mgr.ProbePeer(r.Context(), req.BaseURL)
	if err != nil {
		h.logger.Warn("federation: probe peer failed", "base_url", req.BaseURL, "err", err)
		respondError(w, r, http.StatusBadGateway, "PEER_PROBE_FAILED", "peer probe failed: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": infoToWire(info)})
}

type acceptInviteRequest struct {
	BaseURL string `json:"base_url"`
	Code    string `json:"code"`
}

// AcceptInvite executes the handshake: POSTs to the remote's
// /peer/handshake with our ServerInfo, persists the remote as a
// paired peer on success.
func (h *FederationAdminHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Code = strings.TrimSpace(req.Code)
	if req.BaseURL == "" || req.Code == "" {
		respondError(w, r, http.StatusBadRequest, "FEDERATION_REQUIRED_FIELDS_MISSING", "base_url and code required")
		return
	}
	// Plug-and-play: if the admin hasn't configured AdvertisedURL,
	// fall back to whatever URL THIS admin is currently hitting our
	// UI with. That URL works for them, so it's almost certainly a
	// reachable URL the remote peer can use too.
	fallback := deriveURLFromRequest(r)
	peer, err := h.mgr.AcceptInvite(r.Context(), req.BaseURL, req.Code, fallback)
	if err != nil {
		status, code := http.StatusBadGateway, "PEER_HANDSHAKE_FAILED"
		switch {
		case errors.Is(err, domain.ErrInviteInvalidFormat):
			status, code = http.StatusBadRequest, "INVITE_INVALID_FORMAT"
		case errors.Is(err, domain.ErrInviteExpired):
			status, code = http.StatusForbidden, "INVITE_EXPIRED"
		case errors.Is(err, domain.ErrInviteAlreadyUsed):
			status, code = http.StatusForbidden, "INVITE_ALREADY_USED"
		case errors.Is(err, domain.ErrInviteNotFound):
			status, code = http.StatusForbidden, "INVITE_NOT_FOUND"
		case errors.Is(err, domain.ErrAlreadyExists):
			status, code = http.StatusConflict, "PEER_ALREADY_PAIRED"
		}
		h.logger.Warn("federation: accept invite failed",
			"base_url", req.BaseURL, "err", err, "status", status)
		respondError(w, r, status, code, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": peerToWire(peer)})
}

// ─── Peer CRUD ──────────────────────────────────────────────────────

// ListPeers returns every peer record (including revoked, for audit).
func (h *FederationAdminHandler) ListPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := h.mgr.ListPeers(r.Context())
	if err != nil {
		h.logger.Error("federation: list peers", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list peers")
		return
	}
	out := make([]peerWire, 0, len(peers))
	for _, p := range peers {
		out = append(out, peerToWire(p))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// GetPeer returns a single peer by local UUID.
func (h *FederationAdminHandler) GetPeer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "id required")
		return
	}
	p, err := h.mgr.GetPeer(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Error("federation: get peer", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch peer")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": peerToWire(p)})
}

// ─── Library shares ─────────────────────────────────────────────────

type shareLibraryRequest struct {
	LibraryID   string `json:"library_id"`
	CanBrowse   bool   `json:"can_browse"`
	CanPlay     bool   `json:"can_play"`
	CanDownload bool   `json:"can_download"`
	CanLiveTV   bool   `json:"can_livetv"`
}

// ListShares returns every share row for a peer. Powers the per-peer
// expansion panel in the admin UI.
func (h *FederationAdminHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "id")
	if peerID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "id required")
		return
	}
	shares, err := h.mgr.ListSharesByPeer(r.Context(), peerID)
	if err != nil {
		h.logger.Error("federation: list shares", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list shares")
		return
	}
	out := make([]shareWire, 0, len(shares))
	for _, s := range shares {
		out = append(out, shareToWire(s))
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// CreateShare opts a library into being visible to a peer with the
// given scopes. Idempotent — re-calling with different scopes
// updates the existing row.
func (h *FederationAdminHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "id")
	if peerID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peer id required")
		return
	}
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "unauthenticated")
		return
	}
	var req shareLibraryRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	if req.LibraryID == "" {
		respondError(w, r, http.StatusBadRequest, "FEDERATION_LIBRARY_REQUIRED", "library_id required")
		return
	}
	share, err := h.mgr.ShareLibrary(r.Context(), peerID, req.LibraryID, claims.UserID, federation.ShareScopes{
		CanBrowse:   req.CanBrowse,
		CanPlay:     req.CanPlay,
		CanDownload: req.CanDownload,
		CanLiveTV:   req.CanLiveTV,
	})
	if err != nil {
		status, code := http.StatusInternalServerError, "INTERNAL_ERROR"
		switch {
		case errors.Is(err, domain.ErrPeerNotFound):
			status, code = http.StatusNotFound, "PEER_NOT_FOUND"
		case errors.Is(err, domain.ErrPeerUnauthorized):
			status, code = http.StatusForbidden, "PEER_NOT_PAIRED"
		}
		h.logger.Warn("federation: share library failed", "err", err)
		respondError(w, r, status, code, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"data": shareToWire(share)})
}

// DeleteShare removes a single share. Idempotent — missing share
// is treated as success because the desired state is already true.
func (h *FederationAdminHandler) DeleteShare(w http.ResponseWriter, r *http.Request) {
	peerID := chi.URLParam(r, "id")
	shareID := chi.URLParam(r, "shareID")
	if peerID == "" || shareID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "peer id and share id required")
		return
	}
	if err := h.mgr.UnshareLibrary(r.Context(), peerID, shareID); err != nil {
		h.logger.Error("federation: unshare library", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to unshare")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type shareWire struct {
	ID          string `json:"id"`
	PeerID      string `json:"peer_id"`
	LibraryID   string `json:"library_id"`
	CanBrowse   bool   `json:"can_browse"`
	CanPlay     bool   `json:"can_play"`
	CanDownload bool   `json:"can_download"`
	CanLiveTV   bool   `json:"can_livetv"`
	CreatedAt   string `json:"created_at"`
}

func shareToWire(s *federation.LibraryShare) shareWire {
	return shareWire{
		ID:          s.ID,
		PeerID:      s.PeerID,
		LibraryID:   s.LibraryID,
		CanBrowse:   s.CanBrowse,
		CanPlay:     s.CanPlay,
		CanDownload: s.CanDownload,
		CanLiveTV:   s.CanLiveTV,
		CreatedAt:   s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// RevokePeer terminates a peer. 204 on success, 404 on unknown id.
func (h *FederationAdminHandler) RevokePeer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "id required")
		return
	}
	if err := h.mgr.RevokePeer(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "PEER_NOT_FOUND", "peer not found")
			return
		}
		h.logger.Error("federation: revoke peer", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to revoke peer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── wire types ─────────────────────────────────────────────────────

// peerWire is the JSON shape of a peer for admin listings. Renders
// fingerprint server-side so the UI doesn't have to compute it.
type peerWire struct {
	ID                 string  `json:"id"`
	ServerUUID         string  `json:"server_uuid"`
	Name               string  `json:"name"`
	BaseURL            string  `json:"base_url"`
	Status             string  `json:"status"`
	Fingerprint        string  `json:"fingerprint"`
	PublicKey          string  `json:"public_key"`
	CreatedAt          string  `json:"created_at"`
	PairedAt           *string `json:"paired_at,omitempty"`
	LastSeenAt         *string `json:"last_seen_at,omitempty"`
	LastSeenStatusCode *int    `json:"last_seen_status_code,omitempty"`
	RevokedAt          *string `json:"revoked_at,omitempty"`
}

func peerToWire(p *federation.Peer) peerWire {
	wire := peerWire{
		ID:          p.ID,
		ServerUUID:  p.ServerUUID,
		Name:        p.Name,
		BaseURL:     p.BaseURL,
		Status:      string(p.Status),
		Fingerprint: p.Fingerprint(),
		PublicKey:   federation.EncodePublicKey([]byte(p.PublicKey)),
		CreatedAt:   p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if p.PairedAt != nil {
		s := p.PairedAt.UTC().Format("2006-01-02T15:04:05Z")
		wire.PairedAt = &s
	}
	if p.LastSeenAt != nil {
		s := p.LastSeenAt.UTC().Format("2006-01-02T15:04:05Z")
		wire.LastSeenAt = &s
	}
	if p.LastSeenStatusCode != nil {
		wire.LastSeenStatusCode = p.LastSeenStatusCode
	}
	if p.RevokedAt != nil {
		s := p.RevokedAt.UTC().Format("2006-01-02T15:04:05Z")
		wire.RevokedAt = &s
	}
	return wire
}

// infoWire is the JSON shape of a ServerInfo. The pubkey is base64
// encoded since BLOB doesn't survive JSON.
type infoWire struct {
	ServerUUID        string   `json:"server_uuid"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	PublicKey         string   `json:"public_key"`
	PubkeyFingerprint string   `json:"pubkey_fingerprint"`
	PubkeyWords       []string `json:"pubkey_words"`
	SupportedScopes   []string `json:"supported_scopes"`
	AdvertisedURL     string   `json:"advertised_url"`
	AdminContact      string   `json:"admin_contact,omitempty"`
}

func infoToWire(info *federation.ServerInfo) infoWire {
	return infoWire{
		ServerUUID:        info.ServerUUID,
		Name:              info.Name,
		Version:           info.Version,
		PublicKey:         federation.EncodePublicKey(info.PublicKey),
		PubkeyFingerprint: info.PubkeyFingerprint,
		PubkeyWords:       info.PubkeyWords,
		SupportedScopes:   info.SupportedScopes,
		AdvertisedURL:     info.AdvertisedURL,
		AdminContact:      info.AdminContact,
	}
}

// wireToInfo decodes the JSON shape back into a ServerInfo. Used by
// the public handshake handler when accepting an inbound POST.
func wireToInfo(w infoWire) (*federation.ServerInfo, error) {
	pub, err := federation.DecodePublicKey(w.PublicKey)
	if err != nil {
		return nil, err
	}
	return &federation.ServerInfo{
		ServerUUID:        w.ServerUUID,
		Name:              w.Name,
		Version:           w.Version,
		PublicKey:         pub,
		PubkeyFingerprint: federation.Fingerprint(pub),
		PubkeyWords:       federation.FingerprintWords(pub),
		SupportedScopes:   w.SupportedScopes,
		AdvertisedURL:     w.AdvertisedURL,
		AdminContact:      w.AdminContact,
	}, nil
}
