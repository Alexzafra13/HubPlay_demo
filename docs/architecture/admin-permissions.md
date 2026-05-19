# Permisos de Administración — Design Document

## Overview

HubPlay tiene **un único administrador principal** (el "owner", el que
instala la app) y **administradores secundarios** con capacidades
granulares. La migración 055 introduce los flags; los middlewares
HTTP los enforzan; la UI `/admin/users` los gestiona.

**Decisión clave**: flags granulares en `users` (RBAC plano), no una
tabla `user_permissions` separada. Coherente con el patrón inaugurado
por `can_upload` (migración 053). Si la lista crece más allá de ~10
flags, migrar a tabla es trivial — empezamos simple.

---

## 1. Modelo

### El owner

```sql
-- En la tabla users:
is_owner BOOLEAN NOT NULL DEFAULT 0

-- Constraint:
CREATE UNIQUE INDEX idx_users_one_owner ON users(is_owner)
    WHERE is_owner = 1;
```

**Invariantes**:

1. Como máximo UN owner en la DB (índice parcial UNIQUE).
2. El owner es **inmutable de por vida**: no hay endpoint HTTP para
   transferirlo. Si el operador pierde acceso a la cuenta owner,
   recovery vía shell + edición DB. Razón: añadir UI de "transferir
   ownership" abre superficie de escalada (admin secundario
   convenciendo al owner de pulsar el botón, etc.). Self-hosted no
   tiene equity para considerar transfer-of-ownership.
3. El owner pasa **todo** check de permiso. `User.Can(perm)` devuelve
   `true` para cualquier `perm` cuando `IsOwner=true`. Centralizado
   en un único método para que ningún caller olvide la regla.

### Los flags granulares

| Flag | Habilita |
|---|---|
| `can_manage_admins` | Modificar flags de OTROS admins existentes (no crear nuevos). |
| `can_manage_users` | Alta/baja usuarios normales, reset password, library_access, perfiles. |
| `can_manage_libraries` | CRUD librerías, paths, trigger scans, configurar providers TMDb. |
| `can_manage_iptv` | M3U, EPG sources, canales (disable/enable/logo). |
| `can_edit_metadata` | Título, descripción, identify TMDb, lock. |
| `can_change_artwork` | Posters, fondos, logos, batch image refresh. |
| `can_view_audit` | Acceso a logs de auditoría (uploads, sesiones). |
| `can_upload` | Subir media (PR1 existente). |

Todos `BOOLEAN NOT NULL DEFAULT 0`. Un usuario normal nuevo nace con
TODOS en `false`. Un admin recién promovido también — el owner le da
flags via la matriz.

### Reglas que NO viven en flags

Algunas decisiones son owner-only por seguridad, no por capability:

- **Crear admins** (POST `/users` con `role=admin`). El handler chequea
  `requester.IsOwner` independientemente de `can_manage_users`. Razón:
  defensa anti-"admin sprawl" — un admin con can_manage_users no puede
  fabricar admins paralelos.
- **Promover/degradar role** (PUT `/users/{id}/role`). Ruta gated por
  `RequireOwner` explícito.
- **Backup DB, keystore JWT, federation pairing, restart, DB swap**.
  Todas gated por `RequireOwner` en el router. Son operaciones que
  pueden exfiltrar o reemplazar la DB entera, comprometer JWT, o pair
  con un peer hostil.

---

## 2. Backfill y bootstrap

### Instalación existente (migración 055 aplicada con admins ya en DB)

La migración hace:

```sql
-- Todos los admins existentes reciben TODOS los flags
-- (preserva el comportamiento previo de RequireAdmin)
UPDATE users SET can_manage_admins = 1, can_manage_users = 1, ...
WHERE role = 'admin' AND parent_user_id IS NULL;

-- El admin más antiguo se convierte en owner
UPDATE users SET is_owner = 1
WHERE id = (
    SELECT id FROM users
    WHERE role = 'admin' AND parent_user_id IS NULL
    ORDER BY created_at ASC LIMIT 1
);
```

### Instalación nueva (DB vacía al aplicar la migración)

El backfill no hace nada (tabla vacía). El **setup wizard** crea el
primer admin via `/auth/setup` y luego llama a `users.EnsureOwner(id)`,
que es idempotente: si NO hay owner aún, lo asigna; si ya hay, no toca.

Sin esto, una instalación fresca quedaría sin owner y `RequireOwner`
devolvería 403 en backup/keystore/federation/restart — instalación
rota. El bug fue identificado y arreglado durante PR3 (ver commit del
fix).

### Recuperación manual

Si el owner pierde acceso (contraseña perdida, cuenta borrada por
accidente), no hay UI. El operador entra al host y:

