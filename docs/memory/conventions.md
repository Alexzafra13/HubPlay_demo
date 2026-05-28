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

### Sabor C — Sub-package owns su repo (notification, federation/storage)

Excepción deliberada al patrón "todos los repos viven en `internal/db/`".
Algunas features tienen su propia persistencia dentro del paquete de
dominio:

- `internal/federation/storage/` — wrappers sqlc dual-driver sobre las
  queries de federation. La feature owns su persistencia.
- `internal/notification/storage.go` — mismo patrón pero en el package
  raíz de la feature (un solo fichero, no merece sub-package).

**Cuándo aplicar**:
- La feature ya tiene su sub-paquete (`internal/X/`) con tipos propios
  y la persistencia es **exclusiva** de esa feature — no la consume
  ningún otro paquete.
- Mover el repo a `internal/db/X_repository.go` obligaría a `internal/db`
  a importar tipos de `internal/X/` (inversión de capa).

**Reglas innegociables**:
1. El repo **expone tipos del paquete propio** (`notification.Notification`,
   `*federation.SharedItem`), nunca `sqlc.X` ni `db.X`. La conversión
   vive en el repo (`notificationFromSqliteRow`, etc.), no en el caller.
2. Los imports `hubplay/internal/db/sqlc` y `db/sqlc_pg` son aceptables
   **solo dentro del fichero del repo** del sub-package. No se propagan
   a handlers, services ni tests externos.
3. El paquete `internal/db` queda como **infra pura**: `Open`, `Migrate`,
   `NewRepositories` para repos compartidos, dialect, drivers. No se
   infla con queries feature-específicas.

**Lo que NO es válido**:
- Importar `db/sqlc` desde un fichero que no sea el repo del sub-package
  (handler, service, otro paquete).
- Exponer `sqlc.X` en la firma pública del repo.

Cierra **SS-1** del audit per-package 2026-05-27 — el patrón no es un
olor, es una decisión consistente entre dos features.

---

## Regeneración sqlc

`make sqlc` regenera `internal/db/sqlc/*.sql.go` desde
`internal/db/queries/*.sql`. Idempotente — sin diff cuando las queries
no han cambiado. El target instala primero la versión pineada de sqlc
(`SQLC_VERSION` en el Makefile) si no está ya en el PATH, así que un
contributor nuevo no tiene que gestionar su propia instalación.

```bash
make sqlc        # regenera (instala sqlc pineado si hace falta)
make sqlc-verify # regenera y falla si hay diff (lo usa CI)
```

**Commit los generados** junto con el cambio de query. Convención
estándar de sqlc y evita que cada dev tenga la herramienta para
compilar.

### Drift test en CI

`internal/db/sqlc_drift_test.go` regenera contra un directorio scratch
y compara byte-a-byte con el árbol commiteado. Falla con tres clases de
regresión:

1. **Olvidaste correr `make sqlc`** tras editar una query.
2. **Introdujiste un patrón parser-hostile** en una query (ver lista
   abajo). El regen produce output corrupto en silencio; el drift test
   lo destapa.
3. **Subiste `SQLC_VERSION`** en el Makefile sin re-baselinear.

El test se skipea si `sqlc` no está en PATH (developer local sin la
herramienta no se bloquea). CI siempre lo ejecuta porque el
prerequisito `make sqlc-install` lo instala.

### Patrones parser-hostile a evitar en `*.sql`

Bugs reales de sqlc 1.27 / 1.29 / 1.31 confirmados empíricamente
(sesión 2026-05-04). Cualquier query con estos patrones rompe el
regen en silencio — el drift test los caza, pero mejor evitarlos
desde el inicio:

