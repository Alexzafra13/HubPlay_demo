package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

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
			// Surface the must-change flag so the frontend can route
			// to the forced ChangePassword screen before any rail or
			// detail fetch tries to use the JWT. Cleared by a
			// successful POST /me/password.
			"password_change_required": u.PasswordChangeRequired,
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

	// Profile list goes with the token so the frontend can decide
	// whether to drop into "Who's watching?" or skip straight to the
	// home screen on solo accounts. We swallow lookup errors here —
	// a deploy without profile rows just gets an empty `profiles`
	// array, which the frontend already handles as "no selection
	// needed".
	resp := authTokenResponse(token, u)
	if profiles, perr := h.auth.ListProfiles(r.Context(), u.ID); perr == nil {
		resp["profiles"] = profileListResponse(profiles)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// profileListResponse trims the User wire payload down to what the
// "Who's watching?" screen and the topbar switcher need: identity +
// avatar attribution + PIN flag + parent linkage. Crucially leaves
// `password_hash` and `pin_hash` on the floor.
func profileListResponse(profiles []*db.User) []map[string]any {
	out := make([]map[string]any, len(profiles))
	for i, p := range profiles {
		out[i] = map[string]any{
			"id":             p.ID,
			"username":       p.Username,
			"display_name":   p.DisplayName,
			"role":           p.Role,
			"is_active":      p.IsActive,
			"parent_user_id": p.ParentUserID,
			"has_pin":        p.PINHash != "",
		}
		if p.MaxContentRating != "" {
			out[i]["max_content_rating"] = p.MaxContentRating
		}
		if p.AvatarColor != "" {
			out[i]["avatar_color"] = p.AvatarColor
		}
	}
	return out
}

// ListProfiles returns the profile tree for the authenticated user.
// Used by the "Who's watching?" screen when the frontend lands via a
// refreshed cookie (no fresh login response to consume profiles from).
func (h *AuthHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	profiles, err := h.auth.ListProfiles(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": profileListResponse(profiles),
	})
}

type switchProfileRequest struct {
	ProfileID  string `json:"profile_id"`
	PIN        string `json:"pin"`
	DeviceName string `json:"device_name"`
	DeviceID   string `json:"device_id"`
}

