package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
	"hubplay/internal/db"

	"github.com/go-chi/chi/v5"
)

// PreferencesHandler exposes the per-user key/value preference store
// at /api/v1/me/preferences. Keys and values are opaque strings the
// backend doesn't interpret, letting frontend hooks own the encoding.
//
// Scoped to the authenticated user — no endpoint reads or writes
// another user's preferences. Admin tooling that needs it queries
// the DB directly.
type PreferencesHandler struct {
	repo   UserPreferencesRepo
	logger *slog.Logger
}

// UserPreferencesRepo is the repo surface the handler needs. Kept
// local so tests can pass a fake without pulling in the real db
// package, matching every other handler in this package.
type UserPreferencesRepo interface {
	ListByUser(ctx context.Context, userID string) ([]db.UserPreference, error)
	Set(ctx context.Context, userID, key, value string) (*db.UserPreference, error)
	Delete(ctx context.Context, userID, key string) error
}

func NewPreferencesHandler(repo UserPreferencesRepo, logger *slog.Logger) *PreferencesHandler {
	return &PreferencesHandler{
		repo:   repo,
		logger: logger.With("module", "preferences-handler"),
	}
}

// ListMine returns a flat map of the caller's preferences. Missing
// keys are simply absent from the map — there's no "null means
// default" cleverness; each frontend hook handles its own defaults.
func (h *PreferencesHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	rows, err := h.repo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	out := make(map[string]string, len(rows))
	for _, p := range rows {
		out[p.Key] = p.Value
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

type setPreferenceRequest struct {
	Value string `json:"value"`
}

// SetMine upserts one key. Value is opaque — the handler persists
// whatever string the caller sends, bounded by a sane size cap.
func (h *PreferencesHandler) SetMine(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" || len(key) > 128 {
		respondError(w, r, http.StatusBadRequest, "INVALID_KEY", "key must be 1-128 chars")
		return
	}

	var body setPreferenceRequest
	// 16 KB is generous for UI state and stops a rogue client from
	// stuffing megabytes into a preference row.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if len(body.Value) > 8*1024 {
		respondError(w, r, http.StatusBadRequest, "VALUE_TOO_LARGE", "value must be ≤ 8 KB")
		return
	}

	pref, err := h.repo.Set(r.Context(), claims.UserID, key, body.Value)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"key": pref.Key, "value": pref.Value},
	})
}

// DeleteMine clears one key. Idempotent.
func (h *PreferencesHandler) DeleteMine(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		respondError(w, r, http.StatusBadRequest, "INVALID_KEY", "key required")
		return
	}
	if err := h.repo.Delete(r.Context(), claims.UserID, key); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
