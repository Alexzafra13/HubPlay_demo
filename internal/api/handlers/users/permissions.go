package users

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

// PermissionsStore es la mínima superficie del user repo que estos
// handlers necesitan. Aislamos de *db.UserRepository para que tests
// pasen un fake sin DB y para que el cambio de columna no cambie el
// constructor del handler.
type PermissionsStore interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
	SetPermission(ctx context.Context, id, column string, value bool) error
}

// PermissionsHandler hospeda los endpoints de gestión de permisos de
// admins:
//
//	GET /users/{id}/permissions — lee los flags (admin-only via grupo).
//	PUT /users/{id}/permissions — modifica flags. Owner inmutable;
//	                              sólo el owner puede otorgar
//	                              can_manage_admins a otros.
//
// Owner-transfer NO existe — el owner es inmutable de por vida. Si
// hace falta ceder la instalación, va por CLI fuera de HTTP.
//
// Las decisiones de gate hard (can_manage_admins) van en el router
// via PermissionChecker.Require. Aquí dentro hacemos los chequeos
// FINOS: que el target sea legítimo, que el body no traiga keys
// fuera del whitelist, que un admin con can_manage_admins NO pueda
// auto-otorgarse can_manage_admins.
type PermissionsHandler struct {
	store  PermissionsStore
	audit  handlers.AuditEmitter
	logger *slog.Logger
}

// NewPermissionsHandler — audit nil-safe.
func NewPermissionsHandler(store PermissionsStore, audit handlers.AuditEmitter, logger *slog.Logger) *PermissionsHandler {
	return &PermissionsHandler{
		store:  store,
		audit:  audit,
		logger: logger.With("module", "permissions-handler"),
	}
}

func (h *PermissionsHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// GetPermissions devuelve los flags del usuario indicado. El frontend
// lo usa para pintar la matriz de checkboxes y para que el propio
// usuario pueda consultarse en /me (ya cubierto por /me, pero exponer
// el endpoint separado simplifica el panel admin "edit user").
func (h *PermissionsHandler) GetPermissions(w http.ResponseWriter, r *http.Request) {
	id := handlers.RequireParam(w, r, "id")
	if id == "" {
		return
	}
	u, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":                   u.ID,
			"is_owner":             u.IsOwner,
			"can_manage_admins":    u.CanManageAdmins,
			"can_manage_users":     u.CanManageUsers,
			"can_manage_libraries": u.CanManageLibraries,
			"can_manage_iptv":      u.CanManageIPTV,
			"can_edit_metadata":    u.CanEditMetadata,
			"can_change_artwork":   u.CanChangeArtwork,
			"can_view_audit":       u.CanViewAudit,
			"can_upload":           u.CanUpload,
		},
	})
}

// SetPermissionsRequest mapea el body PUT. Cada campo es *bool para
// distinguir "no enviado" de "enviado false" — un PUT parcial que
// sólo cambia un flag no debe sobreescribir los demás a false.
type SetPermissionsRequest struct {
	CanManageAdmins    *bool `json:"can_manage_admins,omitempty"`
	CanManageUsers     *bool `json:"can_manage_users,omitempty"`
	CanManageLibraries *bool `json:"can_manage_libraries,omitempty"`
	CanManageIPTV      *bool `json:"can_manage_iptv,omitempty"`
	CanEditMetadata    *bool `json:"can_edit_metadata,omitempty"`
	CanChangeArtwork   *bool `json:"can_change_artwork,omitempty"`
	CanViewAudit       *bool `json:"can_view_audit,omitempty"`
	CanUpload          *bool `json:"can_upload,omitempty"`
}

