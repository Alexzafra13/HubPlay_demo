package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
	"hubplay/internal/domain"
	"hubplay/internal/user"
)

type AuthHandler struct {
	auth   *auth.Service
	users  *user.Service
	logger *slog.Logger
}

func NewAuthHandler(authSvc *auth.Service, userSvc *user.Service, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		auth:   authSvc,
		users:  userSvc,
		logger: logger,
	}
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
	DeviceID   string `json:"device_id"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "username and password are required")
		return
	}

	if req.DeviceName == "" {
		req.DeviceName = r.UserAgent()
	}
	if req.DeviceID == "" {
		req.DeviceID = "unknown"
	}

	token, err := h.auth.Login(r.Context(), req.Username, req.Password, req.DeviceName, req.DeviceID, r.RemoteAddr)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": token})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	token, err := h.auth.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": token})
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		handleServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type registerRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	fields := make(map[string]string)
	if req.Username == "" || len(req.Username) < 3 || len(req.Username) > 32 {
		fields["username"] = "must be 3-32 characters"
	}
	if req.Password == "" || len(req.Password) < 8 {
		fields["password"] = "must be at least 8 characters"
	}
	if len(fields) > 0 {
		handleServiceError(w, domain.NewValidationError(fields))
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	u, err := h.auth.Register(r.Context(), auth.RegisterRequest{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Role:        req.Role,
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"role":         u.Role,
		},
	})
}

// Setup creates the first admin user. Only works when no users exist.
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	count, err := h.users.Count(r.Context())
	if err != nil {
		handleServiceError(w, err)
		return
	}
	if count > 0 {
		respondError(w, http.StatusForbidden, "SETUP_COMPLETED", "setup has already been completed")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	fields := make(map[string]string)
	if req.Username == "" || len(req.Username) < 3 {
		fields["username"] = "must be at least 3 characters"
	}
	if req.Password == "" || len(req.Password) < 8 {
		fields["password"] = "must be at least 8 characters"
	}
	if len(fields) > 0 {
		handleServiceError(w, domain.NewValidationError(fields))
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	u, err := h.auth.Register(r.Context(), auth.RegisterRequest{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Role:        "admin",
	})
	if err != nil {
		handleServiceError(w, err)
		return
	}

	h.logger.Info("setup completed — admin user created", "username", u.Username)

	respondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"role":         u.Role,
		},
	})
}

// ─── Error helpers (local to handlers to avoid import cycle) ───

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		respondError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, domain.ErrAlreadyExists):
		respondError(w, http.StatusConflict, "ALREADY_EXISTS", "resource already exists")
	case errors.Is(err, domain.ErrInvalidPassword):
		respondError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
	case errors.Is(err, domain.ErrUnauthorized), errors.Is(err, domain.ErrInvalidToken):
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
	case errors.Is(err, domain.ErrTokenExpired):
		respondError(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "access token has expired")
	case errors.Is(err, domain.ErrForbidden):
		respondError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
	case errors.Is(err, domain.ErrAccountDisabled):
		respondError(w, http.StatusForbidden, "ACCOUNT_DISABLED", "account is disabled")
	case errors.Is(err, domain.ErrValidation):
		var valErr *domain.ValidationError
		if errors.As(err, &valErr) {
			respondJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"code":    "VALIDATION_ERROR",
					"message": "validation failed",
					"details": map[string]any{"fields": valErr.Fields},
				},
			})
			return
		}
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "validation failed")
	case errors.Is(err, domain.ErrConflict):
		respondError(w, http.StatusConflict, "CONFLICT", "operation conflicts with current state")
	default:
		slog.Error("unhandled error", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
