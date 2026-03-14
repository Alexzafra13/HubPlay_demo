package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/user"
)

type UserHandler struct {
	users  *user.Service
	logger *slog.Logger
}

func NewUserHandler(users *user.Service, logger *slog.Logger) *UserHandler {
	return &UserHandler{
		users:  users,
		logger: logger,
	}
}

// Me returns the currently authenticated user's profile.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	u, err := h.users.GetByID(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":            u.ID,
			"username":      u.Username,
			"display_name":  u.DisplayName,
			"role":          u.Role,
			"is_active":     u.IsActive,
			"created_at":    u.CreatedAt,
			"last_login_at": u.LastLoginAt,
		},
	})
}

// List returns all users (admin only).
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	users, total, err := h.users.List(r.Context(), limit, offset)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	items := make([]map[string]any, len(users))
	for i, u := range users {
		items[i] = map[string]any{
			"id":            u.ID,
			"username":      u.Username,
			"display_name":  u.DisplayName,
			"role":          u.Role,
			"is_active":     u.IsActive,
			"created_at":    u.CreatedAt,
			"last_login_at": u.LastLoginAt,
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data":  items,
		"total": total,
	})
}

// Delete removes a user by ID (admin only).
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}

	// Prevent self-deletion
	claims := auth.GetClaims(r.Context())
	if claims != nil && claims.UserID == id {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "cannot delete your own account")
		return
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