1. **Caracteres no-ASCII en comentarios SQL**. Em-dashes (`—`),
   acentos (`á í ó`), backticks (`` ` ``), comillas tipográficas, etc.
   El parser cuenta bytes vs chars mal en UTF-8 multi-byte y a
   partir de un char no-ASCII las posiciones quedan desplazadas N
   bytes. Síntoma: queries posteriores salen truncadas en los
   últimos 1-2 caracteres (`LIMIT ?` → `LIMIT`, `'season'` → `'seaso`).

   **Convención**: comentarios SQL en ASCII puro. Si necesitas un
   guion largo, usa `--` (doble hyphen, que SQL ya interpreta como
   inicio de comentario y se renderiza visualmente como un dash en
   muchos editores).

2. **Placeholders `?` dentro de `NOT (...)` clauses**. sqlc no
   detecta el parámetro anidado y lo descarta del struct `Params`.
   Síntoma: el campo desaparece y el `QueryContext` no lo pasa →
   SQLite recibe un `?` sin valor → error en runtime.

   **Convención**: si necesitas negar un grupo de condiciones que
   incluye un parámetro, usa **DeMorgan** y exprésalo positivamente:

   ```sql
   -- ✗ rompe sqlc
   AND NOT (
     col < ? AND other > 0
   )

   -- ✓ pasa
   AND col IS NOT NULL  -- preservar semantica three-valued si col es nullable
   AND (col >= ? OR other = 0)
   ```

3. **`sqlc.arg('name')` syntax**. Funciona inestablemente en SQLite;
   en algunos contextos sqlc lo deja como literal text en la SQL
   string. Usa `?` posicional simple. Si la auto-naming de sqlc no te
   gusta, renombra en la repo layer (alias el campo del struct
   generado al hacer Params{...}).

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

### React 19 + Compiler — patrones permitidos para no usar `useEffect`

El proyecto enforza `react-hooks/set-state-in-effect` y
`react-compiler/preserve-manual-memoization`. Bajo React 19 + Compiler
estas reglas no son nits — `setState` dentro de `useEffect` produce
re-renders en cascada reales (paint con valor stale, luego paint de
la corrección). Patrones permitidos:

- **Derivación pura** → `useMemo([deps])`. Si el valor es función
  determinística de inputs, no es estado.
- **Mirror de un store externo** (matchMedia, location, localStorage,
  websocket, etc.) → `useSyncExternalStore`. Eliminado el efecto
  por completo, sin race entre initial-state y subscribe.
- **Reset de estado en cambio de prop** → `[prev, setPrev] = useState
  (prop)` y comparar en render. Patrón canónico de React docs
  ("Adjusting state on prop change"). Lint-clean.

```tsx
// ✅ Reset durante render
const [visibleCount, setVisibleCount] = useState(BATCH_SIZE);
const [prevItems, setPrevItems] = useState(items);
if (prevItems !== items) {
  setPrevItems(items);
  setVisibleCount(BATCH_SIZE);
}

// ❌ Reset en efecto (cascading render bajo React 19)
const [visibleCount, setVisibleCount] = useState(BATCH_SIZE);
useEffect(() => {
  setVisibleCount(BATCH_SIZE);
}, [items]);
```

### React Compiler — no manual `useCallback` / `useMemo`

Si el lint dice "Compilation Skipped: Existing memoization could not
be preserved", **borra** el `useCallback` o `useMemo`. El compiler
memoiza el componente entero gratis; un manual mal-depped es siempre
peor que ninguno.

Excepción única: el callback se pasa a un hook externo que **depende**
de la identidad estable (ej. `useEffect` con el callback en deps,
o un hook de terceros que documenta requerirlo). En el resto, fuera.

### `useEffect` con cleanup que lee `ref.current`

El linter `react-hooks/exhaustive-deps` flagea leer `ref.current` en
el cleanup porque la ref puede haber cambiado. Capturar al inicio
del efecto:

```tsx
// ✅
useEffect(() => {
  const node = videoRef.current;
  return () => {
    if (node && node.currentTime > 0) saveProgress(node);
  };
}, []);

// ❌ — videoRef.current puede ser null o un nodo distinto al cleanup
useEffect(() => {
  return () => {
    const node = videoRef.current;
    if (node) saveProgress(node);
  };
}, []);
```

### Component-only files (Fast Refresh)

`react-refresh/only-export-components` rompe HMR si un fichero
exporta a la vez un componente y un helper / constante / hook /
tipo (excepto types via `export type`, que sí se permiten).

Reglas:
- Helpers que solo usa el componente → no se exportan.
- Helpers que usan varios componentes → fichero propio (`*.helpers.ts`).
- Si una constante de UI se usa con un componente, mejor fichero aparte
  (ej. `categoryOrder.ts` separado de `CategoryChips.tsx`).

---

## Linter gate (`gosec`)

CI corre `golangci-lint` con `gosec` activado, pero filtrado a
**HIGH severity + HIGH confidence**. El nivel medium se audita
manualmente cada cuatrimestre, no en CI, porque es ruido neto:

- 27 findings medium en el snapshot 2026-04-28; 21 son falsos
  positivos por sanitización vía `pathmap` (G304/G703) o allowlist
  estática de columnas SQL en repos como `item_repository.go`
  (G201). gosec no rastrea valores así.
- Los 6 que quedan (G118 goroutines detached, G120 form parsing en
  upload de imagen, G124 cookie attrs, G705 XSS taint, G710 open
  redirect en stream/info) son decisiones contextuales del self-
  hosted single-tenant. Cada uno merece un `#nosec` razonado, no un
  fix automático.

`.golangci.yml` excluye explícitamente G104, G304 y G703 con
comentario sobre el porqué. Cualquier nueva exclusión va con
justificación inline, no silenciosa.

---

## Iframe externos: lazy gates antes del load

YouTube / Vimeo iframes cuestan ~700 KB de player JS + ~6 cross-
origin connections cada vez que se montan. Un hero trailer no debe
cargar el iframe **on mount** — debe cargarlo cuando los gates de
a11y, network y visibilidad lo permiten.

Patrón aplicado en `web/src/components/media/SeriesHero.tsx`:

1. **`shouldSkipTrailer()` al mount** → si `prefers-reduced-motion`,
   `connection.saveData`, `connection.effectiveType ∈ {slow-2g, 2g}`
   o `sessionStorage["hubplay:trailers-dismissed"]`, render null. No
   hay iframe.
2. **`IntersectionObserver` con threshold 0.25** → solo se inicia el
   timer de carga cuando el hero entra en viewport. Disconnect al
   primer hit (no se reinicia).
3. **`<link rel="preconnect">`** dropeado al document.head durante
   la espera (2.5s), removido en cleanup. Reduce TTFB ~150 ms.
4. **`<iframe>` montado solo después del flip** (no parked en
   `about:blank`) → ahorra layout cost y wiring del iframe parent-
   frame messaging.

Replicar este patrón si entra otro embed externo (X.com, Twitch,
SoundCloud, etc.). La regla es "cero peso de red por embed cuando el
usuario no quiere o no ha visto el embed".

---

## Async cleanup en Promises

`.finally(cleanup)` crea una promise nueva que **mirror la rejection**
de la original. Si nadie observa esa promise, vitest (y Node) la
flagean como unhandled rejection.

Ocurre cuando se usa `.finally()` para side-effects de cleanup sobre
una promise que se devuelve a otro caller:

```ts
// ❌ — la rejection del .finally(...) no la captura nadie
async refresh() {
  const inflight = this.doRefresh();
  this.refreshInFlight = inflight;
  inflight.finally(() => { this.refreshInFlight = null; });
  return inflight;  // caller captura inflight, pero no el .finally
}
```

```ts
// ✅ — handlers explícitos no propagan rejection a la chain
async refresh() {
  const inflight = this.doRefresh();
  this.refreshInFlight = inflight;
  const clear = () => { this.refreshInFlight = null; };
  inflight.then(clear, clear);
  return inflight;
}
```

Aplica a cualquier dedup pattern, mutex de promise, slot in-flight
en un cliente HTTP, etc.

---

## Cuándo trocear un fichero gordo (y cuándo NO)

Aprendido durante el refactor SRP del 2026-04-28 (late PM). Antes de
hacer split de un fichero grande, revisar la siguiente checklist —
cumplir 1 punto NO es suficiente, hace falta combinar varios:

**Trocear cuando**:
- El fichero mezcla responsabilidades conceptualmente independientes
  (auth + media + IPTV en `hooks.ts`; form state + modal lifecycle +
  per-row card + iptv-org catalogues en `LibrariesAdmin.tsx`).
- Hay copy-paste activo entre dos funciones (Select + Upload en
  `image.go` con los mismos 9 pasos).
- Una "regla de negocio" vive en una capa equivocada (`dedupeSeasons`
  era lógica de items domain dentro de un handler).
- Decoraciones inline (icons SVG, slug catalogues hardcoded) ocupan
  >100 líneas y no son la responsabilidad del fichero.

**NO trocear cuando**:
- El fichero es largo pero cohesivo y cada función justifica su
  tamaño por dominio (`scanner.go` 1126 líneas: enrichment de movies/
  series/episodes/seasons + filesystem walk + image fetch — separar
  empeora navegación porque las funciones se llaman entre sí).
- Sería un split por "tamaño" sin criterio (`api/client.ts` 953
  líneas: una sola responsabilidad clara, método/línea limpio).
- Habría que inventar abstracciones nuevas (interfaces, layers, DI
  containers) sólo para justificar el split. El criterio es
  relocalizar lo que ya existe, no añadir indirecciones.

### Reglas técnicas del split

1. **Mantener back-compat de imports**. Re-exportar desde el fichero
   original como barrel; los call sites NO deben editarse en el
   mismo PR.
2. **Tests verdes tras cada commit**, no al final. Permite revert
   granular.
3. **Cero abstracciones nuevas**. Si después del split necesitas
   inyectar algo o crear una interfaz, el split estaba mal pensado;
   reabsorbe y vuelve a planear.
4. **Carpetas en camelCase** que matchee el page padre (`itemDetail/`
   bajo `pages/ItemDetail.tsx`, `librariesAdmin/` bajo
   `pages/admin/LibrariesAdmin.tsx`). Hace obvio quién es el dueño
   sin abrir los ficheros.
5. **Cada fichero <300 líneas** como heurística. Encima de eso, el
   IDE empieza a costarse navegar; debajo, mover cosas a más
   ficheros añade fricción sin ganancia.

---

## Disciplina Torvalds-simple — empírico > especulativo

> "Lo simple es más fácil de mantener. Lo complejo introduce más
> errores. Si algo no se puede explicar de forma sencilla,
> probablemente está mal diseñado."

Patrón consolidado durante la sesión 2026-04-29 noche y aplicado en
los 4 commits de esa sesión. La regla operativa:

- **Bug latente verificado** (peer-review encuentra una race real,
  un buffer que puede deadlockear, una migración que falta) → fix
  inmediato.
- **Edge case especulativo** (cache miss por title-case mismatch,
  pre-spawn fail por permisos) → **NO se añade código defensivo**
  hasta que se observe en producción. La estructura (UNIQUE index,
  threshold del breaker) atrapa el síntoma; el operador investiga
  la causa raíz cuando aparece.

### Heurísticas concretas

1. **Si el usuario verifica empíricamente que un código no produce
   un síntoma, no añadas recovery code para ese síntoma.** El
   recovery disimula bugs futuros; la ausencia de recovery los
   hace fallar ruidosamente, que es lo que queremos.

2. **Estructura > recuperación**. Una UNIQUE constraint parcial
   cierra una invariante en el schema; un branch de re-fetch en
   código tiene que mantenerse, testarse, y puede regresionar. La
   constraint es ~5 líneas de SQL idempotente.

3. **Whitelist hardcoded > registro dinámico**. El admin endpoint
   `/admin/system/settings` admite 3 keys hoy, validadas en un
   `switch` explícito. Una key nueva: const + caso + i18n. No hay
   registro dinámico de "settings disponibles" porque no se
   necesita y abrir el surface es la única forma de que un typo
   en la UI escriba algo que no debería.

4. **YAGNI en abstracciones**. La sesión 2026-04-29 tuvo dos
   momentos donde estuvimos a punto de inventar un `Runtime`
   struct (overlay de `Config + SettingsReader`) y un
   `ErrItemConflict` tipado. Ambos rechazados por el mismo motivo:
   nadie los necesitaba todavía. Los handlers consumen
   `SettingsReader` directamente; las constraint failures se
   propagan como errores genéricos hasta que aparezca un caller
   que necesite ramificar sobre ellos.

### Cuándo SÍ añadir defensa

- **Cuando observamos el síntoma**. La UNIQUE constraint nueva existe
  porque el operador reportó dups; el reaper de zombie sessions
  existe porque vimos sesiones colgadas en un upstream que evade
  `-rw_timeout`.
- **Cuando el coste de fallar es alto**. El stderr drain barrier
  (`stderrRing.wait()`) protege la decisión de auto-promoción a
  reencode; sin él, ese feature no funciona en el peor caso. Esa
  defensa es necesaria.
- **Cuando el coste de la defensa es nulo o bajo**. Subir el buffer
  del scanner stderr de 4 KiB a 64 KiB es una constante; añadir un
  `io.Copy(io.Discard, rd)` post-scanner es 2 líneas. La defensa
  paga su peso.

---

## Whitelist hardcoded para endpoints que escriben configuración

Patrón aplicado en `internal/api/handlers/settings.go` (commit
`b1a84da`). Cuando un endpoint admin escribe un key-value pair que
afecta runtime config, **la lista de keys aceptadas vive en código,
no en una tabla**:

```go
const (
    settingBaseURL          = "server.base_url"
    settingHWAccelEnabled   = "hardware_acceleration.enabled"
    settingHWAccelPreferred = "hardware_acceleration.preferred"
)

func isAllowedSettingKey(key string) bool {
    switch key {
    case settingBaseURL, settingHWAccelEnabled, settingHWAccelPreferred:
        return true
    default:
        return false
    }
}
```

Razones:

- **Imposible de desbordar por dato**. El gate vive en el binario.
  Una request con un key inventado se rechaza con 400 antes de
  tocar la DB, incluso con admin auth válido.
- **Validación per-key obvia**. Cada key tiene su forma (URL
  absoluta, bool, enum). Un `switch` en `validateSettingValue`
  pone la lógica visible; un sistema de "registro de validators"
  esconde las decisiones reales.
- **Una key nueva = una línea**. Const + caso en validator + par
  i18n. Sin scaffolding ni "diseño extensible".

**No usar este patrón** para datos del dominio (channels, items,
preferences de usuario) — ahí el conjunto es abierto y la
validación va en el repo / service.

---

## Drenaje de pipes externos (ffmpeg stderr) con barrera de sincronización

Patrón aplicado en `internal/iptv/transmux_stderr.go` (commit
`a21204c`). Cuando un proceso externo escribe a un pipe que tu
goroutine consumer debe drenar:

1. **El consumer cierra un canal `done` cuando termina** (defer
   `close(r.done)` al inicio de `consume(rd io.Reader)`).
2. **El watcher del proceso espera ese `done` antes de leer el
   resultado del consumer**:
   ```go
   err := s.cmd.Wait()
   s.stderrTail.wait()  // ← barrera obligatoria
   tail := s.stderrTail.String()
   ```
3. **El consumer tolera una línea > buffer max** drenando lo que
   queda con `io.Copy(io.Discard, rd)` después de salir el
   scanner. Sin esto, una línea anómala wedge-a el writer remoto
   (kernel pipe buffer fill → bloqueo en `write`).

**Por qué no basta con `cmd.Wait()`**: la doc del stdlib es explícita
— `Wait` cierra el pipe cuando el proceso muere, pero NO espera al
consumer goroutine que lee de ese pipe. Sin la barrera, el `String()`
del ring buffer puede ejecutarse antes de que el consumer haya
flushado los últimos bytes del pipe del kernel. Resultado: la línea
fatal que ffmpeg emite justo antes de exit se pierde silenciosamente.

Verificado en `TestStderrRing_WaitBlocksUntilConsumeReturns` y
`TestStderrRing_DrainsAfterOverlongLine`. Replicar este patrón si
entra otro consumer de pipe externo (subtítulos OpenSubtitles,
trickplay generator, lo que sea).

---

## WorkDir versionado por spawn para procesos efímeros

Patrón aplicado en `internal/iptv/transmux.go startLocked` (commit
`a21204c`). Cuando un proceso externo escribe a un directorio y la
sesión puede recrearse para el mismo "slot lógico" antes de que la
limpieza anterior termine:

```go
startedAt := time.Now()
workDir := filepath.Join(m.cfg.CacheDir, channelID, fmt.Sprintf("%d", startedAt.UnixNano()))
```

En vez de:

```go
// ✗ vulnerable a race entre evict.RemoveAll y startLocked.MkdirAll
workDir := filepath.Join(m.cfg.CacheDir, channelID)
```

Razones:

- **evict** suelta el lock antes de `os.RemoveAll(WorkDir)` para no
  bloquear otras operaciones. Con dir compartida, otro `GetOrStart`
  para el mismo canal puede entrar entre el delete-from-map y el
  RemoveAll, crear su propio workdir, y luego el RemoveAll del
  primero se carga la dir nueva.
- Versionar por `startNanos` aísla el árbol de cada spawn.
  evict / terminate sólo borran su subdir; la limpieza del padre
  `<channelID>/` se hace best-effort con `os.Remove` (ENOTEMPTY se
  ignora — si hay otro spawn activo, el padre sobrevive).

Test de regresión: `TestTransmuxManager_PerSpawnVersionedWorkDir`.
Aplica a cualquier patrón "1 channel = 1 sesión efímera con files
en disco que se borran al terminar". No aplica a directorios
"propiedad" del proceso largo (cache de imágenes, etc.).

---

## Configuración runtime: `app_settings` overlay sobre YAML

Decisión arquitectural en ADR-010 (sesión 2026-04-29). Patrón para
añadir un cuarto setting runtime-editable al panel admin:

1. **Const + entrada en whitelist** en
   `internal/api/handlers/settings.go`:
   ```go
   const settingNew = "category.new_key"
   func isAllowedSettingKey(key string) bool {
       switch key {
       // ...
       case settingNew:
           return true
       }
   }
   ```

2. **Caso en `validateSettingValue`** con la forma esperada (URL,
   bool, enum, número con rango). Devolver el valor normalizado
   listo para persistir.

3. **Entrada en `describeAll`** con default desde `Config`, hint,
   `restart_needed` (true si el setting se lee al boot, false si
   se lee runtime), y `allowed_values` opcional para enums.

4. **Lectura runtime** en el handler que consume:
   ```go
   value := h.settings.GetOr(ctx, settingNew, h.cfg.Default)
   ```
   o, si es boot-time, en `cmd/hubplay/main.go` antes del
   `New*Manager` que captura el valor.

5. **i18n**: `admin.system.settings.<slug>.label` + `.hint` en
   `en.json` y `es.json`. El frontend renderiza solo — no hay que
   tocar `SystemSettingsSection.tsx` salvo el mapping del slug en
   `settingI18nKey()`.

**Sin tocar**: el `SettingsRepository` (ya soporta cualquier key),
el frontend hook (consume `allowed_values` del backend), ni el
schema (es key-value).

**Cuándo NO añadir aquí**: bootstrap secrets, paths críticos
(database.path, cache_dir), bind/port. Esos cambian-requieren-restart
de forma destructiva (file handles, listeners), no preferencias.

---

## Page-canvas pattern: `<div fixed inset-0 -z-10>` para fondos por-ruta

Si una ruta quiere pintar un fondo viewport-completo distinto del
`bg-base` global (gradient, aurora, blur, etc.), **no** lo pinta
en su wrapper React — lo monta como una capa hermana fixed:

```tsx
return (
  <div className="flex flex-col">
    {bg && (
      <div
        aria-hidden="true"
        className="fixed inset-0 -z-10"
        style={{ background: bg }}
      />
    )}
    {/* ...page content... */}
  </div>
);
```

**Por qué fixed-inset-0 y no `backgroundColor` en el wrapper**:
el wrapper sólo paint-alcanza dentro de los gutters de AppLayout.
Esto crea un seam visible contra el `bg-base` que pinta los lados
de la página y se lee como "container desplazado" — el usuario lo
flagged dos veces antes de ver este patrón. La capa fixed escapa
del flow y cubre viewport edge-to-edge, sin importar qué paddings
tenga el layout.

**El `-z-10` es real**: la capa va detrás de TODO contenido normal
(que está en z-auto / z-0). Pero requiere que **NINGÚN ancestro**
pinte un background opaco encima — concretamente:

- `AppLayout.tsx` ya **NO** lleva `bg-bg-base` en su wrapper. El
  body lo carga global desde `styles/globals.css`. Si se reañade,
  cualquier capa fixed `-z-10` se vuelve invisible. Documentado
  en el comment del wrapper de AppLayout.
- Sidebar y TopBar pintan sus propios `bg-bg-base/80` con `backdrop-blur`
  — eso es OK porque son áreas explícitas, no un wash global.

**Para pintar varios colores no-uniformes** (caso aurora): apilar
`radial-gradient` en el `backgroundImage` con `color-mix(in srgb,
${rgb} N%, transparent)` para fade-out suave. Cada blob vale
~30-60% mix; la suma reads como ambiente, no como "círculos de
color". Ver `web/src/pages/ItemDetail.tsx::auroraBackground`.

**Cuando NO usar este patrón**:
- Backgrounds que dependen del contenido (bg que sigue al scroll
  del item) — usa `<section>` normal con su propio bg.
- Animated bg con coste de paint constante — el patrón fixed
  pinta una vez y se queda; animar requiere o `transform` en el
  layer (cheap) o repintar el bg cada frame (caro).
- Rutas dentro de overlays/modales — el portal no tiene un
  contenedor cuyo `-z-10` tenga sentido geométrico.

---

## Paleta de colores precomputada (vibrant + muted) en imágenes

Cualquier UI que quiera "que se vea el color del poster" en runtime
**no** debe correr `node-vibrant` ni Color Thief en el frontend
como path principal — el backend ya extrajo dos swatches al
ingestar la imagen y los entrega en el wire.

**Backend** — `internal/imaging/colors.go::ExtractDominantColors`,
llamado desde `IngestRemoteImage`:

1. Decodifica con std-lib decoders (PNG/JPEG/GIF que `image.Decode`
   ya tiene registrados; webp y otros devuelven `("","")` y el
   frontend cae al runtime fallback).
2. Sample grid `step = max_dim / 32` → ~1024 lecturas, coste O(1)
   sin importar tamaño.
3. Bucket cubo RGB 16×16×16 (4096 bins). Cada bin acumula sum + count.
4. Score por bin sobre HSL:
   - `vibrant = saturation × count` con `L ∈ [0.20, 0.80]`
   - `muted = (1 − saturation/2) × count` con `L ≤ 0.40`
5. Persiste como strings `rgb(R, G, B)` en
   `images.dominant_color` + `images.dominant_color_muted`.

**Wire**:
- `db.PrimaryImageRef` carga ambos campos.
- `/items/{id}` los expone como `backdrop_colors: { vibrant, muted }`.
- `GetPrimaryURLs` (batch) los devuelve por item para placeholders
  de listing cards.

**Frontend**:
- Path principal: `item.backdrop_colors.vibrant/muted` directos del
  wire — sin decode, sin fetch extra, paintable en first render.
- Fallback runtime: `useVibrantColors(url)` corre `node-vibrant`
  via dynamic import (lazy chunk) sólo cuando `backdrop_colors` es
  null/empty (items pre-extracción shipping). Hook es no-op cuando
  `url === null` para que páginas con paleta backend no fetcheen
  nada.

**Cuándo NO usar el fallback runtime**:
- Capas viewport-canvas como la aurora: pagar un decode JS por
  paint es absurdo. Si el item no tiene paleta backend, la aurora
  simplemente no se monta y la página queda con el `bg-base`
  global. Eso es la behaviour correcta — la página queda como
  cualquier otra ruta.
- Cards en grids grandes: corres N decodes JS para 30 cards. Mata.
  Usa `attachPosterPlaceholder` que ya consume el path principal
  del wire.

**Si añades una variante nueva** (ej. `dark_vibrant`): seguir el
mismo patrón en `colors.go` (un tercer ganador con sus thresholds),
añadir columna en `images`, exponer en `PrimaryImageRef`, ampliar
el wire shape `backdrop_colors`. **Sin** ampliar el fallback
runtime salvo que sea visualmente esencial (cada nuevo swatch
runtime es CPU + memoria por item).

## Frontend type-checking: `pnpm run build` antes de pushear, no sólo `tsc --noEmit`

CI Dockerfile corre `tsc -b && vite build` (incremental project
build) en el stage frontend; eso es **más estricto** que el
`tsc --noEmit` que muchos refactors lanzan local. Ejemplo concreto
(PR #121, mergeado con CI roto):

`useDetailMenu.tsx` accedía a `item.media_streams` con `item`
tipado como `MediaItem`. `media_streams` vive en `ItemDetail`
(que extiende `MediaItem`). `tsc --noEmit` lo dejó pasar localmente
— probablemente por widening implícito en un union — pero el
`tsc -b` del CI escupió `TS2339: Property 'media_streams' does
not exist on type 'MediaItem'` y el build de Docker `hwaccel`
falló entero.

**Regla**: cualquier refactor que toque tipos plumbed entre
páginas/hooks/components, antes del push:

```bash
cd web && pnpm run build      # replica el step CI exactamente
```

NO sólo `pnpm exec tsc --noEmit`. Si `pnpm run build` pasa,
el Docker frontend stage también pasa (ambos ejecutan la misma
secuencia `tsc -b && vite build`).

**Cuándo basta con `tsc --noEmit`**: cambios puramente de runtime
sin tocar shape de tipos exportados (props nuevos en componentes
internos, lógica condicional, refactors que mueven código sin
cambiar signaturas). El proyecto build incremental sólo
re-typechecks lo afectado, así que estos casos son baratos
incluso de re-correr; el riesgo de divergencia mode local↔CI
está en los que cruzan boundaries entre fichero-tipo y
fichero-consumidor.

## `go get` puede subir el `go` directive — pin manual antes de pushear

Cuando `go get foo/bar` añade una dep cuyo propio `go.mod`
declara una toolchain más alta que la nuestra, **el toolchain Go
local sube nuestro `go` directive al techo del de la dep**. Si la
imagen CI corre una versión por debajo, `go mod download` revienta
con `go: go.mod requires go >= X.Y.Z`.

Ejemplo concreto (esta misma sesión, dos veces):

1. `go get golang.org/x/image/webp` → añade `golang.org/x/image
   v0.39.0`, que requiere `go 1.25`. Mi toolchain local (1.25.0)
   bumpeó automáticamente el `go` directive del proyecto.
2. CI usa Go 1.24.13 → fail al `go mod download`.

**Regla**: tras cualquier `go get`, **inspeccionar `go.mod`** y
si el directive subió, decidir explícito:

   a) Bajar la dep a una versión compatible con CLAUDE.md's Go
      target (lookup las release notes de la dep para el último
      tag pre-bump):
      ```bash
      go get foo/bar@vX.Y.Z          # versión compatible
      ```
   b) O actualizar Dockerfile + GitHub Actions a la nueva
      toolchain, **y** subir CLAUDE.md.

