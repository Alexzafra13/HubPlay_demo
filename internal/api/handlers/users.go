package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/library"
)

type UserHandler struct {
	users     UserService
	libraries LibraryService
	logger    *slog.Logger
}

// NewUserHandler wires the user handler. libraries may be nil in test
// setups that don't exercise the library-access surface; production
// always passes the real service.
func NewUserHandler(users UserService, libraries LibraryService, logger *slog.Logger) *UserHandler {
	return &UserHandler{
		users:     users,
		libraries: libraries,
		logger:    logger,
	}
}

// Me returns the currently authenticated user's profile.
func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	u, err := h.users.GetByID(r.Context(), claims.UserID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":                       u.ID,
			"username":                 u.Username,
			"display_name":             u.DisplayName,
			"role":                     u.Role,
			"is_active":                u.IsActive,
			"created_at":               u.CreatedAt,
			"last_login_at":            u.LastLoginAt,
			"password_change_required": u.PasswordChangeRequired,
			"parent_user_id":           u.ParentUserID,
			"avatar_color":             u.AvatarColor,
		},
	})
}

// List returns all users (admin only).
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	users, total, err := h.users.List(r.Context(), limit, offset)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	// Primary admin id powers the admin table's "this row's
	// destructive buttons stay disabled" gate. Lookup is cheap
	// (single SQL with a bound LIMIT 1); we tolerate failure here
	// because a transient lookup error shouldn't 500 the whole
	// /users response — the client just renders without the gate
	// (which the backend re-checks on every destructive POST/PUT
	// anyway, so the only downside is a confusing button state).
	primaryID, _ := h.users.PrimaryAdminID(r.Context())

	items := make([]map[string]any, len(users))
	for i, u := range users {
		items[i] = map[string]any{
			"id":                       u.ID,
			"username":                 u.Username,
			"display_name":             u.DisplayName,
			"role":                     u.Role,
			"is_active":                u.IsActive,
			"created_at":               u.CreatedAt,
			"last_login_at":            u.LastLoginAt,
			"password_change_required": u.PasswordChangeRequired,
			"parent_user_id":           u.ParentUserID,
			"max_content_rating":       u.MaxContentRating,
			"avatar_color":             u.AvatarColor,
			"has_pin":                  u.PINHash != "",
			"is_primary":               primaryID != "" && u.ID == primaryID,
			"access_expires_at":        u.AccessExpiresAt,
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data":  items,
		"total": total,
	})
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

// SetRole promotes / demotes a user between "user" and "admin". The
// primary admin (oldest by created_at, role=admin) is immutable —
// preventing self-DoS where a sibling admin demotes the owner of
// the deploy.
func (h *UserHandler) SetRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.Role != "user" && req.Role != "admin" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "role must be 'user' or 'admin'")
		return
	}
	if primaryID, _ := h.users.PrimaryAdminID(r.Context()); primaryID != "" && primaryID == id {
		respondError(w, r, http.StatusForbidden, "PRIMARY_ADMIN_LOCKED",
			"the primary admin cannot be demoted")
		return
	}
	if err := h.users.SetRole(r.Context(), id, req.Role); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateActiveRequest struct {
	IsActive bool `json:"is_active"`
}

type updateDisplayNameRequest struct {
	DisplayName string `json:"display_name"`
}

type updateAvatarColorRequest struct {
	// AvatarColor as a hex string (#RRGGBB) — empty clears the
	// override, falling back to the deterministic helper.
	AvatarColor string `json:"avatar_color"`
}

// SetAvatarColor swaps the user's avatar colour override. Same
// authorisation matrix as SetDisplayName / SetPIN: admin OR
// parent-of-target OR self.
func (h *UserHandler) SetAvatarColor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	allowed := claims.Role == "admin" || claims.UserID == id ||
		(target.ParentUserID != "" && target.ParentUserID == claims.UserID)
	if !allowed {
		respondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"you cannot change this user's avatar colour")
		return
	}
	var req updateAvatarColorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.users.SetAvatarColor(r.Context(), id, req.AvatarColor); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetDisplayName renames a user's human-visible label. Authorisation