// SwitchProfile mints a new auth token for a sibling / parent profile.
// Caller authenticates with their current JWT; the service verifies
// the target lives under the same parent before issuing the new
// token. PIN-protected profiles require the matching PIN — wrong
// PIN returns the same 401 the wrong-password path does.
func (h *AuthHandler) SwitchProfile(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	var req switchProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.ProfileID == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "profile_id is required")
		return
	}
	if req.DeviceName == "" {
		req.DeviceName = r.UserAgent()
	}
	if req.DeviceID == "" {
		req.DeviceID = "unknown"
	}
	token, err := h.auth.SwitchProfile(
		r.Context(), claims.UserID, req.ProfileID, req.PIN,
		req.DeviceName, req.DeviceID, r.RemoteAddr,
	)
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
	resp := authTokenResponse(token, u)
	if profiles, perr := h.auth.ListProfiles(r.Context(), u.ID); perr == nil {
		resp["profiles"] = profileListResponse(profiles)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type setPINRequest struct {
	// Empty string clears the PIN. 4 digits otherwise.
	PIN string `json:"pin"`
}

type setContentRatingRequest struct {
	// Empty string clears the cap (= no restriction). Otherwise one
	// of the literals from the rating ranking table (G/PG/PG-13/R/
	// NC-17/TV-Y/TV-Y7/TV-G/TV-PG/TV-14/TV-MA).
	Rating string `json:"rating"`
}

// SetContentRating updates a profile's max content rating. Validation
// is permissive — unknown values are stored as-is and the filter
// callsite fail-opens (treats unknown caps as "no restriction") so a
// future deploy that adds a localised rating won't lock users out
// retroactively.
func (h *AuthHandler) SetContentRating(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	var req setContentRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.users.SetMaxContentRating(r.Context(), id, req.Rating); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetPIN sets or clears the PIN of a user. Authorisation matrix:
//   - admins can set the PIN of any user.
//   - the parent of a profile can set their own child's PIN. This
//     matches the household-owner mental model: you don't need an
//     admin to give your kid a PIN, the parent of the family group
//     can do it themselves.
//   - a user can set their own PIN.
//   - everyone else gets 403.
//
// Routed under /users/{id}/pin which is admin-gated by middleware,
// so the non-admin paths (parent / self) only reach this handler
// because the route is also exposed under the user-side group. The
// gate below catches any future reshuffle that might widen the
// route accidentally.
func (h *AuthHandler) SetPIN(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}

	allowed := claims.Role == "admin" || claims.UserID == id
	if !allowed {
		// Parent-of-target check. The target's parent_user_id must
		// match the caller's user_id; nested profiles are forbidden
		// at creation time so the parent layer is always exactly
		// one hop deep.
		target, err := h.users.GetByID(r.Context(), id)
		if err != nil {
			handleServiceError(w, r, err)
			return
		}
		if target.ParentUserID == claims.UserID {
			allowed = true
		}
	}
	if !allowed {
		respondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"only admins or the profile's parent can change the PIN")
		return
	}

	var req setPINRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	// Validate format: empty (clear) or exactly 4 digits.
	if req.PIN != "" {
		if len(req.PIN) != 4 {
			respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "pin must be exactly 4 digits")
			return
		}
		for _, c := range req.PIN {
			if c < '0' || c > '9' {
				respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "pin must be numeric")
				return
			}
		}
	}
	if err := h.auth.SetPIN(r.Context(), id, req.PIN); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

	token, err := h.auth.RefreshToken(r.Context(), req.RefreshToken, r.RemoteAddr)
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
	// Password is optional in the admin path: when empty the server
	// generates a readable temporary password and returns it once in
	// the response under "generated_password". The new account's
	// password_change_required flag is set so first login lands on
	// the ChangePassword screen.
	Password string `json:"password"`
	Role     string `json:"role"`
	// ParentUserID, when set, makes this row a profile under the
	// referenced account rather than a standalone user. Profiles
	// share the parent's password and switch via /auth/switch-profile;
	// the legacy `password` field is ignored when a parent is set
	// because profiles don't authenticate independently.
	ParentUserID string `json:"parent_user_id"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	isProfile := req.ParentUserID != ""

	fields := make(map[string]string)
	// Profile usernames get auto-derived from the parent + the
	// display name so the admin doesn't have to invent unique
	// usernames for each kid in the household — the slot just
	// doesn't matter for profiles, they don't log in directly.
	if !isProfile {
		if req.Username == "" || len(req.Username) < 3 || len(req.Username) > 32 {
			fields["username"] = "must be 3-32 characters"
		}
	}
	// Password is admin-optional for top-level accounts; profiles
	// share the parent's password so the field is ignored entirely
	// when ParentUserID is set.
	autoGenerated := false
	if !isProfile {
		if req.Password == "" {
			generated, err := auth.GeneratePassword()
			if err != nil {
				handleServiceError(w, r, err)
				return
			}
			req.Password = generated
			autoGenerated = true
		} else if len(req.Password) < 8 {
			fields["password"] = "must be at least 8 characters"
		}
	}
	if len(fields) > 0 {
		handleServiceError(w, r, domain.NewValidationError(fields))
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	// For profiles, synthesise a username from the parent's
	// username + a UUID prefix so the UNIQUE constraint stays happy
	// without making the admin invent unique handles for kids. The
	// password is a random 32-char token used solely as the bcrypt
	// input — profiles can't log in with it.
	if isProfile {
		parent, err := h.users.GetByID(r.Context(), req.ParentUserID)
		if err != nil {
			handleServiceError(w, r, err)
			return
		}
		// Profile creation can only target a top-level account, not
		// another profile (no nested profiles).
		if parent.IsProfile() {
			respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
				"parent_user_id must be a top-level account")
			return
		}
		// Username is admin-supplied? Use it. Otherwise synthesise
		// from the display_name. Either way prefix with the parent's
		// id so collisions are impossible.
		base := req.Username
		if base == "" {
			base = req.DisplayName
		}
		req.Username = parent.Username + "/" + base
		// Token used as bcrypt input for the password column. We
		// don't ship it anywhere — profiles authenticate via the
		// parent's switch-profile flow.
		filler, perr := auth.GeneratePassword()
		if perr != nil {
			handleServiceError(w, r, perr)
			return
		}
		req.Password = filler
		// Profiles never carry the must-change flag — they never
		// see the change-password screen because they don't log in
		// directly.
	}

	u, err := h.auth.Register(r.Context(), auth.RegisterRequest{
		Username:               req.Username,
		DisplayName:            req.DisplayName,
		Password:               req.Password,
		Role:                   req.Role,
		ParentUserID:           req.ParentUserID,
		PasswordChangeRequired: autoGenerated,
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	out := map[string]any{
		"id":                       u.ID,
		"username":                 u.Username,
		"display_name":             u.DisplayName,
		"role":                     u.Role,
		"password_change_required": u.PasswordChangeRequired,
	}
	if autoGenerated {
		// Return the plaintext exactly once. The admin pane copies it
		// into a "share with the user" modal; we never persist it.
		out["generated_password"] = req.Password
	}

	respondJSON(w, http.StatusCreated, map[string]any{"data": out})
}

// ResetPassword is the admin "user lost their password" path. Mints a
// fresh readable password, stores it with must-change=true, blows away
// any active sessions for the target, and returns the plaintext exactly
// once. The legacy /api/v1/users router gates this with RequireAdmin so
// the handler can trust the caller already has admin role.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	plain, err := h.auth.ResetPassword(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"user_id":            id,
			"generated_password": plain,
		},
	})
}

type changePasswordRequest struct {
	// CurrentPassword is required when the caller's account doesn't
	// have password_change_required set. When the flag IS set the
	// server skips the comparison since the caller just authenticated
	// using the temporary password the admin gave them.
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangeMyPassword is the user-side "rotate my own password" flow.
// Mounted under /me/password so the authenticated user can rotate
// their own credential without admin involvement. Clearing the
// must-change flag is the side effect that completes a forced
// rotation — the frontend re-issues `/me` after success and sees
// the flag flip to false.
func (h *AuthHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if len(req.NewPassword) < 8 {
		handleServiceError(w, r, domain.NewValidationError(map[string]string{
			"new_password": "must be at least 8 characters",
		}))
		return
	}
	if err := h.auth.ChangePassword(r.Context(), claims.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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


// ─── Active sessions (user-facing) ──────────────────────────────────

// ListMySessions returns the caller's active auth sessions (one row
// per refresh token alive in the DB). The "Tus dispositivos" panel
// in Settings consumes this. We mark whichever row matches the
// caller's refresh cookie as `current: true` so the UI can label
// it and warn before the operator revokes themselves.
func (h *AuthHandler) ListMySessions(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	rows, err := h.auth.ListSessions(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	currentID := ""
	if c, cerr := r.Cookie(refreshCookieName); cerr == nil {
		currentID = h.auth.CurrentSessionID(r.Context(), c.Value)
	}
	out := make([]map[string]any, len(rows))
	for i, s := range rows {
		out[i] = map[string]any{
			"id":             s.ID,
			"device_name":    s.DeviceName,
			"device_id":      s.DeviceID,
			"ip_address":     s.IPAddress,
			"created_at":     s.CreatedAt,
			"last_active_at": s.LastActiveAt,
			"expires_at":     s.ExpiresAt,
			"current":        s.ID == currentID,
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// RevokeMySession deletes a single auth session if it belongs to
// the caller. Revoking the caller's own session clears the cookies
// too so the next request lands on /login cleanly instead of
// hitting a 401 loop on the now-orphaned refresh token.
func (h *AuthHandler) RevokeMySession(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "session id is required")
		return
	}

	// Detect "user revoked themselves" before the row is gone so we
	// can clear cookies on the response.
	revokedSelf := false
	if c, cerr := r.Cookie(refreshCookieName); cerr == nil {
		if h.auth.CurrentSessionID(r.Context(), c.Value) == sessionID {
			revokedSelf = true
		}
	}

	if err := h.auth.RevokeSession(r.Context(), claims.UserID, sessionID); err != nil {
		handleServiceError(w, r, err)
		return
	}

	if revokedSelf {
		clearAuthCookies(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}