Y **siempre**:
```bash
go mod tidy -go=1.24.7   # pin explícito al directive del proyecto
```

`-go=` evita que un futuro `go mod tidy` re-eleve silenciosamente.

**Verificación local fiable**: el comando que CI ejecuta es
`go mod download` en una imagen fresca con Go 1.24. Una forma
rápida sin Docker es:
```bash
GOTOOLCHAIN=go1.24.7 go mod download && go build ./...
```
Si pasa con `GOTOOLCHAIN` forzado, CI también pasará.

## Access predicate (post-migración 040)

Cualquier query que filtre ítems / canales / bibliotecas por el usuario logueado tiene que usar el predicate strict + profile-aware. La forma canónica es:

```sql
EXISTS (
    SELECT 1 FROM library_access la
    JOIN users u ON u.id = ?
    WHERE la.library_id = <columna_de_la_query>
      AND la.user_id = COALESCE(u.parent_user_id, u.id)
)
```

Reglas:
- Un solo `?` en el predicate (el user_id del logueado). El COALESCE resuelve top-level vs profile sin parámetros extra.
- Sin `OR NOT EXISTS` (fallback público). El modelo strict post-040 dice: si no hay grant, no se ve. Admin override vive en el handler, no en la query.
- **`GrantAccess` / `RevokeAccess` esperan el top-level user id**. Si te llega un profile_id (por ejemplo del JWT del logueado), resuélvelo a parent ANTES de llamar:
  ```go
  effective := userID
  if parentID, _ := userRepo.ParentOf(ctx, userID); parentID != "" {
      effective = parentID
  }
  libRepo.GrantAccess(ctx, effective, libraryID)
  ```
  Pasar un profile_id NO falla — crea un row huérfano que el predicate ignora. Doc-comment del método lo avisa.