// matrix mirrors SetPIN's: admins can rename anyone, parents can
// rename their own profile children, and the user themselves can
// rename their own row. Same anti-tampering rationale as SetPIN —
// the URL path param is the only identity input, the JWT claims
// drive the gate.
func (h *UserHandler) SetDisplayName(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	// Allowed: admin OR self OR (caller is parent of target profile).
	allowed := claims.Role == "admin" || claims.UserID == id ||
		(target.ParentUserID != "" && target.ParentUserID == claims.UserID)
	if !allowed {
		respondError(w, r, http.StatusForbidden, "FORBIDDEN",
			"you cannot rename this user")
		return
	}

	var req updateDisplayNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.users.SetDisplayName(r.Context(), id, req.DisplayName); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateAccessRequest struct {
	// Duration of access in days. 0 (or absent) = clear deadline =
	// permanent access. Server computes ExpiresAt as now + days.
	// Frontend sends one of {1, 3, 7, 30, 90, 365} or 0; server
	// trusts the value (no enum to maintain in two places).
	DurationDays int `json:"duration_days"`
}

// SetAccess writes a temporary-access window or clears it for
// permanent access. duration_days=0 → NULL deadline (permanent);
// any positive integer is taken as "now + N days". Admin-only.
//
// The primary admin is locked out of this surface for the same
// reason as Delete + SetActive: a sibling admin could otherwise
// time-bomb the deploy owner.
func (h *UserHandler) SetAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	if primaryID, _ := h.users.PrimaryAdminID(r.Context()); primaryID != "" && primaryID == id {
		respondError(w, r, http.StatusForbidden, "PRIMARY_ADMIN_LOCKED",
			"the primary admin's access window cannot be changed")
		return
	}
	var req updateAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.DurationDays < 0 {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "duration_days must be non-negative")
		return
	}
	var expiresAt *time.Time
	if req.DurationDays > 0 {
		t := time.Now().UTC().Add(time.Duration(req.DurationDays) * 24 * time.Hour)
		expiresAt = &t
	}
	if err := h.users.SetAccessExpiresAt(r.Context(), id, expiresAt); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetActive flips the per-user is_active flag. Admin-only at the
// route level. Self-deactivation is rejected to prevent the admin
// from accidentally locking themselves out; the primary admin is
// also protected — they're the recovery path for a deactivated
// sibling admin.
func (h *UserHandler) SetActive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	claims := auth.GetClaims(r.Context())
	if claims != nil && claims.UserID == id {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "cannot deactivate your own account")
		return
	}
	if primaryID, _ := h.users.PrimaryAdminID(r.Context()); primaryID != "" && primaryID == id {
		respondError(w, r, http.StatusForbidden, "PRIMARY_ADMIN_LOCKED",
			"the primary admin cannot be deactivated")
		return
	}
	var req updateActiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if err := h.users.SetActive(r.Context(), id, req.IsActive); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetLibraryAccess returns the library_ids the user has explicit grants
// for. Admin-only. Profile ids are normalised to their parent before the
// lookup — library_access never points at a profile, so asking for a
// profile's grants returns the parent's set (which is what the profile
// actually inherits at runtime).
func (h *UserHandler) GetLibraryAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	if h.libraries == nil {
		respondError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE",
			"library access surface is not wired in this deployment")
		return
	}
	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	ownerID := target.ID
	if target.ParentUserID != "" {
		ownerID = target.ParentUserID
	}
	libraryIDs, err := h.libraries.ListAccessByUser(r.Context(), ownerID)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if libraryIDs == nil {
		libraryIDs = []string{}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"user_id":      id,
			"owner_id":     ownerID,
			"library_ids":  libraryIDs,
			"is_inherited": ownerID != id,
		},
	})
}