```sql
-- 1. Resetear el owner a otro admin existente:
BEGIN;
UPDATE users SET is_owner = 0 WHERE is_owner = 1;
UPDATE users SET is_owner = 1,
    can_manage_admins = 1, can_manage_users = 1, can_manage_libraries = 1,
    can_manage_iptv = 1, can_edit_metadata = 1, can_change_artwork = 1,
    can_view_audit = 1
WHERE id = '<new_owner_id>';
COMMIT;
```

El UNIQUE WHERE is_owner=1 permite las dos UPDATE consecutivas dentro
de la TX porque entre statements queda momentáneamente sin owner.

---

## 3. Middlewares HTTP

`internal/auth/permissions.go`:

```go
// PermissionChecker.Require(perm) — un middleware por capability.
// Hace GetByID del current user en CADA request (admin surfaces son
// low-frequency; cache TTL queda para cuando se note latencia).
func (c *PermissionChecker) Require(perm authmodel.Permission) func(http.Handler) http.Handler

// RequireOwner — gate exclusivo para owner. Ni un super-admin con
// todos los flags lo pasa.
func (c *PermissionChecker) RequireOwner(next http.Handler) http.Handler
```

### Aplicación en el router

```go
// /users (CRUD usuarios normales)
r.Use(deps.Permissions.Require(authmodel.PermManageUsers))

// PUT /users/{id}/role (promote/demote admin)
r.With(deps.Permissions.RequireOwner).Put("/{id}/role", userHandler.SetRole)

// PUT /users/{id}/permissions (modificar flags de un admin)
r.With(deps.Permissions.Require(authmodel.PermManageAdmins)).
    Put("/{id}/permissions", permHandler.PutPermissions)

// /admin/auth/keys, /admin/peers, /admin/system/{backup,db,restart}
r.Use(deps.Permissions.RequireOwner)

// /libraries POST/PUT/DELETE/scan
r.Use(deps.Permissions.Require(authmodel.PermManageLibraries))

// IPTV admin (M3U, EPG, channels)
r.Use(deps.Permissions.Require(authmodel.PermManageIPTV))

// Item identify + metadata edits
r.Use(deps.Permissions.Require(authmodel.PermEditMetadata))

// Collection image overrides + batch image refresh
r.Use(deps.Permissions.Require(authmodel.PermChangeArtwork))

// Providers config
r.Use(deps.Permissions.Require(authmodel.PermManageLibraries))
```

### `/admin/system` — el mixed bag

`/admin/system` sigue gated por `RequireAdmin` en bloque por ahora.
Contiene reads (stats, top-items, recently-added) y mutaciones (kill
session, settings updates). Refinar cada endpoint requiere decidir
caso por caso qué capability le toca; backup/db/restart YA están en
un sub-Group con `RequireOwner` dentro.

---

## 4. Endpoints HTTP

### Lectura

```
GET /api/v1/users/{id}/permissions
    Gate: can_manage_users (heredado del grupo)
    Devuelve los 8 flags del usuario.
```

### Mutación

```
PUT /api/v1/users/{id}/permissions
    Gate: can_manage_admins (middleware) + reglas finas en handler
    Body parcial (cada flag es *bool):
      { "can_edit_metadata": true, "can_view_audit": false }

    Reglas del handler:
    1. Si target.is_owner → 403 OWNER_IMMUTABLE.
    2. Si target.role != 'admin' → 400 TARGET_NOT_ADMIN.
    3. Si body otorga can_manage_admins Y requester no es owner →
       403 OWNER_ONLY. (Sólo el owner replica can_manage_admins.)
    4. Aplica los flags pasados (no toca los demás).
    5. Refresca el estado y lo devuelve.
```

### Promoción / degradación

```
PUT /api/v1/users/{id}/role
    Gate: RequireOwner (middleware)
    Body: { "role": "admin" | "user" }

    Sólo el owner promueve o degrada. Defensa anti-sprawl.
```

### Creación de admins

```
POST /api/v1/users
    Gate: can_manage_users (middleware)
    + handler chequea: si body.role == "admin", requester DEBE ser owner.

    Permite a can_manage_users crear usuarios normales sin poder
    fabricar admins.
```

---

## 5. Frontend

### `/me` extendido

El endpoint `/api/v1/me` devuelve los 8 flags + `is_owner`. El
frontend los usa para esconder pestañas / botones que el usuario no
puede tocar. **NUNCA** confía sólo en esto — el backend re-valida
cada mutación.

### Matriz de permisos

`web/src/pages/admin/AdminPermissionsMatrix.tsx` — tabla con admins
en filas, 7 columnas de capability. Reglas visuales:

- **Owner**: badge dorado "Principal" en la fila, TODAS sus celdas
  marcadas + disabled.
