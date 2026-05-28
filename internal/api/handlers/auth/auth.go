package authhandler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/config"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
)

const (
	accessCookieName  = "hubplay_access"
	refreshCookieName = "hubplay_refresh"
)

func authTokenResponse(token *auth.AuthToken, u *authmodel.User) map[string]any {
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

// authLibraryOps es el contrato mínimo que AuthHandler necesita del
// library service: buscar por ID (register flow) y reemplazar acceso
// (grant_library_ids del register). 2 de 25 métodos.
type authLibraryOps interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
	ReplaceAccess(ctx context.Context, userID string, libraryIDs []string) error
}

// authUserOps es el contrato mínimo que AuthHandler necesita del user
// service: 4 de 14 métodos.
type authUserOps interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
	Count(ctx context.Context) (int, error)
	SetMaxContentRating(ctx context.Context, id, rating string) error
	EnsureOwner(ctx context.Context, userID string) (bool, error)
}

type AuthHandler struct {
	auth      handlers.AuthService
	users     authUserOps
	libraries authLibraryOps
	authCfg   config.AuthConfig
	audit     handlers.AuditEmitter
	logger    *slog.Logger
}

func (h *AuthHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// NewAuthHandler wires the auth handler. libraries may be nil (the setup
// wizard reuses a slimmer handler that never receives grant_library_ids);
// the main router always passes the real service. audit nil-safe.
func NewAuthHandler(authSvc handlers.AuthService, userSvc authUserOps, libraries authLibraryOps, authCfg config.AuthConfig, audit handlers.AuditEmitter, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		auth:      authSvc,
		users:     userSvc,
		libraries: libraries,
		authCfg:   authCfg,
		audit:     audit,
		logger:    logger,
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
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.Username == "" || req.Password == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "username and password are required")
		return
	}

	if req.DeviceName == "" {
		req.DeviceName = r.UserAgent()
	}
	if req.DeviceID == "" {
		req.DeviceID = "unknown"
	}

	token, err := h.auth.Login(r.Context(), req.Username, req.Password, req.DeviceName, req.DeviceID, handlers.ClientIP(r))
	if err != nil {
		// Audit del intento fallido. Sólo logueamos el username y el
		// error class — NUNCA la contraseña intentada (ni siquiera
		// hash) para que el log no se convierta en superficie de leak.
		h.auditEmit().LogAuthLoginFailed(r.Context(), r, req.Username, classifyAuthError(err))
		handlers.HandleServiceError(w, r, err)
		return
	}

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	// Audit del login exitoso. Va después de GetByID para tener el
	// username canónico (la entrada del usuario puede haber sido el
	// alias o el username con case raro).
	h.auditEmit().LogAuthLogin(r.Context(), r, u.ID, u.Username)

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
	handlers.RespondData(w, http.StatusOK, resp)
}

