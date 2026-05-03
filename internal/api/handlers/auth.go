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

// cookieSecure decides whether the Secure flag should be set on
// auth cookies for the current request. Returns true when the
// connection looks TLS-protected: either net/http resolved a TLS
// connection state, or a reverse proxy in front of us forwarded the
// original scheme via X-Forwarded-Proto.
//
// On plain http://localhost (the default dev / docker-compose
// setup) we MUST NOT mark the cookie Secure, otherwise some
// browsers refuse to attach it to subsequent same-origin POSTs even
// though they happily send it on GETs — that was the symptom that
// surfaced as a "401 on /libraries/browse while every GET works"
// for the admin folder picker. Letting the flag follow the actual
// transport keeps strict TLS protection for prod (HTTPS reverse
// proxy) and stops the dev environment shooting itself in the foot.
func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// Behind a reverse proxy / load balancer that terminates TLS,
	// the original scheme arrives in X-Forwarded-Proto. We trust
	// that header here because the docker-compose / nginx examples
	// in deploy/ set it explicitly; pure self-hosted dev never
	// receives the header and falls through to "not secure",
	// which is the correct answer for plain http.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	return false
}

// setAuthCookies sets HTTP-only cookies for access and refresh tokens.
//
// Cookie path is `/` (not `/api/v1` like the original) so the
// browser attaches them to every same-origin request; the path-scope
// experiment was tripping up at least one configuration where a
// SameSite=Lax + Secure + Path=/api/v1 combo dropped the cookie on
// non-navigation POSTs while keeping it on GETs. Keeping `/` as the
// scope is what every reference cookie-auth setup (Plex, Jellyfin,
// generic OAuth proxies) uses and makes the behaviour predictable
// across browsers.
func setAuthCookies(w http.ResponseWriter, r *http.Request, token *auth.AuthToken, accessTTL, refreshTTL int) {
	secure := cookieSecure(r)
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    token.AccessToken,
		Path:     "/",
		MaxAge:   accessTTL,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token.RefreshToken,
		Path:     "/api/v1/auth",
		MaxAge:   refreshTTL,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearAuthCookies removes auth cookies by setting them expired.
// Mirrors setAuthCookies' Path/Secure choices so the browser's
// cookie-jar key matches and the deletion actually lands.
func clearAuthCookies(w http.ResponseWriter, r *http.Request) {
	secure := cookieSecure(r)
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "username and password are required")
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
		handleServiceError(w, r, err)
		return
	}

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	respondJSON(w, http.StatusOK, map[string]any{"data": authTokenResponse(token, u)})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	// Fall back to HTTP-only cookie if body is empty.
	if req.RefreshToken == "" {
		if c, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = c.Value
		}
	}

	if req.RefreshToken == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	token, err := h.auth.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
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
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		handleServiceError(w, r, err)
		return
	}

	clearAuthCookies(w, r)
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
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
		handleServiceError(w, r, domain.NewValidationError(fields))
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
		handleServiceError(w, r, err)
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
		handleServiceError(w, r, err)
		return
	}
	if count > 0 {
		respondError(w, r, http.StatusForbidden, "SETUP_COMPLETED", "setup has already been completed")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
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
		handleServiceError(w, r, domain.NewValidationError(fields))
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
		handleServiceError(w, r, err)
		return
	}

	h.logger.Info("setup completed — admin user created", "username", u.Username)

	// Auto-login the new admin user
	token, err := h.auth.Login(r.Context(), req.Username, req.Password, r.UserAgent(), "setup", r.RemoteAddr)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	respondJSON(w, http.StatusCreated, map[string]any{"data": authTokenResponse(token, u)})
}

