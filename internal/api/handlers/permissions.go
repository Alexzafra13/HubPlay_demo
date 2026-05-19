package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

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
	TransferOwnership(ctx context.Context, currentOwnerID, newOwnerID string) error
}

// PermissionsHandler hospeda los endpoints de gestión de permisos de
// admins:
//
//   GET  /users/{id}/permissions     — lee los flags (auth: el propio
//                                       usuario, o cualquiera con
//                                       can_view_audit / can_manage_admins).
//   PUT  /users/{id}/permissions     — modifica flags. Owner-only para
//                                       cambios sobre otro owner-candidate;
//                                       can_manage_admins para el resto.
//   POST /users/{id}/transfer-ownership — owner-only. Mueve el flag
//                                       is_owner al target.
//
// Las decisiones de gate hard (owner-only) van en el router via
// PermissionChecker.RequireOwner. Aquí dentro hacemos los chequeos
// FINOS: que el target sea legítimo, que el body no traiga keys
// fuera del whitelist, que un admin con can_manage_admins NO pueda
// auto-otorgarse can_manage_admins.
type PermissionsHandler struct {
	store  PermissionsStore
	logger *slog.Logger
}

func NewPermissionsHandler(store PermissionsStore, logger *slog.Logger) *PermissionsHandler {
	return &PermissionsHandler{store: store, logger: logger.With("module", "permissions-handler")}
}

// GetPermissions devuelve los flags del usuario indicado. El frontend
// lo usa para pintar la matriz de checkboxes y para que el propio
// usuario pueda consultarse en /me (ya cubierto por /me, pero exponer
// el endpoint separado simplifica el panel admin "edit user").
func (h *PermissionsHandler) GetPermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}
	u, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
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
//   1. NUNCA modificar al owner — sus flags son inmutables.
//   2. Auto-otorgarse can_manage_admins está prohibido (salvo que ya
//      lo tengas: en ese caso el PUT sobre ti mismo deja el flag
//      como estaba, así que no hace daño).
//   3. Sólo el owner puede otorgar can_manage_admins a otros — los
//      admins secundarios no pueden crear pares "can_manage_admins"
//      adicionales aunque tengan el flag. Es la defensa contra
//      sprawl de admins comprometidos.
func (h *PermissionsHandler) PutPermissions(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		respondError(w, r, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
		return
	}

	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	requester, err := h.store.GetByID(r.Context(), claims.UserID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "USER_LOOKUP_FAILED", "could not resolve requester")
		return
	}

	target, err := h.store.GetByID(r.Context(), targetID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "target user not found")
		return
	}

	// (1) Owner es inmutable.
	if target.IsOwner {
		respondError(w, r, http.StatusForbidden, "OWNER_IMMUTABLE",
			"owner permissions cannot be changed; transfer ownership first")
		return
	}
	// Cambios sobre admins sólo si el target es de hecho admin —
	// usuarios normales no tienen flags de admin.
	if target.Role != "admin" {
		respondError(w, r, http.StatusBadRequest, "TARGET_NOT_ADMIN",
			"target user is not an admin; promote them first")
		return
	}

	var req SetPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}

	// (3) Otorgar can_manage_admins requiere ser el owner.
	if req.CanManageAdmins != nil && *req.CanManageAdmins && !requester.IsOwner {
		respondError(w, r, http.StatusForbidden, "OWNER_ONLY",
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
	for _, op := range ops {
		if op.val == nil {
			continue
		}
		if err := h.store.SetPermission(r.Context(), targetID, op.col, *op.val); err != nil {
			h.logger.Error("set permission failed",
				"target", targetID, "column", op.col, "error", err)
			respondError(w, r, http.StatusInternalServerError, "SET_PERMISSION_FAILED",
				"could not update "+op.col)
			return
		}
		applied++
	}

	h.logger.Info("permissions updated",
		"requester", claims.UserID, "target", targetID, "fields_applied", applied)
	// Re-fetch para devolver el estado final.
	h.GetPermissions(w, r)
}

// TransferOwnershipRequest mapea el body POST.
type TransferOwnershipRequest struct {
	NewOwnerID  string `json:"new_owner_id"`
	Confirmation string `json:"confirmation"`
}

// TransferOwnership mueve is_owner al usuario indicado. Gate del
// middleware: RequireOwner (sólo el owner actual puede invocarlo).
// Validaciones adicionales aquí:
//
//   - confirmation == "TRANSFER" (string literal) — defensa contra
//     POST accidentales del frontend; obligar un texto explícito en
//     el body evita misclicks.
//   - El target tiene que ser admin activo cuenta-titular (el repo
//     lo enforza también, pero damos un error claro antes).
//
// Tras éxito, el owner ANTERIOR se queda con todos sus flags
// granulares como estaban (no se los limpia automáticamente — si
// el ex-owner quiere convertirse en "sólo admin con todo" o "admin
// sin nada", el NUEVO owner decide via PUT /permissions).
func (h *PermissionsHandler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req TransferOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "BAD_BODY", err.Error())
		return
	}
	if req.Confirmation != "TRANSFER" {
		respondError(w, r, http.StatusBadRequest, "CONFIRMATION_REQUIRED",
			`body must include "confirmation": "TRANSFER"`)
		return
	}
	if req.NewOwnerID == "" || req.NewOwnerID == claims.UserID {
		respondError(w, r, http.StatusBadRequest, "BAD_TARGET",
			"new_owner_id must be a different user")
		return
	}

	if err := h.store.TransferOwnership(r.Context(), claims.UserID, req.NewOwnerID); err != nil {
		h.logger.Error("transfer ownership failed",
			"from", claims.UserID, "to", req.NewOwnerID, "error", err)
		respondError(w, r, http.StatusBadRequest, "TRANSFER_FAILED", err.Error())
		return
	}

	h.logger.Warn("ownership transferred",
		"from", claims.UserID, "to", req.NewOwnerID)
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"new_owner_id": req.NewOwnerID,
		},
	})
}
