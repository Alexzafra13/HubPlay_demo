package api

import (
	"github.com/go-chi/chi/v5"

	authhandler "hubplay/internal/api/handlers/auth"
	"hubplay/internal/api/handlers/users"
	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
)

// mountUsers registra todo el surface de /users/*. La mayoría va
// gateado por can_manage_users (alta/baja/edición); cuatro rutas
// (pin, display-name, avatar-color, serve-avatar) se quedan fuera del
// gate porque obedecen a la matriz admin-OR-parent-of-target-OR-self,
// que el handler enforza directamente.
func mountUsers(
	r chi.Router,
	authHandler *authhandler.AuthHandler,
	userHandler *users.UserHandler,
	deps Dependencies,
) {
	r.Route("/users", func(r chi.Router) {
		// can_manage_users (migración 055). El gate cubre alta/baja/
		// edición de usuarios normales + perfiles. Casos especiales
		// (que siguen gated más fino):
		//  - POST /users con role=admin → handler chequea owner.
		//  - PUT  /users/{id}/role     → RequireOwner explícito.
		//  - PUT  /users/{id}/permissions → can_manage_admins.
		// El GET / (List) también pasa este gate; admins sin
		// can_manage_users no pueden listar — preferimos eso a
		// leaks de info entre admins parcialmente trusted.
		if deps.Auth.Permissions != nil {
			r.Use(deps.Auth.Permissions.Require(authmodel.PermManageUsers))
		} else {
			r.Use(auth.RequireAdmin)
		}
		r.Get("/", userHandler.List)
		r.Post("/", authHandler.Register)
		r.Delete("/{id}", userHandler.Delete)
		r.Post("/{id}/reset-password", authHandler.ResetPassword)
		r.Put("/{id}/content-rating", authHandler.SetContentRating)
		// SetRole es owner-only (migración 055): sólo el owner
		// promueve a admin o degrada uno. can_manage_admins
		// gestiona FLAGS de admins ya existentes, no el role.
		if deps.Auth.Permissions != nil {
			r.With(deps.Auth.Permissions.RequireOwner).Put("/{id}/role", userHandler.SetRole)
		} else {
			r.Put("/{id}/role", userHandler.SetRole)
		}
		r.Put("/{id}/active", userHandler.SetActive)
		r.Put("/{id}/access", userHandler.SetAccess)
		// Library access matrix. GET pinta la admin UI (current
		// grants for the target's household); PUT reemplaza el
		// set entero transaccionalmente. Los grants siempre
		// targetan al top-level user — pasar un profile id a PUT
		// devuelve 400 (ADR-014). El GET counterpart normaliza
		// profile ids al parent así que el frontend puede pintar
		// el set inherited sin extra round-trip.
		r.Get("/{id}/library-access", userHandler.GetLibraryAccess)
		r.Put("/{id}/library-access", userHandler.SetLibraryAccess)
		// Personal IPTV library shortcut. Crea una livetv library
		// + grants acceso sólo a este usuario en una tx, así el
		// admin no tiene que navegar a /admin/libraries primero y
		// volver a tickear la nueva lib en la access matrix.
		r.Post("/{id}/iptv-libraries", userHandler.CreatePersonalIPTV)

		// Permission flags (migración 055). El read es admin-only
		// genérico; el write está gated por can_manage_admins. El
		// owner es inmutable — sin endpoint de transferencia.
		if deps.Auth.Permissions != nil && deps.Auth.UserRepo != nil {
			permHandler := users.NewPermissionsHandler(deps.Auth.UserRepo, deps.Infra.Audit, deps.Infra.Logger)
			r.Get("/{id}/permissions", permHandler.GetPermissions)
			r.With(deps.Auth.Permissions.Require(authmodel.PermManageAdmins)).
				Put("/{id}/permissions", permHandler.PutPermissions)
		}
	})

	// PIN management — auth-only (el handler enforza la matriz
	// admin-OR-parent-of-target-OR-self). Vive fuera del bloque
	// /users admin-gated así que el padre de un perfil puede
	// usarlo sin tener el rol admin.
	r.Put("/users/{id}/pin", authHandler.SetPIN)
	// Display-name rename — misma matriz de autorización que SetPIN
	// (admin OR parent-of-target OR self) así que un padre puede
	// renombrar sus profile members desde el picker sin necesitar
	// el rol admin.
	r.Put("/users/{id}/display-name", userHandler.SetDisplayName)
	// Avatar colour override — misma matriz que SetDisplayName.
	// Vive fuera del bloque /users admin-gated así que un padre
	// puede recolorear su profile member sin tener el rol admin.
	r.Put("/users/{id}/avatar-color", userHandler.SetAvatarColor)
	// Servir el avatar subido. Auth-gated igual que el resto del
	// bloque — los clientes ya tienen sesión cuando lo renderizan
	// (lista admin, picker de perfil, TopBar). El path es uniforme
	// para todos los avatares aunque cambie el fichero subyacente:
	// la URL incluye ?v=<rel> como cache-buster, no en el path.
	r.Get("/users/{id}/avatar", userHandler.ServeUserAvatar)
}
