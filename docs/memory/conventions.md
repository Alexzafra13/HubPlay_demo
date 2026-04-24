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

### Frontend — Vitest + Testing Library: gotchas descubiertos trabajando

Tres patrones que reventaron la primera vez y merece la pena saber de
antemano. Todos aplicados en `web/src/components/livetv/*.test.*`.

**1. `user-event` + `vi.useFakeTimers` deadlockea con componentes que
tienen `setTimeout` interno.** La cola interna de user-event usa
`setTimeout(fn, 0)` para secuenciar eventos; bajo timers mock esa cola
no avanza, el `await user.click(...)` nunca resuelve, el test expira a
los 5 s. El caso que reventó: `ChannelCard` tiene un debounce de 250 ms
en el hover. Fix: usar `fireEvent` síncrono, que dispara los mismos
handlers DOM sin ceremonia async.

```tsx
// ✗ Se queda colgado con fake timers
await user.click(screen.getByRole("button"));

// ✓ Sincrónico, funciona con fake timers
fireEvent.click(screen.getByRole("button"));
```

**2. `<img alt="">` decorativas quedan fuera del accessibility tree.**
Por WAI-ARIA, una imagen con `alt=""` le dice al lector de pantalla
"ignórame" — y Testing Library respeta ese contrato. `getByRole("img")`
no las encuentra. Para tests que necesitan comprobar presencia/ausencia
del `<img>` (como el regression test del fallback de logo), usar
`container.querySelector("img")`.

**3. `useTranslation()` + `defaultValue` funciona sin provider i18n en
tests.** El codebase tiene la convención de siempre pasar
`t("key", { defaultValue: "texto" })`. Cuando no hay i18n inicializado
la key no se encuentra y react-i18next devuelve el `defaultValue`. Por
eso los tests de livetv **no montan ningún provider** y los componentes
renderizan el texto en español por defecto. Si un componente usa
`t("key")` sin defaultValue, el test devuelve la string `"key"` tal cual
— eso es señal de que hay que añadir el fallback, no inicializar i18n.

### Frontend — Patrones de fixture

- **Reloj fijo**: `const NOW = new Date("2026-04-24T12:00:00Z").getTime();`
  + `vi.useFakeTimers()` + `vi.setSystemTime(NOW)` en `beforeEach`.
  Cualquier `Date.now()` o `new Date()` dentro del componente lee el
  instante mockeado, asserts deterministas.
- **Factory con `Partial<T>` overrides**:

  ```ts
  function channel(overrides: Partial<Channel> = {}): Channel {
    return { /* defaults */, ...overrides };
  }
  ```

  Reduce boilerplate por test y deja los overrides visibles en el
  callsite.
- **Mockear hooks de API con `vi.mock`** en vez de levantar un
  `QueryClientProvider` cuando el test es pura lógica. Ejemplo
  canónico: `useHeroSpotlight.test.ts` mockea `useUserPreference` con
  una tupla `[mode, setMode]` y testea la cadena de fallback sin
  tocar red ni react-query.

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

---

## Frontend — patrones detectados en livetv

### Re-render periódico → `useNowTick(ms)`

Cualquier componente que muestre datos derivados de `Date.now()`
(barra de progreso, línea "ahora", filtrado "próximos X") necesita
re-renderizar cada cierto tiempo sin que el caller tenga que
instanciar su propio `setInterval`. El hook vive en
`web/src/hooks/useNowTick.ts` y se usa en `EPGGrid`, `PlayerOverlay` y
`HeroSpotlight`. Default 30 s: suficiente para granularidad de minutos
sin re-renders innecesarios.

**Regla**: nunca `getProgramProgress(x)` o similar en el render sin un
`useNowTick` arriba del árbol — si no, la barra se congela.

### Política "qué ver en el hero" → hook, no componente

`HeroSpotlight` es presentacional puro. Toda la lógica de "qué señal
se prioriza, cómo caer si está vacía, qué texto poner arriba" vive en
`useHeroSpotlight` (`web/src/components/livetv/useHeroSpotlight.ts`).
Así la policy es testeable sin tocar layout y si mañana añadimos una
nueva señal ("más vistos", "continuar viendo") no hay que tocar el
componente visual.

### Una sola fuente de verdad para listas de UI

Si una lista ordenada aparece en dos sitios (chips + rails,
sidebar + dropdown, …) extraer a un módulo dedicado con un array
exportado. Patrón aplicado en `web/src/components/livetv/categoryOrder.ts`
para el orden de las 13 categorías de canal. Antes duplicado entre
`LiveTV.tsx:railOrder` y `CategoryChips.tsx:defaultOrder`: cambiar el
orden obligaba a tocar los dos y era fácil que divergieran.

### Fallback de `<img>` con `onError` → URL-keyed state

Esconder el `<img>` con `e.currentTarget.style.display = "none"` en
`onError` sólo funciona si el contenido de fallback se renderiza **a
la vez**, no en la rama opuesta de un ternario. Patrón robusto:

```tsx
const [failedUrl, setFailedUrl] = useState<string | null>(null);
const show = !!url && failedUrl !== url;
// show ? <img src={url} onError={() => setFailedUrl(url)} /> : <Fallback />
```

Derivar `show` de props (url + failedUrl) en vez de usar
`useEffect(() => setOk(true), [url])` evita el warning
`react-hooks/set-state-in-effect` que el lint del proyecto enforzea.
