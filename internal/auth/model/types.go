// Package model contains the auth-domain types (User, Session,
// SigningKey, DeviceCode) the rest of the codebase consumes. Lives
// in its own sub-package — instead of in `internal/auth/` directly —
// to break the dependency cycle:
//
//   internal/auth        imports internal/db         (for repo concretes)
//   internal/db          imports internal/auth/model (for return types)
//   internal/auth        imports internal/auth/model (for types it also uses)
//
// `auth/model` is a leaf (no imports beyond stdlib) so a cycle is
// impossible. Closes "Opción B" of olor A from the 2026-05-14 audit:
// types of a feature live IN the feature, not in `internal/db/`.
//
// Si en una iteración futura alguien quiere mover también los repos
// al feature (patrón federation/storage), `auth/model` ya está en su
// sitio definitivo y el movimiento es local — cero re-arrange del
// grafo de importaciones.
package model

import (
	"database/sql"
	"time"
)
//
// Cero cambio de wire HTTP, de tabla SQL, ni de migraciones — los
// tipos son copia verbatim de las definiciones previas en
// `internal/db/{user,session,signing_key,device_code}_repository.go`
// (commits previos los tenían ahí desde el día 1). Los campos
// `sql.Null*` se preservan tal cual; un refactor a `*string`/
// `*time.Time` puro queda fuera de scope (otro chunk).

// User representa una cuenta del sistema HubPlay (top-level account)
// o un profile dentro de una cuenta (parent_user_id apunta al
// titular). Los profiles comparten la contraseña del parent — la
// invariante la enforce el repo en Create.
type User struct {
	ID           string
	Username     string
	DisplayName  string
	PasswordHash string
	AvatarPath   string
	Role         string
	IsActive     bool
	MaxSessions  int
	CreatedAt    time.Time
	LastLoginAt  *time.Time

	// Profile tree fields (migración 034). Top-level account =
	// parent_user_id empty; profile = child row sharing parent's
	// password.
	ParentUserID           string
	PINHash                string
	MaxContentRating       string
	PasswordChangeRequired bool

	// AccessExpiresAt es la temp-access deadline. nil = permanent.
	AccessExpiresAt *time.Time

	// AvatarColor — override opcional per-user. Empty = fallback
	// determinista FNV-1a → paleta en el frontend.
	AvatarColor string

	// Upload gate (migración 053). CanUpload = permiso explícito,
	// off por defecto. UploadQuotaBytes = tope absoluto en disco
	// que sus subidas pueden ocupar; 0 = sin permiso efectivo
	// (aunque CanUpload sea true). UploadUsedBytes = espacio ya
	// ocupado por uploads suyos; lo mantiene el upload service
	// vía Reserve/Release atómicos en el repo.
	CanUpload        bool
	UploadQuotaBytes int64
	UploadUsedBytes  int64

	// Admin permissions (migración 055). RBAC plano:
	//   IsOwner          → admin raíz inmutable. Sólo existe UNO.
	//                      Tiene TODO implícito; los demás flags
	//                      se ignoran cuando IsOwner=true.
	//   CanManageAdmins  → otorgar/revocar flags a otros admins,
	//                      promover/degradar role. Solo el owner
	//                      lo tiene de salida.
	//   CanManageUsers   → alta/baja usuarios no-admin, reset pwd,
	//                      library_access, perfiles.
	//   CanManageLibraries → CRUD librerías, paths, scans.
	//   CanManageIPTV    → M3U, EPG, canales, schedules.
	//   CanEditMetadata  → títulos, descripciones, identify TMDb.
	//   CanChangeArtwork → posters, fondos, overrides.
	//   CanViewAudit     → /admin/uploads/audit y similares.
	//
	// Para checks de capability: usar User.Can(perm) — incluye la
	// regla "owner tiene todo" sin que cada caller la replique.
	IsOwner             bool
	CanManageAdmins     bool
	CanManageUsers      bool
	CanManageLibraries  bool
	CanManageIPTV       bool
	CanEditMetadata     bool
	CanChangeArtwork    bool
	CanViewAudit        bool
}

// Permission es uno de los flags granulares. Definido como string
// (no const-int) para que serialice limpio en endpoints / logs y para
// que el handler pueda iterar sobre los keys que llegan en un PUT
// body { "can_manage_users": true, ... }.
type Permission string