- Tests que seedean rails de home / catálogo / iptv DEBEN incluir `libRepo.GrantAccess(ctx, user.ID, library.ID)` o las queries devolverán `[]`. Patrón ya aplicado en `setupHomeTrendingTest` y los Recommended.

Surfaces que aplican el predicate hoy (auditadas 2026-05-11):
- `internal/db/home_repository.go` — 4 EXISTS (Trending, Recommended ×2, IPTV channel sidebar)
- `internal/db/library_repository.go` — `UserHasAccess`, `ListForUser`
- `internal/api/handlers/iptv_access.go` — usa `LibraryAccessService.UserHasAccess` (heredado)
- `internal/api/handlers/system.go` — admin endpoints saltan el guard intencionadamente

## sqlc raw-SQL holdouts (actualizado 2026-05-11)

Queries que viven como raw SQL en el repo porque sqlc 1.31.1 las trunca o las parsea mal. Cada una tiene comentario cruzado con el "por qué" y referencia al ADR-013. Lista canónica:

1. `UserRepository.ListProfilesForOwner` — UTF-8 multi-byte trip.
2. `AuthRepository` refresh-token UPDATE con previous_refresh_token_hash — 4+ placeholders trip.
3. `FederationRepository.SearchSharedItems` — FTS5 MATCH no parsea (no es bug, es feature).
4. `FederationRepository.UpsertCachedItems` — INSERT con 11 placeholders trip (2026-05-11).
5. `FederationRepository.ListCachedItems` — SELECT con colour columns + ORDER BY COLLATE NOCASE trip (2026-05-11).
6. `FederationRepository.attachPrimaryImageColors` helper — batch SELECT que no encaja en sqlc por la aridad variable del IN-clause (2026-05-11).
7. `LibraryRepository.UserHasAccess` — JOIN + COALESCE trip.
8. `LibraryRepository.ListForUser` — JOIN + COALESCE + ORDER BY trip (2026-05-11).

