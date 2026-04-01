package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
)

const (
	accessCookieName  = "hubplay_access"
	refreshCookieName = "hubplay_refresh"
)

func authTokenResponse(token *auth.AuthToken, u *db.User) map[string]any {
	return map[string]any{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expires_at":    token.ExpiresAt,
		"user": map[string]any{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"role":         u.Role,
			"created_at":   u.CreatedAt,
		},
	}
}

type AuthHandler struct {
	auth    AuthService
	users   UserService
	authCfg config.AuthConfig
	logger  *slog.Logger
}

func NewAuthHandler(authSvc AuthService, userSvc UserService, authCfg config.AuthConfig, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		auth:    authSvc,
		users:   userSvc,
		authCfg: authCfg,
		logger:  logger,
	}
}

// setAuthCookies sets HTTP-only cookies for access and refresh tokens.
func setAuthCookies(w http.ResponseWriter, token *auth.AuthToken, accessTTL, refreshTTL int) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    token.AccessToken,
		Path:     "/api/v1",
		MaxAge:   accessTTL,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token.RefreshToken,
		Path:     "/api/v1/auth",
		MaxAge:   refreshTTL,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearAuthCookies removes auth cookies by setting them expired.
func clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/api/v1",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
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

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	setAuthCookies(w, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	respondJSON(w, http.StatusOK, map[string]any{"data": authTokenResponse(token, u)})
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

	// Fall back to HTTP-only cookie if body is empty.
	if req.RefreshToken == "" {
		if c, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = c.Value
		}
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

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	setAuthCookies(w, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	respondJSON(w, http.StatusOK, map[string]any{"data": authTokenResponse(token, u)})
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Body may be empty when relying solely on cookies.
		req = logoutRequest{}
	}

	// Fall back to HTTP-only cookie.
	if req.RefreshToken == "" {
		if c, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = c.Value
		}
	}

	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		handleServiceError(w, err)
		return
	}

	clearAuthCookies(w)
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

	// Auto-login the new admin user
	token, err := h.auth.Login(r.Context(), req.Username, req.Password, r.UserAgent(), "setup", r.RemoteAddr)
	if err != nil {
		handleServiceError(w, err)
		return
	}

	setAuthCookies(w, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	respondJSON(w, http.StatusCreated, map[string]any{"data": authTokenResponse(token, u)})
}