// profileListResponse trims the User wire payload down to what the
// "Who's watching?" screen and the topbar switcher need: identity +
// avatar attribution + PIN flag + parent linkage. Crucially leaves
// `password_hash` and `pin_hash` on the floor.
func profileListResponse(profiles []*authmodel.User) []map[string]any {
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
		if p.AvatarPath != "" {
			out[i]["avatar_image_url"] = avatarPublicURL(p.ID, p.AvatarPath)
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	profiles, err := h.auth.ListProfiles(r.Context(), claims.UserID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	var req switchProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.ProfileID == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "profile_id is required")
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
		req.DeviceName, req.DeviceID, handlers.ClientIP(r),
	)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	resp := authTokenResponse(token, u)
	if profiles, perr := h.auth.ListProfiles(r.Context(), u.ID); perr == nil {
		resp["profiles"] = profileListResponse(profiles)
	}
	handlers.RespondData(w, http.StatusOK, resp)
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	var req setContentRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.users.SetMaxContentRating(r.Context(), id, req.Rating); err != nil {
		handlers.HandleServiceError(w, r, err)
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
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
			handlers.HandleServiceError(w, r, err)
			return
		}
		if target.ParentUserID == claims.UserID {
			allowed = true
		}
	}
	if !allowed {
		handlers.RespondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"only admins or the profile's parent can change the PIN")
		return
	}

	var req setPINRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	// Validate format: empty (clear) or exactly 4 digits.
	if req.PIN != "" {
		if len(req.PIN) != 4 {
			handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "pin must be exactly 4 digits")
			return
		}
		for _, c := range req.PIN {
			if c < '0' || c > '9' {
				handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "pin must be numeric")
				return
			}
		}
	}
	if err := h.auth.SetPIN(r.Context(), id, req.PIN); err != nil {
		handlers.HandleServiceError(w, r, err)
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
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	// Fall back to HTTP-only cookie if body is empty.
	if req.RefreshToken == "" {
		if c, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = c.Value
		}
	}

	if req.RefreshToken == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	token, err := h.auth.RefreshToken(r.Context(), req.RefreshToken, handlers.ClientIP(r))
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	u, err := h.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	handlers.RespondData(w, http.StatusOK, authTokenResponse(token, u))
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
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
		return
	}

	// Captura sessionID antes del Logout — tras el revoke ya no
	// podríamos resolverlo.
	sessionID := h.auth.CurrentSessionID(r.Context(), req.RefreshToken)

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}

	// Audit del logout. El actor sale de las claims del request (el
	// user iba autenticado para llegar al endpoint).
	if claims := auth.GetClaims(r.Context()); claims != nil {
		h.auditEmit().LogAuthLogout(r.Context(), r, claims.UserID, sessionID)
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
	// GrantLibraryIDs, when present, attaches library_access grants
	// to the freshly created user in the same request. Only valid for
	// top-level accounts: profiles inherit access from their parent
	// (ADR-014), so sending grants with parent_user_id set is a 400.
	// Empty / absent means "no grants" — the admin can still call
	// PUT /users/{id}/library-access afterwards.
	GrantLibraryIDs []string `json:"grant_library_ids"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	isProfile := req.ParentUserID != ""

	// Reject library grants on profile creation early — they MUST go
	// to the parent account (ADR-014). Doing this before the heavier
	// password / username validation surfaces the contract failure
	// without burning a password autogen on a request that can't
	// succeed.
	if isProfile && len(req.GrantLibraryIDs) > 0 {
		handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
			"grant_library_ids cannot be set on a profile; grant access on the parent account")
		return
	}

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
				handlers.HandleServiceError(w, r, err)
				return
			}
			req.Password = generated
			autoGenerated = true
		} else if len(req.Password) < 8 {
			fields["password"] = "must be at least 8 characters"
		}
	}
	if len(fields) > 0 {
		handlers.HandleServiceError(w, r, domain.NewValidationError(fields))
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	// Creación de admin requiere ser owner (migración 055). El gate
	// del router (Require(PermManageUsers)) deja entrar a admins con
	// ese flag, pero crear NUEVOS admins desde uno secundario abriría
	// "admin sprawl". Sólo el owner puede convocar nuevos admins.
	// Profile creation no aplica este chequeo — los profiles heredan
	// rol del parent y nunca son admin.
	if !isProfile && req.Role == "admin" {
		claims := auth.GetClaims(r.Context())
		if claims == nil {
			handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
			return
		}
		requester, err := h.users.GetByID(r.Context(), claims.UserID)
		if err != nil {
			handlers.RespondError(w, r, http.StatusInternalServerError, "USER_LOOKUP_FAILED",
				"could not resolve requester")
			return
		}
		if !requester.IsOwner {
			handlers.RespondError(w, r, http.StatusForbidden, "OWNER_ONLY",
				"only the instance owner can create new admins")
			return
		}
	}

	// For profiles, synthesise a username from the parent's
	// username + a UUID prefix so the UNIQUE constraint stays happy
	// without making the admin invent unique handles for kids. The
	// password is a random 32-char token used solely as the bcrypt
	// input — profiles can't log in with it.
	if isProfile {
		parent, err := h.users.GetByID(r.Context(), req.ParentUserID)
		if err != nil {
			handlers.HandleServiceError(w, r, err)
			return
		}
		// Profile creation can only target a top-level account, not
		// another profile (no nested profiles).
		if parent.IsProfile() {
			handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
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
			handlers.HandleServiceError(w, r, perr)
			return
		}
		req.Password = filler
		// Profiles never carry the must-change flag — they never
		// see the change-password screen because they don't log in
		// directly.
	}

	// Validate every requested library_id BEFORE creating the user so a
	// typo doesn't leave behind a half-applied account (user row created
	// but no grants attached, surfacing as "the new user can't see
	// anything"). Top-level accounts only — the profile branch above
	// already rejected grants outright.
	var validatedGrantIDs []string
	if !isProfile && len(req.GrantLibraryIDs) > 0 {
		if h.libraries == nil {
			handlers.RespondError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE",
				"library access surface is not wired in this deployment")
			return
		}
		seen := make(map[string]struct{}, len(req.GrantLibraryIDs))
		validatedGrantIDs = make([]string, 0, len(req.GrantLibraryIDs))
		for _, libID := range req.GrantLibraryIDs {
			if libID == "" {
				handlers.RespondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
					"grant_library_ids must not contain empty values")
				return
			}
			if _, dup := seen[libID]; dup {
				continue
			}
			seen[libID] = struct{}{}
			if _, err := h.libraries.GetByID(r.Context(), libID); err != nil {
				handlers.HandleServiceError(w, r, err)
				return
			}
			validatedGrantIDs = append(validatedGrantIDs, libID)
		}
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
		handlers.HandleServiceError(w, r, err)
		return
	}

	// Audit. Tipo "user.created" cubre tanto cuentas top-level como
	// profiles — el payload distingue por role.
	h.auditEmit().LogUserCreated(r.Context(), r, u.ID, u.Username, u.Role)

	// Best-effort grant application. The user already exists at this
	// point: a grant failure is logged but does not 500 the create —
	// the admin can retry via PUT /users/{id}/library-access without
	// having to recreate the account. ReplaceAccess is a transaction,
	// so partial state inside the grant set is impossible.
	if len(validatedGrantIDs) > 0 && h.libraries != nil {
		if err := h.libraries.ReplaceAccess(r.Context(), u.ID, validatedGrantIDs); err != nil {
			h.logger.Error("apply library grants after user create",
				"user_id", u.ID, "error", err)
			handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL",
				"user created but library access grants failed; retry via PUT /users/{id}/library-access")
			return
		}
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

	handlers.RespondData(w, http.StatusCreated, out)
}

// ResetPassword is the admin "user lost their password" path. Mints a
// fresh readable password, stores it with must-change=true, blows away
// any active sessions for the target, and returns the plaintext exactly
// once. The legacy /api/v1/users router gates this with RequireAdmin so
// the handler can trust the caller already has admin role.
func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	plain, err := h.auth.ResetPassword(r.Context(), id)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	// Audit. NO incluimos la nueva contraseña en el payload — el
	// admin la ve UNA VEZ en la respuesta; el log es trazabilidad,
	// no recuperación.
	h.auditEmit().LogPasswordReset(r.Context(), r, id)
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if len(req.NewPassword) < 8 {
		handlers.HandleServiceError(w, r, domain.NewValidationError(map[string]string{
			"new_password": "must be at least 8 characters",
		}))
		return
	}
	if err := h.auth.ChangePassword(r.Context(), claims.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Setup creates the first admin user. Only works when no users exist.
//
// Two password paths, mirroring the admin /users/POST flow:
//
//   - Operator picks their own password → standard validation
//     (≥ 8 chars).
//   - Operator omits the password → server auto-generates a
//     readable 12-char temp password and returns it once on the
//     wire under `generated_password`. The wizard's last step
//     surfaces it for the operator to copy into a password
//     manager. Same shape POST /users uses for newly-created
//     non-admin accounts; consistency across the bootstrap and
//     ongoing-admin paths.
//
// Forced rotation is NOT applied to the auto-generated path here.
// The operator running setup IS the new admin, sees the
// password in the same flow, and would be bounced to
// /change-password the moment they log in — which would feel
// hostile after they just confirmed the value. Manual
// password = no rotation; auto-generated = no rotation; the
// rotation flag exists for accounts where the admin is creating
// FOR someone else.
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	count, err := h.users.Count(r.Context())
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	if count > 0 {
		handlers.RespondError(w, r, http.StatusForbidden, "SETUP_COMPLETED", "setup has already been completed")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	fields := make(map[string]string)
	if req.Username == "" || len(req.Username) < 3 {
		fields["username"] = "must be at least 3 characters"
	}
	autoGenerated := false
	if req.Password == "" {
		generated, gerr := auth.GeneratePassword()
		if gerr != nil {
			handlers.HandleServiceError(w, r, gerr)
			return
		}
		req.Password = generated
		autoGenerated = true
	} else if len(req.Password) < 8 {
		fields["password"] = "must be at least 8 characters"
	}
	if len(fields) > 0 {
		handlers.HandleServiceError(w, r, domain.NewValidationError(fields))
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
		handlers.HandleServiceError(w, r, err)
		return
	}

	// Promover al primer admin como owner (migración 055). EnsureOwner
	// es idempotente: si ya existe owner (caso re-setup tras
	// recuperación manual de DB), no toca nada. Sin esta llamada un
	// install fresh quedaría con role='admin' pero sin is_owner, y
	// RequireOwner devolvería 403 en backup/keystore/federation/restart.
	// El error lo logueamos pero no abortamos el setup — el usuario
	// puede arreglarlo después con un comando manual; lo crítico
	// (cuenta creada + auto-login) ya está hecho.
	if _, ownerErr := h.users.EnsureOwner(r.Context(), u.ID); ownerErr != nil {
		h.logger.Error("setup: could not promote first admin to owner",
			"user_id", u.ID, "error", ownerErr)
	}

	h.logger.Info("setup completed — admin user created",
		"username", u.Username, "auto_generated_password", autoGenerated)
	h.auditEmit().LogUserCreated(r.Context(), r, u.ID, u.Username, "admin")

	// Auto-login the new admin user
	token, err := h.auth.Login(r.Context(), req.Username, req.Password, r.UserAgent(), "setup", handlers.ClientIP(r))
	if err != nil {
		handlers.HandleServiceError(w, r, err)
		return
	}
	h.auditEmit().LogAuthLogin(r.Context(), r, u.ID, u.Username)

	resp := authTokenResponse(token, u)
	if autoGenerated {
		// Surface the plaintext exactly once. The wizard renders it
		// in the completion step; we never persist it anywhere else.
		resp["generated_password"] = req.Password
	}

	setAuthCookies(w, r, token, int(h.authCfg.AccessTokenTTL.Seconds()), int(h.authCfg.RefreshTokenTTL.Seconds()))
	handlers.RespondData(w, http.StatusCreated, resp)
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
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	rows, err := h.auth.ListSessions(r.Context(), claims.UserID)
	if err != nil {
		handlers.HandleServiceError(w, r, err)
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
			"auth_method":    sessionAuthMethod(s.DeviceID),
		}
	}
	handlers.RespondData(w, http.StatusOK, out)
}

// sessionAuthMethod classifies a session by how it was minted. The
// device-code service prefixes device_id with "device-code-" (see
// internal/auth/device.go), so a string prefix check is enough to
// distinguish a paired-via-QR-or-link session from a regular
// username/password login. The wire string lets the UI badge each
// session honestly ("Vínculo dispositivo" vs "Sesión web") instead of
// dumping every refresh token undifferentiated.
func sessionAuthMethod(deviceID string) string {
	if strings.HasPrefix(deviceID, "device-code-") {
		return "device_link"
	}
	return "password"
}

// RevokeMySession deletes a single auth session if it belongs to
// the caller. Revoking the caller's own session clears the cookies
// too so the next request lands on /login cleanly instead of
// hitting a 401 loop on the now-orphaned refresh token.
func (h *AuthHandler) RevokeMySession(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	sessionID := handlers.RequireParam(w, r, "id")
	if sessionID == "" {
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
		handlers.HandleServiceError(w, r, err)
		return
	}

	if revokedSelf {
		clearAuthCookies(w, r)
	}
	// Audit. Tipo "auth.session.revoked"; el actor es el caller (que
	// auth.GetClaims devuelve), el target es el sessionID.
	if claims := auth.GetClaims(r.Context()); claims != nil {
		// Reusamos LogAuthLogout porque revocar una sesión vía API es
		// semánticamente equivalente al logout — el panel admin las
		// puede mostrar juntas si quiere. Si en el futuro un evento
		// separado importa, lo separamos.
		h.auditEmit().LogAuthLogout(r.Context(), r, claims.UserID, sessionID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// classifyAuthError extrae una etiqueta corta del error que el auth
// service devuelve, para meter en el audit log de logins fallidos.
// La idea es que el operador vea "wrong_password" o "user_locked"
// en el panel, no el mensaje crudo (que cambia entre releases) ni
// el error.Error() completo (que puede leak detalles internos).
func classifyAuthError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case stringsContains(msg, "invalid credentials"):
		return "wrong_password"
	case stringsContains(msg, "not found"):
		return "user_not_found"
	case stringsContains(msg, "locked"):
		return "user_locked"
	case stringsContains(msg, "not active"), stringsContains(msg, "inactive"):
		return "user_disabled"
	case stringsContains(msg, "rate"):
		return "rate_limited"
	default:
		return "other"
	}
}

// stringsContains es un alias trivial para evitar importar "strings"
// sólo para esta función (auth.go ya tiene varios imports; mantener
// el listado más corto ayuda al diff).
func stringsContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
