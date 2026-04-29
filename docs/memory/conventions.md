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
