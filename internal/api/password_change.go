package api

import (
	"context"
	"net/http"

	"hubplay/internal/api/apperror"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/domain"
)

// userByIDLookup es la superficie mínima que necesita EnforcePasswordChange.
type userByIDLookup interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}

// isPasswordChangeAllowed lista los paths mutantes permitidos mientras el
// usuario arrastra password_change_required: cambiar su propia contraseña
// y cerrar sesión. Cualquier otra mutación se bloquea.
func isPasswordChangeAllowed(path string) bool {
	switch path {
	case "/api/v1/me/password", "/api/v1/auth/logout":
		return true
	default:
		return false
	}
}

// EnforcePasswordChange bloquea con 403 las peticiones mutantes
// (POST/PUT/PATCH/DELETE) de un usuario con password_change_required
// activo, salvo el allowlist. Cierra el olor M2: antes el flag era solo
// advisory — el server emitía tokens válidos y el frontend era quien
// enrutaba a la pantalla de cambio, así que un cliente API con password
// temporal podía saltarse la rotación y operar igual.
//
// El check mira el flag en DB (no el JWT) a propósito: ChangePassword
// limpia el flag pero NO re-emite el access token, así el desbloqueo es
// inmediato sin esperar a que rote el token (hasta 15 min). El lookup
// solo ocurre en métodos mutantes fuera del allowlist — los GET del hot
// path no pagan nada.
func EnforcePasswordChange(users userByIDLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) || isPasswordChangeAllowed(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			claims := auth.GetClaims(r.Context())
			if claims != nil && users != nil {
				if u, err := users.GetByID(r.Context(), claims.UserID); err == nil && u != nil && u.PasswordChangeRequired {
					apperror.Write(w, r.Context(), &domain.AppError{
						Code:       "PASSWORD_CHANGE_REQUIRED",
						HTTPStatus: http.StatusForbidden,
						Message:    "debes cambiar tu contraseña temporal antes de continuar",
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
