# Convenciones del código HubPlay

> Patrones descubiertos trabajando, no imaginados. Cada entrada se añade
> solo después de haberlo aplicado o detectado en el codebase.
> Si contradice la realidad del código, la realidad gana y se actualiza esto.

---

## Capa de DB — patrón sqlc adapter

Desde ADR-001. Todos los repos nuevos y migraciones siguen uno de estos dos
sabores, en este orden de preferencia:

### Sabor A — Type alias (fields 1:1)

Cuando el struct `sqlc.Foo` tenga **exactamente los mismos campos** que el
dominio (mismo nombre, mismo tipo, misma nullability):

```go
// Alias, sin conversión. Cero ripple fuera de internal/db/.
type Foo = sqlc.Foo

type FooRepository struct{ q *sqlc.Queries }

func (r *FooRepository) GetByID(ctx context.Context, id string) (*Foo, error) {
    x, err := r.q.GetFoo(ctx, id)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, fmt.Errorf("foo %s: %w", id, domain.ErrNotFound)
    }
    if err != nil {
        return nil, fmt.Errorf("get foo: %w", err)
    }
    return &x, nil
}
```

Ejemplo canónico: `signing_key_repository.go`.

### Sabor B — Adapter con struct propio

Cuando el schema use nullable y el dominio no lo quiera, cuando el casing
difiera (`IpAddress` vs `IPAddress`), o cuando se quiera esconder el tipo
sqlc del resto del código:

```go
// Struct propio, convertido en el boundary.
type Session struct {
    IPAddress string // plain string en dominio ("" = unknown)
    // ...
}

func sessionFromRow(r sqlc.Session) Session {
    return Session{
        IPAddress: r.IpAddress.String, // NullString → string
        // ...
    }
}

func nullableString(s string) sql.NullString {
    if s == "" {
        return sql.NullString{}
    }
    return sql.NullString{String: s, Valid: true}
}
```

Ejemplo canónico: `session_repository.go`.

### Regla para elegir

- Si `type X = sqlc.Y` compila y pasa tests → **alias**.
- Si falla por casing, nullability o tipo → **adapter** con helpers
  explícitos (`fooFromRow`, `nullableString`, etc.).

### Error mapping

- `sql.ErrNoRows` → `domain.ErrNotFound` con `errors.Is`, nunca `==`.
- Otros errores: `fmt.Errorf("operación: %w", err)` para preservar la cadena.
- `Delete` sin afectar filas: preserva el comportamiento previo (algunos
  repos tragaban, otros devolvían NotFound). No homogeneizar de paso,
  hacerlo en un commit separado si es necesario.

### Nullable semantics

El adapter decide la correspondencia `""` ↔ `NULL`. Default aplicado en
sessions: cadena vacía → NULL (semántica correcta, `""` y `NULL` observan
igual en el dominio, SQLite queda limpio).

---

## Regeneración sqlc

Después de editar cualquier `internal/db/queries/*.sql` **o** cualquier
`migrations/sqlite/*.sql`:

```powershell
# Windows con Docker Desktop (sin instalar sqlc)
docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc generate

# Cualquier OS con sqlc instalado
sqlc generate
# o
make sqlc
```

**Commit los generados**. Convención de sqlc y evita obligar a cada dev a
tener la herramienta para compilar.

---

## Queries con self-join o subquery

Si una query referencia la misma tabla dos veces (subquery SELECT con el
mismo FROM que el outer DELETE/UPDATE), **alias la interna**. Sin alias,
sqlc falla con "column reference is ambiguous" aunque semánticamente sea
claro:

```sql
-- ✗ falla en sqlc
DELETE FROM sessions WHERE id = (
    SELECT id FROM sessions WHERE user_id = ? ...
);

-- ✓ pasa
DELETE FROM sessions WHERE id = (
    SELECT s.id FROM sessions s WHERE s.user_id = ? ...
);
```

Dejar un comentario de una línea en el `.sql` explicando el alias.

---

## Anti-ciclos de paquetes

