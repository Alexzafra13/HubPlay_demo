package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/auth"
	"hubplay/internal/domain"
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
	svc    *auth.DeviceCodeService
	mgr    baseURLProvider // for verification_url derivation; same shape used by federation
	logger *slog.Logger
}

// baseURLProvider is the small surface DeviceAuthHandler needs to
// build the verification URL it returns to the device. The federation
// manager already implements this (via a wrapper); rather than depend
// on federation here, we accept the interface directly.
type baseURLProvider interface {
	EffectiveBaseURL(r *http.Request) string
}

// NewDeviceAuthHandler wires the device-code service. The base URL
// provider is optional (nil = derive from request only).
func NewDeviceAuthHandler(svc *auth.DeviceCodeService, base baseURLProvider, logger *slog.Logger) *DeviceAuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceAuthHandler{svc: svc, mgr: base, logger: logger.With("handler", "auth_device")}
}

// ─── Start ──────────────────────────────────────────────────────────

type deviceStartRequest struct {
	DeviceName string `json:"device_name"`
}

type deviceStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`         // dash-formatted ABCD-EFGH for display
	VerificationURL string `json:"verification_url"`  // where the operator goes
	VerificationURI string `json:"verification_uri"`  // RFC 8628 alias of the above (alias kept for spec-compliance)
	ExpiresIn       int    `json:"expires_in"`        // seconds
	Interval        int    `json:"interval"`          // seconds
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
	respondJSON(w, http.StatusCreated, map[string]any{
		"data": deviceStartResponse{
			DeviceCode:      pair.DeviceCode,
			UserCode:        auth.FormatUserCodeDisplay(pair.UserCode),
			VerificationURL: pair.VerificationURL,
			VerificationURI: pair.VerificationURL,
			ExpiresIn:       int(pair.ExpiresIn.Seconds()),
			Interval:        int(pair.Interval.Seconds()),
		},
	})
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
	respondJSON(w, http.StatusOK, map[string]any{"data": tok})
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
