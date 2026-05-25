package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// DeviceAuthHandler implements the device authorization grant
// endpoints (RFC 8628). Three routes:
//
//   POST /api/v1/auth/device/start    — unauthenticated (anyone with
//                                       network access can request a code)
//   POST /api/v1/auth/device/poll     — unauthenticated (the device polls)
//   POST /api/v1/auth/device/approve  — authenticated (an operator
//                                       on a logged-in browser approves)
//
// The pattern Netflix / Spotify / YouTube TV use: the device cannot
// type a password (TV remote, headless service), so it asks the
// server to mint a short user_code, displays it, and polls. The
// operator types the code on a phone and approves. The device's
// next poll receives access + refresh tokens.
type DeviceAuthHandler struct {
	svc     *auth.DeviceCodeService
	mgr     baseURLProvider // for verification_url derivation; same shape used by federation
	authCfg config.AuthConfig
	bus     EventBusSubscriber // optional; nil disables the SSE Events endpoint
	limiter *SSELimiter        // optional; same instance shared with /events and /me/events
	logger  *slog.Logger
}

// baseURLProvider is the small surface DeviceAuthHandler needs to
// build the verification URL it returns to the device. The federation
// manager already implements this (via a wrapper); rather than depend
// on federation here, we accept the interface directly.
type baseURLProvider interface {
	EffectiveBaseURL(r *http.Request) string
}

// NewDeviceAuthHandler wires the device-code service. The base URL
// provider is optional (nil = derive from request only). authCfg
// drives the cookie TTLs that Poll sets when the caller is a browser
// (zero value disables cookie issuance — handy for tests). bus +
// limiter are optional; when bus is nil the SSE Events endpoint is
// not functional and the router skips registering it.
func NewDeviceAuthHandler(svc *auth.DeviceCodeService, base baseURLProvider, authCfg config.AuthConfig, bus EventBusSubscriber, limiter *SSELimiter, logger *slog.Logger) *DeviceAuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceAuthHandler{
		svc:     svc,
		mgr:     base,
		authCfg: authCfg,
		bus:     bus,
		limiter: limiter,
		logger:  logger.With("handler", "auth_device"),
	}
}

// HasEventBus reports whether the SSE Events endpoint can be wired by
// the router. Returns false when no bus was injected (e.g. unit tests
// that exercise Start/Poll/Approve only).
func (h *DeviceAuthHandler) HasEventBus() bool { return h.bus != nil }

// ─── Start ──────────────────────────────────────────────────────────

type deviceStartRequest struct {
	DeviceName string `json:"device_name"`
}

type deviceStartResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`                // dash-formatted ABCD-EFGH for display
	VerificationURL         string `json:"verification_url"`         // where the operator goes
	VerificationURI         string `json:"verification_uri"`         // RFC 8628 alias of the above (alias kept for spec-compliance)
	VerificationURIComplete string `json:"verification_uri_complete"` // RFC 8628 §3.3.1: URL with the user_code pre-filled (QR-friendly)
	ExpiresIn               int    `json:"expires_in"`               // seconds
	Interval                int    `json:"interval"`                 // seconds
}

// Start mints a fresh code pair. Body: { "device_name": "..." }.
// Response shape matches RFC 8628 §3.2 with one extension
// (verification_url alongside verification_uri for older client libs).
func (h *DeviceAuthHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req deviceStartRequest
	if err := decodeJSON(r, &req); err != nil {
		// Empty body is fine — device_name is optional.
		req = deviceStartRequest{}
	}
	verURL := h.deriveVerificationURL(r)

	pair, err := h.svc.StartDevice(r.Context(), req.DeviceName, verURL)
	if err != nil {
		h.logger.Error("device start failed", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to start device flow")
		return
	}
	displayCode := auth.FormatUserCodeDisplay(pair.UserCode)
	complete := buildVerificationURIComplete(pair.VerificationURL, displayCode)
	respondJSON(w, http.StatusCreated, map[string]any{
		"data": deviceStartResponse{
			DeviceCode:              pair.DeviceCode,
			UserCode:                displayCode,
			VerificationURL:         pair.VerificationURL,
			VerificationURI:         pair.VerificationURL,
			VerificationURIComplete: complete,
			ExpiresIn:               int(pair.ExpiresIn.Seconds()),
			Interval:                int(pair.Interval.Seconds()),
		},
	})
}

// buildVerificationURIComplete returns the verification URL with the
// user_code attached as a query parameter, ready for QR encoding. The
// /link page's canonicalise() strips dashes, so the dashed display
// form is fine to embed verbatim (RFC 8628 §3.3.1 explicitly allows
// either form). When the base URL is missing we still emit something
// usable rather than a bare "?code=" — the /link page resolves
// relative URLs in the browser regardless.
func buildVerificationURIComplete(verificationURL, displayCode string) string {
	base := strings.TrimRight(verificationURL, "?&")
	if displayCode == "" {
		return base
	}
	if strings.Contains(base, "?") {
		return base + "&code=" + displayCode
	}
	return base + "?code=" + displayCode
}

// ─── Poll ───────────────────────────────────────────────────────────

type devicePollRequest struct {
	DeviceCode string `json:"device_code"`
}

// Poll exchanges a device_code for tokens, OR returns one of the RFC
// 8628 protocol errors (authorization_pending, slow_down, expired_token,
// access_denied). Response codes follow the spec (400 with error JSON
// for protocol errors, 200 + tokens for success).
func (h *DeviceAuthHandler) Poll(w http.ResponseWriter, r *http.Request) {
	var req devicePollRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	req.DeviceCode = strings.TrimSpace(req.DeviceCode)
	if req.DeviceCode == "" {
		respondError(w, r, http.StatusBadRequest, "DEVICE_CODE_REQUIRED", "device_code required")
		return
	}

	tok, err := h.svc.PollDevice(r.Context(), req.DeviceCode, r.RemoteAddr)
	if err != nil {
		h.writePollError(w, r, err)
		return
	}
	// Issue cookies on success so a browser-side poll (the in-app
	// "Vincular este dispositivo" pairing UI on a TV) gets logged in
	// without exposing tokens to JS. Native clients (TV apps, CLI)
	// keep reading access_token / refresh_token from the JSON body and
	// just ignore the cookies. Zero-valued authCfg disables cookies —
	// useful in tests and headless setups that explicitly opt out.
	if h.authCfg.AccessTokenTTL > 0 && h.authCfg.RefreshTokenTTL > 0 {
		setAuthCookies(w, r, tok,
			int(h.authCfg.AccessTokenTTL.Seconds()),
			int(h.authCfg.RefreshTokenTTL.Seconds()))
	}
	respondData(w, http.StatusOK, tok)
}

// writePollError maps the device-code service errors to the RFC 8628
// protocol error codes. Spec-compliant clients dispatch on the `error`
// string in the body, not the HTTP status (which is always 400 for
// protocol-level pending/slowdown).
func (h *DeviceAuthHandler) writePollError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrAuthorizationPending):
		respondError(w, r, http.StatusBadRequest, "authorization_pending",
			"the user has not yet approved the device")
	case errors.Is(err, auth.ErrSlowDown):
		respondError(w, r, http.StatusBadRequest, "slow_down",
			"polling too frequently; back off and retry")
	case errors.Is(err, domain.ErrTokenExpired):
		respondError(w, r, http.StatusBadRequest, "expired_token",
			"the device code has expired or already been used")
	case errors.Is(err, domain.ErrNotFound):
		respondError(w, r, http.StatusBadRequest, "expired_token",
			"unknown device_code")
	case errors.Is(err, domain.ErrAccessExpired):
		respondError(w, r, http.StatusBadRequest, "access_denied",
			"the approving user's temporary access window has expired")
	case errors.Is(err, domain.ErrAccountDisabled):
		respondError(w, r, http.StatusBadRequest, "access_denied",
			"the approving user account is disabled")
	default:
		h.logger.Error("device poll failed unexpectedly", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "poll failed")
	}
}

// ─── Approve ────────────────────────────────────────────────────────

type deviceApproveRequest struct {
	UserCode string `json:"user_code"`
}

// Approve binds the calling user's session to a pending device_code
// row. The auth middleware that gates this route guarantees the
// caller is a logged-in user.
func (h *DeviceAuthHandler) Approve(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "AUTH_REQUIRED", "unauthenticated")
		return
	}
	var req deviceApproveRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	req.UserCode = strings.TrimSpace(req.UserCode)
	if req.UserCode == "" {
		respondError(w, r, http.StatusBadRequest, "USER_CODE_REQUIRED", "user_code required")
		return
	}

	if err := h.svc.ApproveDevice(r.Context(), req.UserCode, claims.UserID); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			respondError(w, r, http.StatusNotFound, "USER_CODE_UNKNOWN",
				"user code not recognised — check the code and try again")
		case errors.Is(err, domain.ErrTokenExpired):
			respondError(w, r, http.StatusGone, "USER_CODE_EXPIRED",
				"user code has expired — ask the device to start a new one")
		case errors.Is(err, domain.ErrAlreadyExists):
			respondError(w, r, http.StatusConflict, "USER_CODE_ALREADY_APPROVED",
				"this code was already approved by a different user")
		case errors.Is(err, domain.ErrAccessExpired):
			respondError(w, r, http.StatusForbidden, "ACCESS_EXPIRED",
				"your temporary access window has expired — contact the admin to extend it")
		case errors.Is(err, domain.ErrAccountDisabled):
			respondError(w, r, http.StatusForbidden, "ACCOUNT_DISABLED",
				"your account is disabled")
		default:
			h.logger.Error("device approve failed", "err", err)
			respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "approve failed")
		}
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"approved": true},
	})
}

// ─── Events (SSE) ───────────────────────────────────────────────────

// Events streams the lifecycle of a single device_code as Server-Sent
// Events so the browser-side pairing UI (the QR + user_code screen on
// a TV running HubPlay in a tab) reacts instantly to approval instead
// of polling /auth/device/poll on a timer. The /poll endpoint stays
// the single source of truth for token issuance — a client that sees
// the "approved" event still calls /poll exactly once to swap the
// device_code for tokens.
//
// Wire (text/event-stream):
//
//	event: pending      data: {"device_code":"..."}    // initial state
//	event: approved     data: {"device_code":"..."}    // call /poll now
//	event: consumed     data: {}                       // someone else polled
//	event: expired      data: {}                       // 10-min TTL elapsed
//	: ping                                              // keepalive every 25s
//
// Auth: the opaque `device_code` query parameter is the secret — same
// threat model as POST /auth/device/poll. No bearer required.
func (h *DeviceAuthHandler) Events(w http.ResponseWriter, r *http.Request) {
	// Streaming endpoint: opt-out del WriteTimeout 30s global
	// (cierre olor Q). El segmento puede tardar > 30s con HW accel cold-start.
	_ = DisableWriteDeadline(w)
	if h.bus == nil {
		respondError(w, r, http.StatusServiceUnavailable, "SSE_UNAVAILABLE",
			"event stream not wired")
		return
	}
	deviceCode := strings.TrimSpace(r.URL.Query().Get("device_code"))
	if deviceCode == "" {
		respondError(w, r, http.StatusBadRequest, "DEVICE_CODE_REQUIRED",
			"device_code query parameter required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "SSE_NOT_SUPPORTED",
			"streaming not supported")
		return
	}

	// Verify the code exists BEFORE flushing headers so a typo lands
	// as a clean 404 instead of an empty stream the client has to
	// interpret. InspectDevice does not bump LastPolledAt — it is a
	// read-only inspector and safe to call on every connect.
	initial, err := h.svc.InspectDevice(r.Context(), deviceCode)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "DEVICE_CODE_UNKNOWN",
				"unknown device_code")
			return
		}
		h.logger.Error("device events inspect failed", "err", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to start event stream")
		return
	}

	// Acquire the SSE slot. Limiter is keyed by the device_code so a
	// runaway client cannot stack streams for the same pairing, while
	// different pairings stay independent. Unauthenticated, so we
	// cannot key by user_id like /me/events does.
	if h.limiter != nil {
		release, err := h.limiter.Acquire("device:" + deviceCode)
		if err != nil {
			w.Header().Set("Retry-After", "30")
			respondError(w, r, http.StatusServiceUnavailable, "SSE_CAP_EXCEEDED",
				"too many concurrent event streams; retry shortly")
			return
		}
		defer release()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", CacheControlNoCache)
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Synthetic terminal events: if the row is already in a terminal
	// state when the client connects (raced the approve, reconnected
	// after a network blip), emit the matching event and close. The
	// client interprets it identically to a live push.
	switch initial.Status {
	case "approved":
		writeSSE(w, "approved", map[string]any{"device_code": deviceCode})
		flusher.Flush()
		return
	case "consumed":
		writeSSE(w, "consumed", map[string]any{})
		flusher.Flush()
		return
	case "expired":
		writeSSE(w, "expired", map[string]any{})
		flusher.Flush()
		return
	}

	// Pending: subscribe to the bus and wait. Buffer of 4 is plenty
	// for a stream that emits at most one terminal event before
	// closing — the extra slots cover bus chatter the filter drops.
	eventCh := make(chan event.Event, 4)
	unsub := h.bus.Subscribe(event.DeviceCodeApproved, func(e event.Event) {
		if e.Data == nil {
			return
		}
		if dc, _ := e.Data["device_code"].(string); dc != deviceCode {
			return
		}
		select {
		case eventCh <- e:
		default:
		}
	})
	defer unsub()

	// Local expiry timer so we close the stream the moment the row's
	// TTL elapses rather than waiting for /poll to surface the error.
	ttl := time.Until(initial.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Second
	}
	expiryTimer := time.NewTimer(ttl)
	defer expiryTimer.Stop()

	writeSSE(w, "pending", map[string]any{"device_code": deviceCode})
	flusher.Flush()

	h.logger.Info("device events client connected",
		"device_code_prefix", deviceCode[:8], "remote_addr", r.RemoteAddr)

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-expiryTimer.C:
			writeSSE(w, "expired", map[string]any{})
			flusher.Flush()
			return
		case <-eventCh:
			writeSSE(w, "approved", map[string]any{"device_code": deviceCode})
			flusher.Flush()
			return
		}
	}
}

// writeSSE emits a single event in the text/event-stream wire format.
// Encoding errors are swallowed — the connection will close on the
// next flush failure anyway and the SSE protocol has no way to signal
// "this one event failed, try again" mid-stream.
func writeSSE(w http.ResponseWriter, name string, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, body)
}

// ─── Helpers ────────────────────────────────────────────────────────

// deriveVerificationURL builds the URL we tell the device to send its
// operator to. Priority:
//
//  1. baseURL provider's effective URL (admin-set or runtime-derived).
//  2. Inbound request's deduced URL (deriveURLFromRequest).
//
// The path "/link" is the user-facing route the SPA serves.
func (h *DeviceAuthHandler) deriveVerificationURL(r *http.Request) string {
	base := ""
	if h.mgr != nil {
		base = h.mgr.EffectiveBaseURL(r)
	}
	if base == "" {
		base = deriveURLFromRequest(r)
	}
	if base == "" {
		return "/link"
	}
	return strings.TrimRight(base, "/") + "/link"
}