type updateLibraryAccessRequest struct {
	// LibraryIDs is the full desired set of grants. Empty/null clears
	// every grant. The handler performs a transactional diff against
	// the current set, so duplicate entries are deduplicated.
	LibraryIDs []string `json:"library_ids"`
}

// SetLibraryAccess replaces the user's library_access grant set in one
// idempotent PUT. Admin-only. The target MUST be a top-level user
// (parent_user_id == ""): grants for profiles belong to the parent, so
// the endpoint rejects profile ids with 400 instead of silently
// re-targeting (which would surprise the admin when the profile got
// access through a sibling later). Unknown library_ids also 400.
func (h *UserHandler) SetLibraryAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	if h.libraries == nil {
		respondError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE",
			"library access surface is not wired in this deployment")
		return
	}
	var req updateLibraryAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if target.ParentUserID != "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
			"library access grants must target the top-level account, not a profile")
		return
	}
	// Dedupe + validate. Doing this before touching the repo keeps a
	// half-applied state impossible: either every library_id checks out
	// and the tx-backed ReplaceAccess commits, or nothing changes.
	seen := make(map[string]struct{}, len(req.LibraryIDs))
	cleaned := make([]string, 0, len(req.LibraryIDs))
	for _, libID := range req.LibraryIDs {
		if libID == "" {
			respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
				"library_ids must not contain empty values")
			return
		}
		if _, dup := seen[libID]; dup {
			continue
		}
		seen[libID] = struct{}{}
		if _, err := h.libraries.GetByID(r.Context(), libID); err != nil {
			handleServiceError(w, r, err)
			return
		}
		cleaned = append(cleaned, libID)
	}
	if err := h.libraries.ReplaceAccess(r.Context(), target.ID, cleaned); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// createPersonalIPTVRequest is the body for POST
// /admin/users/{id}/iptv-libraries. All livetv-specific fields from
// the generic create-library payload are accepted; non-livetv knobs
// (paths, scan_mode, content_type) are ignored because the service
// forces them to the personal-IPTV shape.
type createPersonalIPTVRequest struct {
	Name           string   `json:"name"`
	M3UURL         string   `json:"m3u_url"`
	EPGURL         string   `json:"epg_url,omitempty"`
	LanguageFilter []string `json:"language_filter,omitempty"`
	TLSInsecure    bool     `json:"tls_insecure,omitempty"`
}

// CreatePersonalIPTV creates a livetv library scoped to the target
// user (the only non-admin grant) in a single transaction. Admin
// only. The target MUST be a top-level user — profile ids return
// 400 because library_access never points at a profile (ADR-014);
// the admin can still hand a profile member an IPTV list by
// targeting the household's top-level user.
func (h *UserHandler) CreatePersonalIPTV(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	if h.libraries == nil {
		respondError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE",
			"library access surface is not wired in this deployment")
		return
	}
	var req createPersonalIPTVRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if target.ParentUserID != "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR",
			"personal iptv libraries must target the top-level account, not a profile")
		return
	}
	lib, err := h.libraries.CreatePersonalIPTV(r.Context(), target.ID, library.CreateRequest{
		Name:           req.Name,
		M3UURL:         req.M3UURL,
		EPGURL:         req.EPGURL,
		LanguageFilter: req.LanguageFilter,
		TLSInsecure:    req.TLSInsecure,
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"id":              lib.ID,
			"name":            lib.Name,
			"content_type":    lib.ContentType,
			"m3u_url":         lib.M3UURL,
			"epg_url":         lib.EPGURL,
			"language_filter": lib.LanguageFilter,
			"tls_insecure":    lib.TLSInsecure,
			"owner_user_id":   target.ID,
			"created_at":      lib.CreatedAt,
		},
	})
}

// Delete removes a user by ID (admin only).
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}

	// Prevent self-deletion
	claims := auth.GetClaims(r.Context())
	if claims != nil && claims.UserID == id {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "cannot delete your own account")
		return
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
