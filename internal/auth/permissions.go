package auth

import (
	"context"
	"net/http"

	"hubplay/internal/api/apperror"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/domain"
)

// PermissionStore es la mínima superficie del user repo que los
// middlewares de capability necesitan. Definida aquí (en vez de
// importar el repo concreto) para que tests pasen un fake sin DB.
type PermissionStore interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}

// PermissionChecker centraliza los middlewares por capability. Una
// sola instancia se cablea en el router; los handlers individuales
// piden Require(perm).
//
// El chequeo hace un GetByID por request — los admin surfaces son
// low-frequency comparados con /items, /stream, etc. Si más adelante
// se nota latencia, un cache TTL de 10s vale; no se hace ahora para
// no aumentar la superficie de "datos cacheados que pueden quedar
// stale" sin un caso real que lo justifique.
type PermissionChecker struct {
	users PermissionStore
}

// NewPermissionChecker es el constructor estándar. Acepta cualquier
// implementación de PermissionStore (en producción, *db.UserRepository).
func NewPermissionChecker(users PermissionStore) *PermissionChecker {
	if users == nil {
		panic("auth.NewPermissionChecker: nil store")
	}
	return &PermissionChecker{users: users}
}

// Require devuelve un middleware que sólo deja pasar usuarios cuyo
// User.Can(perm) == true. Owner pasa automáticamente vía la regla
// "owner tiene todo" dentro de Can(). Rechaza con 403 + código
// FORBIDDEN_PERMISSION para que el frontend distinga este 403 de
// otros (p.ej. content-rating).
func (c *PermissionChecker) Require(perm authmodel.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetClaims(r.Context())
			if claims == nil {
				apperror.Write(w, r.Context(), domain.NewUnauthorized("missing authorization"))
				return
			}
			u, err := c.users.GetByID(r.Context(), claims.UserID)
			if err != nil {
				apperror.Write(w, r.Context(), &domain.AppError{
					Code:       "USER_LOOKUP_FAILED",
					HTTPStatus: http.StatusInternalServerError,
					Message:    "could not resolve current user",
				})
				return
			}
			if !u.IsActive {
				apperror.Write(w, r.Context(), domain.NewForbidden("account disabled"))
				return
			}
			if !u.Can(perm) {
				apperror.Write(w, r.Context(), &domain.AppError{
					Code:       "FORBIDDEN_PERMISSION",
					HTTPStatus: http.StatusForbidden,
					Message:    "missing permission: " + string(perm),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireOwner es un middleware que sólo deja pasar al owner. Las
// operaciones más sensibles (backup DB, keystore, federation pairing,
// server restart, transferir el ownership, promover a admin) lo
// usan. Distintivo respecto a Require(PermManageAdmins): aquí ni
// siquiera un admin con todos los flags puede pasar.
func (c *PermissionChecker) RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r.Context())
		if claims == nil {
			apperror.Write(w, r.Context(), domain.NewUnauthorized("missing authorization"))
			return
		}
		u, err := c.users.GetByID(r.Context(), claims.UserID)
		if err != nil {
			apperror.Write(w, r.Context(), &domain.AppError{
				Code:       "USER_LOOKUP_FAILED",
				HTTPStatus: http.StatusInternalServerError,
				Message:    "could not resolve current user",
			})
			return
		}
		if !u.IsOwner {
			apperror.Write(w, r.Context(), &domain.AppError{
				Code:       "FORBIDDEN_OWNER_ONLY",
				HTTPStatus: http.StatusForbidden,
				Message:    "this operation is reserved for the instance owner",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