Cuando upstream publique sqlc 1.32+ con el parser fix, abrir tarea "migrate raw-SQL holdouts back to sqlc" — son ~15 min cada una.

## Frontend — Patrones React Doctor (añadidos 2026-05-20)

Tras la integración de [React Doctor](https://github.com/millionco/react-doctor) en CI (PR #358) y las dos PRs de quick wins (#359 + #360), el repo adopta los siguientes patrones obligatorios para que el score se mantenga ≥71 y no introduzcamos regresiones:

### Iteración + ordenación

- `arr.toSorted((a,b) => …)` en lugar de `[...arr].sort((a,b) => …)`. ES2023, sin spread allocation.
- `.flatMap(it => cond ? [it] : [])` para combinar filter+map en una sola pasada cuando el resultado va al JSX. Listas con cientos de elementos (canales IPTV, items de biblioteca) se benefician notablemente.
- `.reduce()` o `for...of` cuando el flatMap empeora la legibilidad.

### Tailwind

- `size-N` siempre, NUNCA `w-N h-N` con el mismo N.
- `p-N` siempre, NUNCA `px-N py-N` con el mismo N.
- `font-semibold` (600) en headings `<hN>`, NUNCA `font-bold` (700) o `font-extrabold` (800). El compilador del proyecto enforza esto vía React Doctor.

### Keys de React

- NUNCA `key={index}` excepto en arrays REALMENTE estáticos (skeleton placeholders donde N es fijo y el orden nunca cambia). En ese caso usar prefijo descriptivo: `key={`peer-recent-skeleton-${i}`}`.
- Para listas dinámicas: ID del backend si existe; si el item no tiene ID estable, añadir `localId: crypto.randomUUID()` al type local (ver `LibrariesStep.tsx`).
- Composite keys (`${item.field1}-${item.field2}`) cuando garantiza unicidad sin índice.

### Framer-motion

- `LazyMotion strict` envolviendo el Router en `App.tsx` con `domAnimation` (subset suficiente para todas nuestras animaciones).
- `import { m } from "framer-motion"` y `<m.div>`, NUNCA `import { motion }` ni `<motion.div>`.
- Ahorra ~30 KB del bundle base; el motor completo sólo se carga si algún consumer pide features explícitamente.

### Fechas

- `formatDateTime`, `formatDate`, `formatTime`, `epochOf` desde [`src/utils/dateFormat.ts`](../../web/src/utils/dateFormat.ts).
- NUNCA `new Date(iso).toLocale*()` directo en JSX o en callbacks alcanzables desde JSX.
- Los helpers toleran inputs vacíos/inválidos (devuelven `""` en lugar de "Invalid Date").

### Tipografía en JSX

- Em-dash (`—`) NUNCA en JSX text: lee como output AI.
  - Para "sin valor": en-dash (`–`).
  - Para separador inline: bullet (`·`).

### Accesibilidad

- `autoFocus` prohibido. Si la UX lo justifica (overlay que aparece y debe confirmar con Enter):
  - Patrón: `useEffect + ref.current.focus()` (no atributo).
  - Si DE VERDAD necesitas el atributo: `// eslint-disable-next-line jsx-a11y/no-autofocus` con justificación inline + test que cubre el orden de foco.

### React Compiler

- Plugin `eslint-plugin-react-compiler` activo con regla `react-compiler/react-compiler: 'error'`. Las regresiones de compatibilidad rompen el CI.
- `react-compiler-healthcheck` hard gate en CI: el job FALLA si la tasa de compatibilidad baja de 100%.
- Patrón "Adjusting state when a prop changes" (render-time guarded setState con `useState` de tracking) en lugar de `useState + useEffect` que sincroniza state derivado.

### Quality gates en CI

- `typecheck` (`tsc -b`) — hard gate.
- `react-compiler-healthcheck` — hard gate, 100% requerido.
- `react-doctor` — visibility-only, comenta inline en cada PR con regresiones/mejoras del score.
- `knip` — info-only, `--no-exit-code` (cuando lleguemos a 0 unused, elevar a hard).

### Squash merge audit (regla nueva 2026-05-20)

Tras descubrir que el squash merge de PR #360 descartó silenciosamente 54 líneas de un commit (migración LazyMotion sobre 7 archivos) — se mergeó la activación de `LazyMotion strict` en `App.tsx` pero NO los cambios `motion` → `m` en los call sites, lo que habría roto runtime al primer render con animación:

- **Tras mergear cualquier PR que aplica un script masivo** (sed-like changes, code mods, migraciones cross-archivo), `git diff origin/main~1 origin/main -- <archivo_clave>` para verificar que los cambios reales aterrizaron. El visor de "files changed" del PR de GitHub puede esconder conflict resolutions silenciosas durante el squash.
- **Si una regla del lint que se eliminó vuelve a aparecer** en un PR posterior, primero auditar main (`git show origin/main:<file>`) antes de asumir que el autor del nuevo PR la introdujo. La regresión puede venir del merge anterior.
- **Para scripts masivos próximamente**: idealmente, hacerlos en commits separados pequeños o usar `git merge --no-ff` en lugar de squash si la PR contiene scripts de mass-edit, para que el historial preserve la atomicidad del cambio.

### Patrones añadidos en el push final a 75 ("Great")

Una segunda tanda de patrones que vinieron al cruzar el umbral del 75 con PR #367:

- **`forwardRef` PROHIBIDO en componentes nuevos**. React 19 acepta `ref` como prop normal en componentes función. Declarar `ref?: Ref<HTML*Element>` en la interfaz de props y desestructurar en el componente. Migrados: Button, Input.

- **Patrón "latest value via ref"** sustituye a `useEffectEvent` (aún experimental):
  ```tsx
  const cbRef = useRef(cb);
  useEffect(() => { cbRef.current = cb; }, [cb]);
  useEffect(() => {
    const handle = () => cbRef.current();
    el.addEventListener("x", handle);
    return () => el.removeEventListener("x", handle);
  }, [/* SIN cb */]);
  ```
  Aplica cuando un listener depende de un prop callable pero el effect NO debería re-suscribirse al cambiar la identidad del callable. Aplicado en BottomSheet, VideoPlayer, ImageManager.

- **NUNCA closures `const renderX = (...) => <jsx>` en cuerpo de render**. Extraer a un componente real (`<X />` con props explícitas). Mejora reconciliación y satisface `react-doctor/no-render-in-render`.

- **`Set` para lookups en bucles**: si un array se consulta con `.includes()` dentro de un loop, exponer también un `Set` del mismo dataset. Caso real: `ACCEPTED_EXTENSIONS_SET = new Set(ACCEPTED_EXTENSIONS)` en Uploads.tsx (array para el `accept=` del `<input>`, Set para validación O(1) en `validateFiles()`).

- **Animaciones**: `scale: 0` PROHIBIDO en entradas. Usar `{ scale: 0.95, opacity: 0 }` → `{ scale: 1, opacity: 1 }`. Los elementos deben "desinflar" naturalmente, no aparecer de la nada.

- **`<video>` containers y otros widgets de pantalla completa**: `role="application"` + `aria-label` para que lectores de pantalla los anuncien correctamente como widgets interactivos.

- **Backdrops de modal con `<button>` siempre que sea posible**. Si no (porque contiene otros botones internos), `<div role="dialog">` con `onClick` + `onKeyDown` (Escape) + body interno `role="presentation"` con `stopPropagation` en ambos.

### Conflicto documentado: `rerender-state-only-in-handlers`

React Doctor pide reemplazar `useState` que nunca se lee en JSX por `useRef`. Pero el patrón canónico para "Adjusting state when a prop changes" (recomendado por React docs + `react-hooks/set-state-in-effect`) usa `useState` tracking. Cambiar a `useRef` con `ref.current = value` durante render viola `react-hooks/refs`.

**Decisión**: ignorar la regla `rerender-state-only-in-handlers` para este patrón. **También se ignora `no-derived-useState` en los mismos sitios** porque la única alternativa sería volver al patrón useEffect+setState que `react-hooks/set-state-in-effect` prohíbe. La consistencia con react-hooks importa más que el score numérico de react-doctor. Casos vivos: `MediaGrid.tsx:43` (prevItems), `UserAvatar.tsx:64` (prevSrc), y `ExternalSubsModal.tsx:39` (langs — caso diferente: state es edición local, derivar reiniciaría la selección).

### knip hard gate (2026-05-21): 0 unused

Tras #375 + #377 el repo llegó a 0 unused files / deps / exports / types. CI ahora bloquea PRs que reintroduzcan dead code (job `knip` en `ci.yml`, sin `--no-exit-code`).

**Gotcha**: knip NO detecta el patrón `import("./types").Foo` en el tipo de retorno o anotación. Usar siempre imports normales al top del archivo:

```ts
// ❌ knip lo marca unused incluso si se usa
async getStudio(slug: string): Promise<import("./types").StudioDetail> { … }

// ✅ knip lo detecta correctamente
import type { StudioDetail } from "./types";
async getStudio(slug: string): Promise<StudioDetail> { … }
```

## Naming `Is{X}Safe` / `Is{X}Blocked` para validators (2026-05-21, audit F14-5-a)

Convención para helpers booleanos de validación. La elección entre **Safe** y **Blocked** marca **la dirección de la lista**, no la severidad de la falla:

- **`Is{X}Safe(...)` / `is{X}Safe(...)`** — el helper es **whitelist**: devuelve `true` cuando el valor está en el conjunto permitido. Falsable por construcción (lista positiva). Ejemplo: `isSafeUpstream(url)` valida que la URL outbound caiga en el set de schemes/hosts permitidos (no `data:`, no `file:`, no IP privada).

- **`{X}Blocked(...)` / `is{X}Blocked(...)`** — el helper es **blacklist**: devuelve `true` cuando el valor está en el conjunto denegado. La lista negativa siempre tiene riesgo de incompletitud (algo nuevo cae por defecto en "allowed"). Ejemplo: `BlockedIP(ip)` valida que la IP NO esté en rangos privados/loopback (defensa SSRF).

- **Exported (`Is...` / camelcase inicial mayúscula)** cuando lo consumen otros paquetes; **unexported (`is...`)** cuando es helper privado del paquete.

Casos sanos hoy:

| Helper | Paquete | Dirección |
|---|---|---|
| `isSafeMethod` | `api/csrf.go` | whitelist (`GET/HEAD/OPTIONS`) |
| `isSafeUpstream` | `iptv/proxy.go` | whitelist (schemes + hosts SSRF-safe) |
| `IsSafePathSegment` | `imaging/safety.go` | whitelist (sin `..`, sin `/`) |
| `BlockedIP` | `imaging/safety.go` | blacklist (rangos privados/loopback) |

`BlockedIP` rompe la familia `IsSafe...` pero el nombre **es correcto** — su semántica es blacklist, no whitelist. Renombrar a `IsIPBlocked` haría el call-site más verboso (`if IsIPBlocked(ip)` vs `if BlockedIP(ip)`); la convención privilegia el nombre que se lee mejor en el caller.

**Regla nueva al añadir un validator**: elegir según la dirección de la lista. Si dudas, prefiere whitelist y nómbralo `Is{X}Safe` — las whitelists son inherentemente más conservadoras.

**Convención**: types `*Props` / `*Variant` / `*Size` que sólo se usan dentro del archivo del componente NO se exportan. Mantenerlos como `interface FooProps {…}` sin `export`. El componente público importa fine — el type interno no.

## Estilo de comentarios (2026-05-21)

Tres reglas, en orden de importancia:

1. **Español, no inglés.** Convención del repo. Los nombres de identificadores se quedan en inglés (es Go); los comentarios y los doc-comments largos van en castellano.
2. **Cortos por defecto.** 1-3 líneas. Si necesitas un párrafo es porque hay un invariante no obvio — entonces sí, pero no narres history ni alternativas descartadas.
3. **Comenta el "por qué", no el "qué".** El código ya dice qué hace. Los comentarios son para el contexto que no se ve.

### Qué SÍ comentar

- Restricciones no obvias del entorno ("ffmpeg ignora -preset si el encoder no es libx264").
- Workarounds de un bug ajeno con link/issue.
- Decisiones contraintuitivas que parecen errores a primera vista.
- Invariantes de concurrencia/locking que el compilador no enforza.

### Qué NO comentar

- **El "qué" cuando el nombre lo dice.** `func cleanupOldSessions()` no necesita `// limpia las sesiones viejas`.
- **El "cómo" si el código es legible.** No narres el algoritmo línea a línea.
- **Historia del fichero.** "Antes esto vivía en X, lo movimos a Y porque..." va al commit message + memoria, no al código. El código se lee por su estado actual.
- **Alternativas descartadas.** Si en review se discutieron 3 opciones y se eligió una, el comentario no necesita las otras 2. Quedan en la PR.
- **Sinónimos.** `// Inicializa el cliente` arriba de `client := newClient()` es ruido.

### Antes / después (ejemplos reales del repo)

**Antes** — 8 líneas para explicar un map de sesión:

```go
// Burn-in subtitle codec from BurnSubtitle.Codec, only present when
// the player picked a PGS/DVDSUB/ASS sub for burn-in. The codec
// itself is what BuildFFmpegArgs needs to choose between filter
// strategies (overlay vs subtitles= filter), so the wrapper hangs
// onto the spec rather than re-resolving from MediaStream rows on
// every restart. nil means no burn-in selected — the player either
// has no sub or relies on a native HLS sub track.
BurnSubtitle *BurnSubtitleSpec
```

**Después** — 2 líneas:

```go
// Subtítulo burned (PGS/DVDSUB/ASS). nil = sin burn-in.
// RestartAt lo reusa para no re-resolver la spec en cada seek.
BurnSubtitle *BurnSubtitleSpec
```

El "por qué" (no re-resolver en cada restart) sobrevive. Lo demás eran apuntes para PR review que ya quedaron en el PR.

**Antes** — 15 líneas justificando un panic:

```go
// NewProberWorker wires a worker around the building blocks.
// Logger is required (a silent worker is a debugging nightmare).
// We panic on a nil dependency rather than returning an error
// because every production caller would have to error-check
// something that's always a programming bug, never a runtime
// condition. The other auth-side constructors (federation, library)
// follow the same pattern. Tests construct workers with concrete
// fakes so this never triggers there either.
//
// If you're seeing this panic in production, the issue is upstream
// in main.go's wiring — there's a nil being passed where there
// shouldn't be one. Trace back from the panic message.
func NewProberWorker(...) *ProberWorker {
    if prober == nil || libs == nil || ... {
        panic("iptv.NewProberWorker: nil dependency")
    }
    ...
}
```

**Después** — 1 línea:

```go
// Devuelve error si cualquier dep es nil. main.go decide qué hacer.
func NewProberWorker(...) (*ProberWorker, error) { ... }
```

(Y de paso pasó de `panic` a `error` por convención del repo — F14-4-a del audit.)

### Tamaño de doc-comment de función

- **Privada (lowercase)**: 0-1 líneas. Si no necesita comentario, no lo pongas.
- **Pública (uppercase)**: máx 3 líneas, salvo un invariante real que merezca explicación.
- **Constructor**: 1 línea + lista de defaults si los hay (estilo `TranscoderConfig`).
- **Función pura con contrato matemático** (`buildVideoFilterChain`, `DetectFromChapters`): puede ser más larga si describe el "shape" del input/output. Pero igual: 5 líneas, no 30.

### Regla operativa al editar código existente

Cuando toques un fichero, si pasas por un comentario obviamente sobredimensionado, **acórtalo en el mismo commit**. No fuerces una pasada "barrer todos los comentarios" — es BB del audit y se hace en sesión propia. Pero si ya tienes el fichero abierto, recortar 5 líneas no cuesta nada y mejora la siguiente lectura.

### Reglas para el assistant

Aplican también a comentarios generados por IA. Antes de escribir un doc-comment de más de 3 líneas, preguntarse:

1. ¿Esto lo dice el código por sí solo? → No comentar.
2. ¿Esto es contexto histórico de la PR? → Va al commit message, no al código.
3. ¿Estoy parafraseando el nombre de la función? → Borrar.
4. ¿Esto es realmente un invariante no obvio? → OK, comentar.
