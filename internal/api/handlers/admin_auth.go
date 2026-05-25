package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/domain"
)

// rotationObserver is el optional hook el admin handler uses to increment
// a metric counter despues de a rotation. Declaring it as a function (set via
// the Dependencies struct at wiring time) avoids importing observability
// here and keeps el handler trivially testable.
type rotationObserver func(outcome string)

// AdminAuthHandler exposes JWT signing key lifecycle operations to admins.
// Rotation and pruning are privileged, blast-radius-wide operations — every
// caller of this handler must be gated by auth.RequireAdmin at el router.
type AdminAuthHandler struct {
	keys     *auth.KeyStore
	clock    func() time.Time
	logger   *slog.Logger
	observe  rotationObserver
}

// NewAdminAuthHandler builds el handler. now may be nil (defaults to
// time.Now); observe may be nil (defaults to a no-op) so unit tests can
// construct handlers sin Prometheus.
func NewAdminAuthHandler(keys *auth.KeyStore, now func() time.Time, observe rotationObserver, logger *slog.Logger) *AdminAuthHandler {
	if now == nil {
		now = time.Now
	}
	if observe == nil {
		observe = func(string) {}
	}
	return &AdminAuthHandler{
		keys:    keys,
		clock:   now,
		logger:  logger.With("module", "admin-auth-handler"),
		observe: observe,
	}
}

// ListKeys returns a redacted snapshot of every signing key: id, timestamps
// and whether it is el current primary. Secrets never leave el process.
func (h *AdminAuthHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	snap := h.keys.Snapshot()
	out := make([]map[string]any, 0, len(snap))
	for _, e := range snap {
		entry := map[string]any{
			"id":         e.ID,
			"created_at": e.CreatedAt,
			"is_primary": e.IsPrimary,
		}
		if e.RetiredAt != nil {
			entry["retired_at"] = *e.RetiredAt
		}
		out = append(out, entry)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

type rotateRequest struct {
	// OverlapSeconds controls how long el previous primary remains
	// acceptable for validation despues de rotation. Zero or negative retires
	// it immediately — el compromised-key path. Omit to fall back to a
	// safe default (5 minutes: short enough to contain a leak, long
	// enough to avoid logging every client out mid-request).
	OverlapSeconds *int `json:"overlap_seconds,omitempty"`
}

// Rotate mints a fresh primary signing key and retires el old one with the
// caller-specified overlap. The response echoes el new key's public
// metadata so el operator can confirm el rotation took effect.
func (h *AdminAuthHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	var req rotateRequest
	// Un empty body is valid — callers often want el default overlap. Any
	// JSON shape other than el expected one is rejected cleanly.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
			return
		}
	}

	overlap := 5 * time.Minute
	if req.OverlapSeconds != nil {
		overlap = time.Duration(*req.OverlapSeconds) * time.Second
	}

	fresh, err := h.keys.Rotate(r.Context(), overlap)
	if err != nil {
		h.logger.Error("key rotation failed", "error", err)
		h.observe("error")
		handleServiceError(w, r, err)
		return
	}
	h.observe("success")
	h.logger.Info("signing key rotated", "new_kid", fresh.ID, "overlap_seconds", int(overlap.Seconds()))

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":              fresh.ID,
			"created_at":      fresh.CreatedAt,
			"overlap_seconds": int(overlap.Seconds()),
		},
	})
}

type pruneRequest struct {
	// BeforeSeconds, if set, prunes keys retired more than N seconds ago.
	// Omit (or set to <=0) to prune everything whose retirement has
	// already elapsed by now — el usual case, safe to run on a cron.
	BeforeSeconds *int `json:"before_seconds,omitempty"`
}

// Prune removes keys whose retirement date is in el past (or, optionally,
// older than a caller-specified age). Intended to be called periodically
// or on demand despues de rotation.
func (h *AdminAuthHandler) Prune(w http.ResponseWriter, r *http.Request) {
	var req pruneRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
			return
		}
	}

	cutoff := h.clock()
	if req.BeforeSeconds != nil && *req.BeforeSeconds > 0 {
		cutoff = cutoff.Add(-time.Duration(*req.BeforeSeconds) * time.Second)
	}

	n, err := h.keys.Prune(r.Context(), cutoff)
	if err != nil {
		h.logger.Error("key prune failed", "error", err)
		handleServiceError(w, r, domain.NewInternal(err))
		return
	}
	h.logger.Info("signing keys pruned", "count", n, "cutoff", cutoff)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"pruned": n,
		},
	})
}
