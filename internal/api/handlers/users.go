package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/library"
	"hubplay/internal/user"
)

type UserHandler struct {
	users     UserService
	libraries LibraryService
	audit     AuditEmitter
	logger    *slog.Logger
}

// NewUserHandler wires the user handler. libraries may be nil in test
// setups that don't exercise the library-access surface; production
// always passes the real service. audit nil-safe.
func NewUserHandler(users UserService, libraries LibraryService, audit AuditEmitter, logger *slog.Logger) *UserHandler {
	return &UserHandler{
		users:     users,
		libraries: libraries,
		audit:     audit,
		logger:    logger,
	}
}

func (h *UserHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
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
			"avatar_image_url":         avatarPublicURL(u.ID, u.AvatarPath),
			// Permission flags (migración 055). El frontend los usa
			// para mostrar/esconder pestañas del panel admin sin
			// tener que pedir /users/{id} para sí mismo. Owner los
			// recibe todos como true vía User.Can(), pero también
			// exponemos is_owner para que la UI marque la cuenta
			// con un badge "Owner".
			"is_owner":             u.IsOwner,
			"can_manage_admins":    u.Can(authmodel.PermManageAdmins),
			"can_manage_users":     u.Can(authmodel.PermManageUsers),
			"can_manage_libraries": u.Can(authmodel.PermManageLibraries),
			"can_manage_iptv":      u.Can(authmodel.PermManageIPTV),
			"can_edit_metadata":    u.Can(authmodel.PermEditMetadata),
			"can_change_artwork":   u.Can(authmodel.PermChangeArtwork),
			"can_view_audit":       u.Can(authmodel.PermViewAudit),
			"can_upload":           u.Can(authmodel.PermUpload),
			// Cuota de upload (migración 053). El frontend la usa
			// para pintar la línea "X de Y usados" en /uploads y
			// rechazar cliente-side ficheros que no caben antes
			// de empezar a subir bytes.
			"upload_quota_bytes": u.UploadQuotaBytes,
			"upload_used_bytes":  u.UploadUsedBytes,
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
			"avatar_image_url":         avatarPublicURL(u.ID, u.AvatarPath),
			"has_pin":                  u.PINHash != "",
			"is_primary":               primaryID != "" && u.ID == primaryID,
			"access_expires_at":        u.AccessExpiresAt,
			// Permission flags (migración 055) — el panel admin los usa
			// para pintar la matriz user × permission. is_owner aparte
			// porque marca un badge distinto en la UI ("Owner") y porque
			// el frontend lo necesita para deshabilitar las casillas de
			// esa fila (los flags del owner son inmutables).
			"is_owner":             u.IsOwner,
			"can_manage_admins":    u.CanManageAdmins,
			"can_manage_users":     u.CanManageUsers,
			"can_manage_libraries": u.CanManageLibraries,
			"can_manage_iptv":      u.CanManageIPTV,
			"can_edit_metadata":    u.CanEditMetadata,
			"can_change_artwork":   u.CanChangeArtwork,
			"can_view_audit":       u.CanViewAudit,
			"can_upload":           u.CanUpload,
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
	// Capturamos el role anterior para el audit. Si falla la lectura
	// (race con un delete concurrent), seguimos igual — el audit no
	// debe abortar la mutación.
	var oldRole string
	if cur, err := h.users.GetByID(r.Context(), id); err == nil {
		oldRole = cur.Role
	}
	if err := h.users.SetRole(r.Context(), id, req.Role); err != nil {
		handleServiceError(w, r, err)
		return
	}
	h.auditEmit().LogRoleChanged(r.Context(), r, id, oldRole, req.Role)
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
	h.auditEmit().LogUserActiveChanged(r.Context(), r, id, req.IsActive)
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

// UploadMyAvatar acepta una imagen multipart en POST /me/avatar, la
// procesa (validar MIME/tamaño, recortar cuadrado, resize a 256×256
// JPEG) y la guarda en disco + DB. Devuelve la URL pública lista
// para consumir en el frontend.
func (h *UserHandler) UploadMyAvatar(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	// Cap el cuerpo total con http.MaxBytesReader: si el cliente
	// envía más, ParseMultipartForm devuelve un error claro y no
	// hemos cargado nada en memoria todavía.
	r.Body = http.MaxBytesReader(w, r.Body, user.AvatarMaxBytes+1024)
	if err := r.ParseMultipartForm(user.AvatarMaxBytes + 1024); err != nil {
		respondError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
			"avatar exceeds the maximum allowed size")
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST",
			"multipart form must include an 'avatar' file part")
		return
	}
	defer file.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(file, user.AvatarMaxBytes+1))
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "could not read uploaded file")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		// Sin Content-Type del cliente caemos a detección por bytes.
		contentType = http.DetectContentType(data)
	}

	relName, err := h.users.UploadAvatar(r.Context(), claims.UserID, data, contentType)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"avatar_image_url": avatarPublicURL(claims.UserID, relName),
	})
}

// DeleteMyAvatar borra el avatar subido del usuario autenticado.
// Idempotente: 204 incluso si no había avatar previo.
func (h *UserHandler) DeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	if err := h.users.DeleteAvatar(r.Context(), claims.UserID); err != nil {
		handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeUserAvatar sirve los bytes del avatar de un usuario. Público
// para que peers federados y clientes anónimos (login screen) puedan
// renderizarlo sin auth. El user_id es UUID, no enumerable.
//
// 404 si el usuario no tiene avatar subido — el frontend cae al
// círculo de iniciales sobre color en ese caso.
func (h *UserHandler) ServeUserAvatar(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	target, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		handleServiceError(w, r, err)
		return
	}
	if target.AvatarPath == "" {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "user has no avatar")
		return
	}
	full, err := h.users.AvatarFilePath(target.AvatarPath)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AVATAR_PATH", err.Error())
		return
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			respondError(w, r, http.StatusNotFound, "NOT_FOUND", "avatar file missing")
			return
		}
		respondError(w, r, http.StatusInternalServerError, "AVATAR_READ", err.Error())
		return
	}
	defer f.Close() //nolint:errcheck

	stat, err := f.Stat()
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AVATAR_STAT", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// Cache 5 min en cliente; cuando el usuario re-suba, la URL
	// cambia (nuevo sufijo en relName) y el navegador refetchea
	// igualmente. ServeContent también pone ETag/Last-Modified
	// para revalidación con If-Modified-Since.
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, target.AvatarPath, stat.ModTime(), f)
}

// avatarPublicURL devuelve la URL pública para un avatar dado el
// userID + relName. Embebe el relName como query param para que el
// cliente la use como cache-buster sin que el server tenga que
// despachar por path.
func avatarPublicURL(userID, relName string) string {
	if relName == "" {
		return ""
	}
	return "/api/v1/users/" + userID + "/avatar?v=" + relName
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

	// Capturamos username antes del Delete para enriquecer el audit.
	var username string
	if u, err := h.users.GetByID(r.Context(), id); err == nil {
		username = u.Username
	}

	if err := h.users.Delete(r.Context(), id); err != nil {
		handleServiceError(w, r, err)
		return
	}

	h.auditEmit().LogUserDeleted(r.Context(), r, id, username)
	w.WriteHeader(http.StatusNoContent)
}