// PutPermissions aplica un set parcial de flags al target. El gate
// del middleware es RequireCanManageAdmins; aquí añadimos:
//
//  1. NUNCA modificar al owner — sus flags son inmutables.
//  2. Auto-otorgarse can_manage_admins está prohibido (salvo que ya
//     lo tengas: en ese caso el PUT sobre ti mismo deja el flag
//     como estaba, así que no hace daño).
//  3. Sólo el owner puede otorgar can_manage_admins a otros — los
//     admins secundarios no pueden crear pares "can_manage_admins"
//     adicionales aunque tengan el flag. Es la defensa contra
//     sprawl de admins comprometidos.
func (h *PermissionsHandler) PutPermissions(w http.ResponseWriter, r *http.Request) {
	targetID := handlers.RequireParam(w, r, "id")
	if targetID == "" {
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		handlers.RespondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	requester, err := h.store.GetByID(r.Context(), claims.UserID)
	if err != nil {
		handlers.RespondError(w, r, http.StatusInternalServerError, "USER_LOOKUP_FAILED", "could not resolve requester")
		return
	}

	target, err := h.store.GetByID(r.Context(), targetID)
	if err != nil {
		handlers.RespondError(w, r, http.StatusNotFound, "NOT_FOUND", "target user not found")
		return
	}

	// (1) Owner es inmutable.
	if target.IsOwner {
		handlers.RespondError(w, r, http.StatusForbidden, "OWNER_IMMUTABLE",
			"owner permissions cannot be changed; transfer ownership first")
		return
	}
	// Cambios sobre admins sólo si el target es de hecho admin —
	// usuarios normales no tienen flags de admin.
	if target.Role != "admin" {
		handlers.RespondError(w, r, http.StatusBadRequest, "TARGET_NOT_ADMIN",
			"target user is not an admin; promote them first")
		return
	}

	var req SetPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}

	// (3) Otorgar can_manage_admins requiere ser el owner.
	if req.CanManageAdmins != nil && *req.CanManageAdmins && !requester.IsOwner {
		handlers.RespondError(w, r, http.StatusForbidden, "OWNER_ONLY",
			"only the owner can grant can_manage_admins")
		return
	}

	// (2) Auto-revocación de can_manage_admins sobre uno mismo está
	// permitida (paradoxal pero útil: te das cuenta de que no quieres
	// ese poder); auto-otorgárselo cuando no lo tienes está cubierto
	// por (3) porque entonces el requester no es owner.
	//
	// Aplicamos los cambios. Cada flag es una mutación independiente
	// — si una falla, las anteriores ya están aplicadas. Es aceptable
	// porque el target es UN admin y los flags son ortogonales; un
	// fallo parcial no deja la cuenta en estado inconsistente, sólo
	// con menos cambios de los pedidos.
	ops := []struct {
		col string
		val *bool
	}{
		{"can_manage_admins", req.CanManageAdmins},
		{"can_manage_users", req.CanManageUsers},
		{"can_manage_libraries", req.CanManageLibraries},
		{"can_manage_iptv", req.CanManageIPTV},
		{"can_edit_metadata", req.CanEditMetadata},
		{"can_change_artwork", req.CanChangeArtwork},
		{"can_view_audit", req.CanViewAudit},
		{"can_upload", req.CanUpload},
	}
	applied := 0
	changes := make(map[string]bool)
	for _, op := range ops {
		if op.val == nil {
			continue
		}
		if err := h.store.SetPermission(r.Context(), targetID, op.col, *op.val); err != nil {
			h.logger.Error("set permission failed",
				"target", targetID, "column", op.col, "error", err)
			handlers.RespondError(w, r, http.StatusInternalServerError, "SET_PERMISSION_FAILED",
				"could not update "+op.col)
			return
		}
		applied++
		changes[op.col] = *op.val
	}

	h.logger.Info("permissions updated",
		"requester", claims.UserID, "target", targetID, "fields_applied", applied)
	// Audit del cambio: payload incluye sólo los flags modificados.
	// Si no cambió ninguno (body vacío), no emitimos para no ensuciar
	// el log con no-ops.
	if applied > 0 {
		h.auditEmit().LogPermissionChanged(r.Context(), r, targetID, changes)
	}
	// Re-fetch para devolver el estado final.
	h.GetPermissions(w, r)
}

// Owner-transfer: deliberately NOT implemented. El owner es inmutable
// de por vida. Cualquier necesidad operativa de "ceder la app a otro"
// se resuelve por canal admin (CLI / edición DB), no por HTTP.
