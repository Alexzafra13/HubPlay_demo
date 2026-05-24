package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"

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
//
// Plug-and-play AdvertisedURL: if the admin hasn't set
// HUBPLAY_SERVER_BASE_URL / server.base_url, we derive the URL from
// the inbound request itself. The peer asking "what's your URL?"
// already knows — they're hitting it. We just echo it back.
func (h *FederationPublicHandler) ServerInfo(w http.ResponseWriter, r *http.Request) {
	info := h.mgr.PublicServerInfo()
	if info.AdvertisedURL == "" {
		info.AdvertisedURL = deriveURLFromRequest(r)
	}
	respondJSON(w, http.StatusOK, infoToWire(info))
}

// pairingRequestWire es el wire format del POST inicial al inbox
// del peer. Espejo del federation.pairingRequestBody.
type pairingRequestWire struct {
	RequestID    string   `json:"request_id"`
	RequestToken string   `json:"request_token"`
	Requester    infoWire `json:"requester"`
}

// pairingCallbackWire es el wire format del POST de callback.
type pairingCallbackWire struct {
	Outcome      string   `json:"outcome"`
	RequestToken string   `json:"request_token"`
	Accepter     infoWire `json:"accepter"`
	Signature    string   `json:"signature"`
}

// pairingCancelWire es el wire format del POST de cancelacion.
type pairingCancelWire struct {
	RequestToken string `json:"request_token"`
}

// pairingRequestResponse es lo que devolvemos al sender de la
// peticion para que persista el id confirmado + el expires.
type pairingRequestResponse struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

// ReceivePairingRequest recibe el POST inicial A -> B. Persiste
// como INCOMING pending + publica evento (notification service
// fan-outea a los admins). Rate-limited por el middleware general
// del router para evitar spam de peers desconocidos.
func (h *FederationPublicHandler) ReceivePairingRequest(w http.ResponseWriter, r *http.Request) {
	var body pairingRequestWire
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	requester, err := wireToInfo(body.Requester)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUESTER", "requester info malformed")
		return
	}
	pending, err := h.mgr.HandleIncomingPairingRequest(r.Context(), body.RequestID, body.RequestToken, requester)
	if err != nil {
		if errors.Is(err, domain.ErrPairingRequestsDisabled) {
			// 403 con mensaje generico — el remoto no necesita saber
			// si es "disabled por admin" o "el server no soporta".
			respondError(w, r, http.StatusForbidden, "PAIRING_REQUESTS_DISABLED",
				"this server is not accepting pairing requests")
			return
		}
		if errors.Is(err, domain.ErrPairingRequestQuotaExceeded) {
			w.Header().Set("Retry-After", "300")
			respondError(w, r, http.StatusTooManyRequests, "QUOTA_EXCEEDED",
				"too many pending requests; retry later")
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			respondError(w, r, http.StatusConflict, "PEER_ALREADY_PAIRED", "this peer is already paired")
			return
		}
		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "request body validation failed")
			return
		}
		h.logger.Warn("federation: receive pairing request failed", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not accept the request")
		return
	}
	respondJSON(w, http.StatusAccepted, pairingRequestResponse{
		ID:        pending.ID,
		ExpiresAt: pending.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// ReceivePairingCallback recibe la respuesta de B tras accept/decline
// (B -> A). Verifica la firma Ed25519 con el pubkey pineado en step 1.
func (h *FederationPublicHandler) ReceivePairingCallback(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "id")
	if requestID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing request id")
		return
	}
	var body pairingCallbackWire
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	accepter, err := wireToInfo(body.Accepter)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_ACCEPTER", "accepter info malformed")
		return
	}
	sig, err := federation.DecodePublicKey(body.Signature)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_SIGNATURE", "signature base64 invalid")
		return
	}
	if err := h.mgr.HandlePairingCallback(r.Context(), requestID, body.Outcome, body.RequestToken, accepter, sig); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "REQUEST_NOT_FOUND", "request not found")
			return
		}
		// Cualquier otro fallo es protocolo/firma invalida.
		h.logger.Warn("federation: pairing callback rejected", "request_id", requestID, "err", err)
		respondError(w, r, http.StatusBadRequest, "CALLBACK_REJECTED", "callback validation failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReceivePairingCancel recibe la notificacion de cancelacion A -> B.
// Best-effort: si no encontramos la peticion (ya respondida, expirada,
// purgada), devolvemos 204 igualmente para que A no haga retries.
func (h *FederationPublicHandler) ReceivePairingCancel(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "id")
	if requestID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing request id")
		return
	}
	var body pairingCancelWire
	if err := decodeJSON(r, &body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	pending, err := h.mgr.GetPendingRequest(r.Context(), requestID)
	if err != nil || pending == nil {
		// Idempotente: nada que cancelar, todo OK.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if pending.Direction != federation.PendingDirectionIncoming {
		respondError(w, r, http.StatusBadRequest, "WRONG_DIRECTION", "only incoming requests can be cancelled")
		return
	}
	if pending.RequestToken != body.RequestToken {
		respondError(w, r, http.StatusForbidden, "TOKEN_MISMATCH", "request token mismatch")
		return
	}
	if pending.Status != federation.PendingStatusPending {
		// Ya respondida/expirada; idempotente.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.mgr.CancelIncomingPairingRequest(r.Context(), requestID); err != nil {
		h.logger.Warn("federation: cancel incoming pairing request failed", "id", requestID, "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not cancel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeIdentityAvatar sirve los bytes del avatar del servidor.
// Público a propósito: cualquier peer que recibe nuestro
// /federation/info ve el campo avatar_image_url apuntando aquí, y
// tiene que poder pintarlo sin firmar JWT (la foto no es secreta
// — es lo mismo que se ve en las apps del admin remoto).
//
// 404 si no hay avatar subido — el peer cae al fallback de
// color/iniciales que ya tiene en su frontend.
func (h *FederationPublicHandler) ServeIdentityAvatar(w http.ResponseWriter, r *http.Request) {
	relName := h.mgr.IdentityAvatarPath()
	if relName == "" {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "server has no avatar")
		return
	}
	full, err := h.mgr.IdentityAvatarFilePath(relName)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AVATAR_PATH", err.Error())
		return
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, r, http.StatusNotFound, "NOT_FOUND", "avatar file missing")
			return
		}
		respondError(w, r, http.StatusInternalServerError, "AVATAR_READ", err.Error())
		return
	}
	defer f.Close() //nolint:errcheck

	stat, err := f.Stat()
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AVATAR_STAT", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// Cache 5 min en cliente; cuando el admin re-sube, el path
	// (relName) cambia y los peers refetchean igualmente porque
	// la URL publicada en /federation/info incluye el nombre
	// nuevo como query param. ServeContent también pone ETag /
	// Last-Modified para revalidación.
	w.Header().Set("Cache-Control", CacheControlMediumPublic)
	http.ServeContent(w, r, relName, stat.ModTime(), f)
}