const (
	PermManageAdmins    Permission = "can_manage_admins"
	PermManageUsers     Permission = "can_manage_users"
	PermManageLibraries Permission = "can_manage_libraries"
	PermManageIPTV      Permission = "can_manage_iptv"
	PermEditMetadata    Permission = "can_edit_metadata"
	PermChangeArtwork   Permission = "can_change_artwork"
	PermViewAudit       Permission = "can_view_audit"
	PermUpload          Permission = "can_upload"
)

// AllPermissions devuelve todos los flags granulares conocidos. El
// frontend lo usa para pintar la matriz; el handler de PUT lo usa
// para validar que las keys del body están en el set permitido.
func AllPermissions() []Permission {
	return []Permission{
		PermManageAdmins,
		PermManageUsers,
		PermManageLibraries,
		PermManageIPTV,
		PermEditMetadata,
		PermChangeArtwork,
		PermViewAudit,
		PermUpload,
	}
}

// Can devuelve true si el usuario tiene la capability. IsOwner==true
// implica TODOS los permisos automáticamente — el chequeo aquí
// centraliza esa regla para que ningún caller la olvide.
//
// Nota: Can(PermUpload) IGNORA la lógica de cuota (used vs quota); eso
// vive en el repo (ReserveUploadBytes hace el chequeo atómico). Aquí
// sólo respondemos "¿tiene permiso?", no "¿le cabe?".
func (u User) Can(p Permission) bool {
	if u.IsOwner {
		return true
	}
	switch p {
	case PermManageAdmins:
		return u.CanManageAdmins
	case PermManageUsers:
		return u.CanManageUsers
	case PermManageLibraries:
		return u.CanManageLibraries
	case PermManageIPTV:
		return u.CanManageIPTV
	case PermEditMetadata:
		return u.CanEditMetadata
	case PermChangeArtwork:
		return u.CanChangeArtwork
	case PermViewAudit:
		return u.CanViewAudit
	case PermUpload:
		return u.CanUpload
	default:
		return false
	}
}

// IsAdmin: el usuario tiene capacidades de administración. Cubre
// tanto al owner como a admins con cualquier flag activo. Sustituye
// al antiguo `Role == "admin"` cuando el caller sólo quiere saber
// "¿tiene UI de admin?" sin importar exactamente qué puede tocar.
func (u User) IsAdmin() bool {
	return u.IsOwner || u.Role == "admin"
}

// IsProfile is the canonical readability helper around `ParentUserID`.
// Top-level account → false; profile dentro de un hogar → true.
func (u User) IsProfile() bool { return u.ParentUserID != "" }

// Session representa una sesión de login activa. El refresh token
// hash + el previous-hash permiten rotación con grace window
// (rationale en migración 038).
type Session struct {
	ID                       string
	UserID                   string
	DeviceName               string
	DeviceID                 string
	IPAddress                string
	RefreshTokenHash         string
	PreviousRefreshTokenHash string
	CreatedAt                time.Time
	LastActiveAt             time.Time
	ExpiresAt                time.Time
}

// SigningKey es el secreto HMAC usado para firmar JWT. La rotación
// + caching vive en `internal/auth/keystore.go`; el repo en
// `internal/db/signing_key_repository.go` sólo adapta sqlc al
// interface del keystore.
type SigningKey struct {
	ID        string
	Secret    string
	CreatedAt time.Time
	RetiredAt sql.NullTime
}

// DeviceCode persiste OAuth 2.0 device authorization grants
// (RFC 8628). El lifecycle: insert → poll/approve → consume →
// expire.
//
// Semántica de estados:
//
//	pending    user_id IS NULL
//	approved   user_id IS NOT NULL AND consumed_at IS NULL
//	consumed   consumed_at IS NOT NULL  (single-use post token issuance)
//	expired    expires_at < now()       (independiente del estado)
type DeviceCode struct {
	DeviceCode   string
	UserCode     string
	DeviceName   string
	UserID       sql.NullString
	ExpiresAt    time.Time
	CreatedAt    time.Time
	ApprovedAt   sql.NullTime
	ConsumedAt   sql.NullTime
	LastPolledAt sql.NullTime
}