Patrones encontrados en el código que valen la pena preservar:

### Resolver por función vs interface cuando hay ciclo

`internal/auth/jwt.go:24` define `type keyResolver func(kid string) (*db.SigningKey, error)`
en vez de depender de un `*KeyStore`. Evita que `jwt.go` importe keystore y
que keystore importe jwt para el mismo ciclo. Patrón:

```go
// El tipo "interface-as-function" permite que el caller pase un cierre sobre
// cualquier fuente sin crear dependencia entre paquetes.
type keyResolver func(kid string) (*db.SigningKey, error)
```

### Sink interface para observabilidad sin importar Prometheus

`internal/stream/` declara su propio `MetricsSink` interface con métodos
abstractos, y `internal/observability/` implementa esa interface. Así
`stream/` no importa Prometheus (sería ciclo si Prometheus acabara tocando
streams).

### Interface estrecha en el consumer, no en el productor

`internal/auth/keystore.go:21-28` declara una interface `signingKeyRepo`
que es el subset de métodos de `*db.SigningKeyRepository` que la keystore
realmente usa. Va **en el paquete consumidor**, no en `db/`. Permite
mockear sin exportar interfaces globales.

---

## Tests

### `-race` requiere CGO

El driver `modernc.org/sqlite` es pure-Go por diseño (para binario único
sin toolchain C). El race detector de Go **necesita CGO**. Por tanto:

- En Windows / cualquier máquina sin gcc: `go test -count=1 ./...` (sin
  `-race`). El Makefile actual con `test: go test -race` no funciona ahí.
- En CI / Linux con gcc: `go test -race -count=1 ./...`.

TODO: dividir el target `test` del Makefile en `test` (portable) y
`test-race` (requiere CGO).

### Skip bajo root en tests de permisos

Los tests que manipulan permisos (`chmod`, PATH, cache dir no escribible)
skipan cuando el proceso corre como root (devbox / algunos CI runners).
Ejemplo en `internal/config/preflight_test.go`. No es un bug del test, es
por diseño — bajo root no se pueden simular fallos de permiso.

---

## Handlers HTTP

### Respuesta con envelope `{"data": ...}` / `{"error": ...}`

Todos los handlers devuelven JSON envuelto. `respondJSON`, `respondError`
y `respondAppError` en `internal/api/handlers/responses.go` son los
únicos puntos de salida correctos.

### Excepciones conocidas (deuda pendiente, ver `audit-2026-04-15.md`)

- `internal/api/csrf.go:61`
- `internal/auth/middleware.go:18,24,38`

Usan `http.Error()` con string JSON crudo → rompen Content-Type y no
pasan por el `ErrorRecorder` de observabilidad. Migrarlos cuando se
toque la zona.

### Handler construye su propio response map, no serializa structs de DB

Ver `internal/api/handlers/admin_auth.go:49-64`. Esto protege contra
exponer campos sensibles (como `Secret` de una signing key) por olvido
de un tag JSON. No aliases `type Foo = sqlc.Foo` si la intención es
serializarla directamente — los tags JSON de sqlc llevan `snake_case`
y campos potencialmente sensibles.

---

## Docs

### `docs/architecture/` describe código que **existe**

Si un diseño no tiene una línea de código detrás, **no va ahí** — va a
`docs/roadmap/` (a crear cuando toque) o a un issue. `plugins.md` y
`federation.md` violan esto hoy y son deuda (ver audit §5).

### `docs/memory/` es working memory, se actualiza cada sesión

- `project-status.md` — qué se hizo, qué falta, próximos pasos
  concretos. Actualizar **antes** de cerrar sesión.
- `architecture-decisions.md` — ADRs numerados; no se editan cerrados.
- `conventions.md` — este fichero; se añade, no se reescribe.
- `audit-YYYY-MM-DD.md` — snapshots puntuales, se archivan por fecha.

### `CLAUDE.md` solo con datos estables

No cifras de LOC, no nombre de rama activa, no estado de sprint. Eso
envejece muy rápido y se mueve a `docs/memory/project-status.md`.