// ListLibraries returns the libraries we've shared with the calling
// peer. Server-side filtered via JOIN — a peer cannot see (or
// guess at) libraries they have no share row for. Empty array is
// the "no shares yet" case (legitimate, NOT an error).
//
// Response shape:
//
//	[
//	  {
//	    "id": "lib-uuid", "name": "Movies", "content_type": "movies",
//	    "scopes": { "can_browse": true, "can_play": true, ... }
//	  }
//	]
func (h *FederationPublicHandler) ListLibraries(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	libs, err := h.mgr.ListSharedLibrariesForPeer(r.Context(), peer.ID)
	if err != nil {
		h.logger.Error("federation: list shared libraries", "err", err, "peer_id", peer.ID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list libraries")
		return
	}
	if libs == nil {
		libs = []*federation.SharedLibrary{}
	}
	respondJSON(w, http.StatusOK, libs)
}

// ListLibraryItems returns paginated items in a shared library.
// Returns 404 if the calling peer has no share for the library —
// deliberately conflated with "library doesn't exist" so attackers
// can't enumerate library IDs.
func (h *FederationPublicHandler) ListLibraryItems(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	libraryID := chi.URLParam(r, "libraryID")
	if libraryID == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "library id required")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, total, err := h.mgr.ListSharedItems(r.Context(), peer.ID, libraryID, offset, limit)
	if err != nil {
		if errors.Is(err, domain.ErrPeerNotFound) {
			respondError(w, r, http.StatusNotFound, "LIBRARY_NOT_FOUND", "library not found")
			return
		}
		h.logger.Error("federation: list shared items", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list items")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

// SearchLibraries returns titles matching `q` from libraries the
// calling peer has CanBrowse on. Server-side ACL via the share JOIN
// — a peer cannot match titles in libraries not shared with them.
//
// GET /api/v1/peer/search?q=<query>&limit=<n>  (peer JWT required)
//
// Empty q is a 400. The repo applies a sensible upper limit so a
// pathological query cannot stream the whole catalog.
func (h *FederationPublicHandler) SearchLibraries(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "q required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := h.mgr.SearchLocalSharedItems(r.Context(), peer.ID, query, limit)
	if err != nil {
		h.logger.Error("federation: search shared items", "err", err, "peer_id", peer.ID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "search failed")
		return
	}
	if items == nil {
		items = []*federation.SharedItem{}
	}
	// Same shape as ListLibraryItems for client reuse — items + total.
	// Total here equals len(items) because search is non-paginated;
	// the limit caps it.
	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// ListRecent returns the most recently added items across every
// library the calling peer has CanBrowse on. Powers the consumer-side
// "Recently added on peers" rail. Same wire shape as ListLibraryItems
// (items + total) for trivial client reuse; each item carries its
// library_id so the consumer can route a click into the per-library
// detail view.
//
// GET /api/v1/peer/recent?limit=<n>  (peer JWT required)
func (h *FederationPublicHandler) ListRecent(w http.ResponseWriter, r *http.Request) {
	peer := federation.PeerFromContext(r.Context())
	if peer == nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "peer context missing")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := h.mgr.ListLocalRecentSharedItems(r.Context(), peer.ID, limit)
	if err != nil {
		h.logger.Error("federation: list recent shared items", "err", err, "peer_id", peer.ID)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "list recent failed")
		return
	}
	if items == nil {
		items = []*federation.SharedItem{}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
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

	// Same plug-and-play augmentation as /federation/info: if our
	// configured AdvertisedURL is empty, derive it from the inbound
	// handshake request so the responding ServerInfo carries a URL
	// the caller can use to reach us back.
	derivedURL := deriveURLFromRequest(r)
	_, ours, err := h.mgr.HandleInboundHandshake(r.Context(), req.Code, remote)
	if ours != nil && ours.AdvertisedURL == "" {
		ours.AdvertisedURL = derivedURL
	}
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