- **can_manage_admins** (primera columna): editable sólo si
  `viewer.is_owner === true`. Para admins secundarios con
  can_manage_admins, esa columna queda disabled con tooltip "Sólo el
  principal puede otorgar".
- **Otras columnas**: editables si viewer es owner OR tiene
  can_manage_admins.
- **Admins sin can_manage_admins**: la matriz es read-only (siguen
  viendo quién tiene qué, sin poder cambiar nada).

Click en checkbox = mutación inmediata (no hay botón "Guardar").
Errores del backend (OWNER_IMMUTABLE, OWNER_ONLY) se muestran inline
bajo la celda en lugar de a nivel página.

### Routing

`/admin/users` (existente). La matriz sólo se renderiza en desktop —
8 columnas no caben en mobile y rompería el contrato del test "mobile
renders cards no table". En mobile el operador puede crear/borrar
admins pero no editar sus flags.

---

## 6. Decisiones y rationales

### ¿Por qué flags en `users` en vez de tabla `user_permissions`?

Pros del approach actual:
- Una sola query devuelve el usuario con todos sus flags. Sin JOINs.
- Coherente con `can_upload` (migración 053).
- El struct `User` lleva la verdad — no hay drift entre "permisos en
  la DB" y "permisos materializados".

Cons asumidos:
- Si crecemos a 15+ permisos, la tabla se ensucia. Coste de migrar a
  `user_permissions` cuando llegue: una migración + adaptar
  `User.Can()` para que mire un mapa. Trivial el día que se necesite.

### ¿Por qué owner inmutable?

Self-hosted con un solo operador real. Transfer of ownership es un
caso degenerate que abre escalada (un admin secundario sabe que con
ingeniería social puede convencer al owner de transferirle). Quitar
el botón evita el ataque por construcción. Si el operador necesita
cambiar de mano, el shell access al host es la vía. Aplicar Occam.

### ¿Por qué fetch del user en cada middleware en vez de JWT claims?

JWT son short-lived (15min). Meter flags en el claim significaría
que un cambio de permiso tarda hasta 15min en propagar (o forzar
revocación de refresh tokens, complicado). DB-fetch en cada request
admin es barato (admin surfaces son low-frequency; estamos hablando
de docenas de req/min como mucho). Cache TTL queda como optimización
si en el futuro un dashboard rapidísimo lo justifica.

### ¿Por qué can_upload está en el mismo set?

Porque el modelo `User.Can()` ya lo cubre. La diferencia operativa es
que `can_upload` se otorga a usuarios normales también (es una
capacidad de usuario, no de operador). Estructuralmente el mismo flag.

---

## 7. Tests

- `internal/db/user_permissions_test.go`: SetPermission, EnsureOwner,
  GetOwnerID, Can() helper.
- `internal/auth/permissions_test.go`: middleware Require y
  RequireOwner — owner passa todo, flag granular, inactivo rechazado,
  lookup fail, super-admin no pasa owner gate.
- `internal/api/handlers/permissions_test.go`: GET/PUT permissions
  con reglas finas (owner inmutable, owner-only para can_manage_admins).
- `internal/api/handlers/auth_test.go`: Register reglas anti-sprawl
  (non-owner no crea admins) + EnsureOwner en Setup.
- `web/src/pages/admin/AdminPermissionsMatrix.test.tsx`: 7 tests del
  componente — empty state, owner badge, owner disabled, gate de
  can_manage_admins, read-only para admin sin flag.

---

## 8. Futuro

### Refinar `/admin/system`

Actualmente bloque RequireAdmin. Endpoints concretos:

- `GET /admin/system/{stats,stream-activity,top-items,recently-added,
  storage/disks}` → razonable `RequireAdmin` (lecturas dashboard).
- `DELETE /admin/system/sessions/{id}` (kill session) → ¿permiso
  nuevo `can_kill_sessions` o `can_manage_users`?
- `PUT /admin/system/settings/{key}` (system settings) → ¿permiso
  nuevo `can_change_settings` o owner-only?
- `GET /admin/system/logs/*` → `can_view_audit` parece lógico.

Cada refinement necesita un test pin + actualización del frontend
(esconder botones que el viewer no puede tocar). Producto-driven, no
ingeniería-driven.

### Token de revocación por usuario

Si un admin secundario se compromete, hoy hay que esperar a que
expire su access JWT (15min) o invalidar TODOS los refresh tokens.
Un mecanismo de revocación selectiva (lista de jti revocados,
o `is_active=false` + check en middleware) sería el siguiente
paso natural.

### Audit log de cambios de permisos

Hoy un PUT `/users/{id}/permissions` no deja rastro. Un audit log
similar al de uploads (tabla `permission_changes`) sería útil para
"quién le dio a Bob el flag can_manage_libraries y cuándo".
