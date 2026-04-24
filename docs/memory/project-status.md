# Estado del proyecto

> Snapshot: **2026-04-24** (live-TV arc + simplify sweep + frontend coverage slice 1 + setup-wizard tests) · Rama: `claude/review-project-tasks-f5Rdg` · **tests: verde**

---

## 👉 HANDOFF PARA LA PRÓXIMA SESIÓN

> Track B (setup wizard tests) ✅ hecho en esta sesión: **+48 tests**,
> los 5 ficheros del wizard cubiertos (SetupWizard/Account/Libraries/
> Settings/Complete). Vitest 138 → 186, tsc + build verdes, lint sin
> regresiones (14 problemas pre-existentes idénticos antes/después).
>
> **Pendiente principal ahora mismo**: Track A — slice 2 de coverage
> LiveTV (`LiveTvTopBar`, `DiscoverView`, `HeroSpotlight`, `EPGGrid`).
> Sigue aplicando. El resto del handoff original se conserva abajo
> para referencia.
>
> Leer esto ANTES que el resto del documento. Hay dos tracks pedidos,
> ambos independientes y paralelizables. Elige el que prefieras y ataca.

**Estado al abrir sesión**: asume que PR `claude/frontend-livetv-coverage`
ya está mergeado a main (si no, mergéalo primero o trabaja sobre él).
La base ya trae: los hooks `useNowTick`/`useHeroSpotlight`, los
componentes split (`DiscoverView`, `FavoritesView`, `LiveTvTopBar`,
`OverlayHeader`, `NowPlayingCard`), y 138 tests verdes.

### TRACK A — Coverage slice 2 (componentes livetv restantes)

**Objetivo**: cubrir los 4 componentes que quedaron sin tests en el
slice 1. Mismo patrón exacto, debería ser ~1 día y +40-50 tests.

**Fixtures reutilizables**: copia directamente de los tests que ya
existen — todos usan el mismo shape de `channel(overrides)` factory
+ `program(id, startOffsetMin, durationMin, overrides)` + reloj fijo
`const NOW = new Date("2026-04-24T12:00:00Z").getTime()` con
`vi.useFakeTimers()` + `vi.setSystemTime(NOW)` en `beforeEach`.
Ejemplo canónico: `web/src/components/livetv/NowPlayingCard.test.tsx`.

Los tres gotchas ya aprendidos (en `conventions.md` sección "Frontend
— Vitest + Testing Library"): **fireEvent en vez de user-event**,
`container.querySelector("img")` para `<img alt="">`, y **no hace
falta i18n provider** si el componente usa `defaultValue`.

Componentes a cubrir y qué pinear en cada uno:

- **`LiveTvTopBar.tsx`** (146 loc):
  - Renderiza título + total channels + live-now count (aria labels).
  - 3 tabs con `role="tab"` + `aria-selected`, wiring de `onTab`.
  - Input de búsqueda dispara `onSearch` con el valor.
  - `<HeroSettings>` solo aparece en la tab `discover` (no en guide /
    favorites).
  - Mockear `HeroSettings` con `vi.mock("./HeroSettings")` para no
    arrastrar su lógica interna al test.

- **`DiscoverView.tsx`** (136 loc):
  - Con `category="all"`, los rails aparecen en el orden de
    `CHANNEL_CATEGORY_ORDER` **saltando** categorías vacías.
  - Con `category` específica, solo se renderiza ese rail.
  - Rail `Apagados` solo aparece si hay unhealthy **Y** la category es
    `"all"` (si hay unhealthy pero estamos filtrando por "sports", NO
    debe verse).
  - Empty state cuando `visibleRails.length === 0` (ninguna categoría
    con canales).
  - `onSeeAll` solo existe en modo `"all"` (cambiar a esa category en
    el click).
  - Mockear `HeroSpotlight` (ya tiene su propio test) para no mezclar.

- **`HeroSpotlight.tsx`** (~210 loc):
  - `items.length === 0` → renderiza null.
  - 1 item: no hay dots, no auto-rotación (el `useEffect` con timer
    respeta `items.length < 2`).
  - 2+ items: dots con `role="tab"`, `aria-selected` en el activo, y
    auto-rotación cada 12 s (anchor `ROTATE_MS`). Usa fake timers +
    `vi.advanceTimersByTime(12_000)` para verificar rotación.
  - Click en dot cambia `rawIdx` y el activo.
  - Clamp: si `items` se reduce de 3 a 1, el render sigue funcionando
    sin crash (re-render con `rawIdx >= items.length`).
  - `useNowTick(30_000)` integrado — mock del hook NO es necesario si
    usas fake timers, pero sí lo es si quieres forzar re-render
    explícito. Alternativa: `vi.advanceTimersByTime(30_000)` y
    `rerender()`.
  - `StreamPreview` tiene HLS.js que en jsdom peta; **mockear**
    `./StreamPreview` con `vi.mock` para evitar montarlo.

- **`EPGGrid.tsx`** (430 loc, el más complejo):
  - Header sticky con hour ruler (24 columnas).
  - Renderiza una fila por canal, dentro programas que **caen en la
    ventana** `windowStart..windowEnd` (descarta fuera-de-rango).
  - `now-line` visible solo cuando `nowLineOffset` está entre 0 y
    `TIMELINE_WIDTH`.
  - Botón "Ahora · HH:MM" hace smooth-scroll al offset de ahora −120.
  - Auto-scroll inicial solo una vez (`hasScrolledRef`). Re-ticks
    NO re-scrollean.
  - Click en una celda de programa llama `onSelect(channel)`.
  - `activeChannelId` aplica clase + `aria-pressed="true"` a esa fila.
  - Empty state cuando `channels.length === 0`.
  - Programa con `start_time` tras `windowEnd` o `end_time` antes de
    `windowStart` no se renderiza (devuelve null).

**Criterio de "hecho"**: `pnpm test` sube de 138 a ≥180, `pnpm tsc
--noEmit` verde, `pnpm build` verde, `pnpm lint` sin nuevos errors.

### TRACK B — Setup wizard tests (ruta crítica 1er arranque)

**Objetivo**: cubrir el flujo más importante del producto — la primera
ejecución. Hoy tiene **0 tests**. Si esto se rompe, nadie usa el
producto.

**Ficheros**:
```
web/src/pages/setup/SetupWizard.tsx    235 loc  — orquestador
web/src/pages/setup/AccountStep.tsx    197 loc  — crear admin
web/src/pages/setup/LibrariesStep.tsx  341 loc  — elegir bibliotecas
web/src/pages/setup/SettingsStep.tsx   280 loc  — TMDB key, hw accel
web/src/pages/setup/CompleteStep.tsx   257 loc  — finalizar
web/src/components/setup/FolderBrowser.tsx  195 loc
```

**Gotcha de entrada**: `FolderBrowser` está en la lista de
pre-existing lint debt (`react-hooks/set-state-in-effect`). Si tocas
ese fichero para testearlo, considera arreglar el lint a la vez — el
patrón de fix ya está documentado (ver `CountrySelector` en el commit
del lint burndown del ciclo anterior).

**Qué pinear, por step**:

- **`SetupWizard.tsx`** (orquestador — más fácil, empezar por aquí):
  - `StepIndicator` marca activo/completed según `currentStep`.
  - `initialStep` prop mapea a índice correcto (hay un `STEP_MAP` al
    principio; verifica que un valor desconocido cae a 0).
  - `goNext` avanza pero topa en `STEP_KEYS.length - 1` (4 steps,
    índice 3 max).
  - `goBack` retrocede pero topa en 0.
  - `handleAccountNext` → persiste `setupData.user` y avanza.
  - `handleLibrariesNext` → persiste `setupData.libraries` y avanza.
  - `handleSettingsNext` → persiste `setupData.settings` y avanza.
  - Mockear los 4 step components con `vi.mock` para aislar el
    orquestador; cada mock expone los props recibidos para verificar
    wiring (`onNext`, `onBack`, `initialData`).

- **`AccountStep.tsx`**:
  - Validación local: username < 3 chars → error, password < 8 →
    error, confirm ≠ password → error. Cada uno con su key de
    traducción (ver defaultValues).
  - `createAdmin.mutate` NO se llama si `validate()` falla.
  - `onSuccess` → `setAuth(user)` + `onNext({username, password, ...})`.
  - Server error → aparece `serverError` string en UI.
  - Mockear `useSetupCreateAdmin` con `vi.mock("@/api/hooks", ...)`
    devolviendo un mutation stub (`mutate: vi.fn()`, `isPending: bool`,
    etc. según API de TanStack Query v5).
  - Mockear `useAuthStore` — hay precedente: `web/src/store/auth.test.ts`.

- **`LibrariesStep.tsx`** (más largo, trae FolderBrowser):
  - Permitir añadir/quitar entries (name + contentType + path).
  - Validar que hay ≥ 1 biblioteca y que cada una tiene los 3 campos.
  - `onNext(entries)` solo si la lista es válida.
  - `onBack` dispara sin validar.
  - El `FolderBrowser` dispara un callback `onSelect(path)`; mockear
    para no montar la navegación real.

- **`SettingsStep.tsx`**:
  - Campos opcionales (TMDB key, hwAccel dropdown).
  - `onNext({tmdbApiKey?, hwAccel?})` siempre pasa — no hay
    validación obligatoria.
  - `onBack` funciona.

- **`CompleteStep.tsx`**:
  - Resume lo creado.
  - Botón "Start" que probablemente llama un mutation final
    (revisar el fichero) y redirige. Mockear la mutation + el router.

**Criterio de "hecho"**: los 5 ficheros del wizard cubiertos. Mínimo
~25 tests. `SetupWizard` orquestador es el más valioso (guía todo el
flujo), ataca ese primero.

### Orden sugerido si haces ambos

1. **Track A completo** (4 ficheros, +40-50 tests) → 1 commit → PR.
2. Mergear PR A.
3. **Track B completo** (5 ficheros, +25-30 tests) → 1 commit → PR.
4. Actualizar esta memoria (retirar el handoff, añadir ciclo).

Si prefieres paralelizar: A y B tocan directorios distintos
(`components/livetv/` vs `pages/setup/`), no hay conflicto real.

### Checklist al abrir sesión
- [ ] Leer este handoff + el resto de `project-status.md`.
- [ ] `git checkout main && git pull` — verifica que slice 1 está en.
- [ ] Elegir track.
- [ ] Crear rama `claude/frontend-coverage-slice-2` o
  `claude/setup-wizard-tests`.
- [ ] Atacar fichero por fichero, correr `pnpm test -- <fichero>`
  frecuentemente.
- [ ] Al acabar: `pnpm test` + `pnpm tsc --noEmit` + `pnpm build`
  verdes, commit descriptivo, push, PR.

---

## Resumen ejecutivo

MVP funcional con backend maduro y frontend refactorizado. Sqlc completa, 15/15
handlers backend con tests, IPTV hardened (SSRF, timeouts, ACL por biblioteca,
dedup M3U), LiveTV partido en 10+ ficheros con **EPG grid real** estilo Plex.

Arco de trabajo de esta rama — la parte de LiveTV de "funciona" a "producto":
importador davidmuma robusto (fix 414 + UTC scan), EPG **multi-fuente con
catálogo curado** (prioridad + merge + per-source status), **health-check
oportunista** de canales (apagados auto-ocultos del user, surface para admin),
**edición manual de tvg-id** con overrides persistentes entre M3U refreshes,
**admin polish** (panel unificado con status dot + stats strip + tabs), y
**Discover rediseñado** (tarjetas limpias, rail Apagados, hero personalizable
con auto-preview HLS + persistencia multi-dispositivo). 14 commits, todos con
tests + lint + go race verde.

Ciclo de cierre (`claude/review-memory-tv-code-8bjei`, 1 commit + 1 follow-up):
revisión de código contra la memoria del arc, **−400 loc netas** con cero
cambios de comportamiento y **dos bugs arreglados**: el fallback del logo
roto de `ChannelCard` ya muestra las iniciales (antes dejaba el hueco), y la
barra de progreso del `HeroSpotlight` se actualiza también con un solo item
(antes quedaba congelada hasta que cambiaba `nowPlaying`). Extraídos tres
hooks / módulos reutilizables: `useNowTick`, `useHeroSpotlight`,
`livetv/categoryOrder`. Mergeado en PR #81.

Ciclo siguiente (`claude/frontend-livetv-coverage`, 1 commit): primera
tajada del coverage-burndown frontend identificado en la senior review.
**Vitest 74 → 138 tests (+64)** cubriendo los hooks y componentes
recién partidos. Los 7 ficheros nuevos son todos <200 loc sin setup de
i18n. Incluye **test de regresión** para el fix del logo roto de
`ChannelCard`, blindado contra retrocesos. Abre PR con los 64 tests
+ la actualización de esta memoria + 3 patrones nuevos de testing en
`conventions.md`.

## Tamaño verificado

- **~100** ficheros `.go` de producción · **~60** `_test.go`
- **74** rutas HTTP (ver `internal/api/router.go`)
- **~25** test files en frontend (añadidos `livetv/` + 5 del wizard)
- **Cero** `TODO`/`FIXME`/`HACK`
- `go test -race ./...` verde en 21 paquetes; `golangci-lint v1.64.8`: exit 0
- Frontend: `pnpm build` + `pnpm test` (186/186) verdes, `pnpm tsc --noEmit` verde

## Setup wizard coverage (`claude/review-project-tasks-f5Rdg`)

Ruta crítica del primer arranque — antes 0 tests, ahora **+48**
cubriendo los 5 ficheros del wizard. Aísla el orquestador del resto
mockeando los 4 step components (`vi.mock`) para testar sólo
transiciones + persistencia de `setupData`; cada step se testea
contra mocks de `@/api/hooks` y `@/store/auth` para desacoplar de red.

Delta: **138 → 186 tests en Vitest** (+48), 5 ficheros nuevos.

| Fichero | Tests | Lo que pin-ea |
|---|---|---|
| `web/src/pages/setup/SetupWizard.test.tsx` | 7 | `initialStep` mapping + fallback step 0 · transición completa 0→3 con persistencia de data · back re-hydrata user en Account · step indicator refleja currentStep |
| `web/src/pages/setup/AccountStep.test.tsx` | 9 | Validación (username<3, password<8, mismatch) bloquea mutate · submit normaliza trim + displayName undefined · onSuccess setAuth + onNext · **fallback login SETUP_COMPLETED** (happy + bad password → adminExists copy) · server error surface · hydratación initialData |
| `web/src/pages/setup/LibrariesStep.test.tsx` | 12 | 1 entry por defecto (sin remove) · skip con rows vacías → onNext([]) sin mutate · validación per-field (name sin path → error) · mutation payload con `content_type` + `paths[]` · onSuccess forward al snake-case interno · FolderBrowser mockeado (open + pick-path auto-fill name, NO overwrite si name existe) · add/remove rows idempotente (identifica por `select[id^="content-type-"]` porque Input genera id duplicado desde label) |
| `web/src/pages/setup/SettingsStep.test.tsx` | 10 | No-op skip (onNext(empty) sin mutate) · Skip button · TMDB key trim + payload solo con fields presentes · ffmpeg-missing badge · radio options solo si `ffmpeg_found && hw_accels.length` · hw_accel selection en mutation · server error surface |
| `web/src/pages/setup/CompleteStep.test.tsx` | 10 | Summary muestra user + libraries con path · "None added" cuando no hay libraries + checkbox oculto · scan checkbox checked por defecto · toggle forward false · `useSetupComplete.mutate(scanFlag)` · onSuccess navigate("/") · onError surface + NO navigate · hw_accel uppercase · software default |

**Patrones de test nuevos descubiertos** (añadir a `conventions.md`
si reincide en otros forms):

1. **setServerError tras `onError` necesita `findByText`, no
   `getByText`**. Llamar `handlers.onError(...)` directamente (fuera
   de un user event) no envuelve el `setState` en `act()` de React
   18/19, así que la aserción corre antes del re-render. `findByText`
   espera el siguiente tick y pasa.
2. **`<Input>` genera `id = label.toLowerCase().replace(/\s+/g,"-")`**
   — cuando un formulario repite rows con el mismo label (ej.
   LibrariesStep), dos `<input>` acaban con el mismo `id` y
   `<label htmlFor>` solo apunta al primero. `getAllByLabelText`
   devuelve N-1 matches. Workaround: identificar rows por un atributo
   único (ej. `select[id^="content-type-${idx}"]` que sí es index-ed)
   y leer el sibling `<input placeholder^="...">`.
3. **Mock de `react-router`**: `vi.mock("react-router", () => ({
   useNavigate: () => navigateMock }))`. No hace falta envolver en
   `<MemoryRouter>` si solo se usa `useNavigate` — el mock corta la
   dependencia.
4. **`@/i18n` se importa al inicio del test file** para inicializar
   i18next con el bundle `en.json`. Así los keys resuelven a strings
   reales y los asserts son legibles (`/Password must be at least 8/`
   en vez del path crudo). Patrón opuesto al de los componentes
   livetv (que usan `defaultValue` inline), más apropiado aquí porque
   el wizard no lo hace.

## Frontend coverage — slice 1 (`claude/frontend-livetv-coverage`)

Primer paso del track de estabilidad. Cubre los hooks y componentes
extraídos en PR #81, que al ser ya pequeños y SRP-estrictos hacen los
tests triviales (~30 loc de media por test).

Delta: **74 → 138 tests en Vitest** (+64), 7 ficheros nuevos <200 loc.

| Fichero | Tests | Lo que pin-ea |
|---|---|---|
| `web/src/hooks/useNowTick.test.ts` | 6 | Tick inicial · re-tick boundary · interval custom · unmount limpia timer · intervalMs change resetea cadencia |
| `web/src/components/livetv/epgHelpers.test.ts` | 17 | `getNowPlaying` (start inclusivo/end exclusivo) · `getUpNext` (contrato no-sort, confía en backend `ORDER BY start_time`) · `getProgramProgress` clamp 0..100 · `formatTime` · `capitalize` |
| `web/src/components/livetv/useHeroSpotlight.test.ts` | 9 | Fallback silencioso favorites → live-now → newest · `off` honrado sin fallback · 6-item cap · live-now ordenado por channel number · `setMode` forward · mockea `useUserPreference` para aislar |
| `web/src/components/livetv/ChannelCard.test.tsx` | 12 | Identity · EPG on-air / no-EPG · logo vs iniciales · **regression test del fix del logo roto** (fireEvent.error → avatar aparece) · favorite toggle aislado del card click · dimmed "Apagado" |
| `web/src/components/livetv/FavoritesView.test.tsx` | 6 | Empty state · filtrado favoriteSet · stale-favorites dropped silently (post-M3U refresh) · wiring callbacks · EPG data pasa a las cards |
| `web/src/components/livetv/OverlayHeader.test.tsx` | 7 | Identity + country uppercase · close wiring · heart condicional · aria-pressed refleja isFavorite · country opcional |
| `web/src/components/livetv/NowPlayingCard.test.tsx` | 7 | No-EPG fallback · campos + duración + categoría · up-next condicional · barra de progreso driven-by-`now` prop (50% halfway, clamped 0/100) |

**Patrones de test descubiertos** (ahora en `conventions.md`):

1. `user-event` + `vi.useFakeTimers` **se bloquea** con componentes que
   tienen `setTimeout` internos (el debounce de hover de `ChannelCard`
   es el caso que reventó). El `userEvent.click` nunca resuelve
   porque su cola interna no avanza. `fireEvent` síncrono ejercita
   los mismos handlers sin deadlock.
2. `<img alt="">` decorativas quedan **fuera del accessibility tree**
   por WAI-ARIA — `getByRole("img")` no las encuentra. Para estos
   casos usar `container.querySelector("img")`. Queda consistente con
   que screen readers tampoco las anuncian.
3. Componentes que usan `useTranslation()` con `defaultValue` en cada
   key funcionan en tests **sin inicializar i18n**. Con el provider
   ausente, `t("missing", {defaultValue: "X"})` devuelve "X". El
   codebase lo usa por convención (ver cualquier componente livetv),
   así que los tests no tienen que montar provider.

## Simplify sweep (2026-04-24, `claude/review-memory-tv-code-8bjei` — PR #81)

Review completa del paquete `internal/iptv` + `web/src/components/livetv` +
`web/src/pages/LiveTV.tsx` contra la memoria de este arc. Un único commit
grande con 9 cambios ordenados por impacto:

1. **`HeroMosaic.tsx` borrado** (−182 loc): reemplazado por `HeroSpotlight`
   en `5baeae5`, quedaba exportado en el barrel sin que nada lo importara.
2. **`stopCh` eliminado de `iptv.Service`**: creado, cerrado por `Shutdown`
   y nunca seleccionado. `Shutdown()` queda como no-op documentado para no
   romper el patrón simétrico con los otros servicios.
3. **HLS en `proxy.go` deduplicado**: extraídos `peekForHLS`,
   `absorbAndRewriteHLS`, `isAmbiguousStreamCT`. `ProxyStream` y `ProxyURL`
   usan los mismos primitivos; un fix futuro ya no se puede aplicar a solo
   uno de los dos paths (`pipeStream` con flush vs `io.Copy`, ACAO, health
   reporting siguen siendo diferentes a propósito).
4. **`useNowTick(ms)` hook nuevo** (`web/src/hooks/useNowTick.ts`): colapsa
   tres `setInterval → setState(Date.now())` idénticos en `EPGGrid`,
   `PlayerOverlay` y `HeroSpotlight`. **Bug colateral arreglado**: la barra
   de progreso del hero se congelaba con `items.length < 2` (el timer de
   auto-rotate no disparaba).
5. **Fallback real del logo en `ChannelCard`**: `onError` ponía
   `display: none` al `<img>` y las iniciales vivían en la rama opuesta del
   ternario — el resultado era un hueco sobre el gradient. Ahora se trackea
   `failedLogoUrl` y se muestra `<ChannelLogo>`. Estado derivado de props
   para evitar `setState-in-effect`.
6. **`useHeroSpotlight` extraído**: la preferencia + el fallback silencioso
   `favorites → live-now → newest` + las opciones del menú vivían en
   `LiveTV.tsx` (~130 loc). Ahora en `web/src/components/livetv/
   useHeroSpotlight.ts`. `LiveTV.tsx` baja de **763 a ~600 loc**.
7. **`getUpNext` simplificado**: quitada la ordenación cliente-side
   (`[...programs].sort`). El backend ya devuelve `ORDER BY start_time` en
   `internal/db/epg_repository.go:118,198`.
8. **`livetv/categoryOrder.ts`**: orden canónico de categorías en un único
   sitio. Antes duplicado entre `LiveTV.tsx:railOrder` y
   `CategoryChips.tsx:defaultOrder`.
9. **Guide tab ya no traga búsquedas sin resultado**: `channels={filteredChannels
   .length > 0 ? filteredChannels : channels}` era un silent fallback que
   mostraba TODOS los canales cuando la búsqueda no matcheaba nada. Ahora
   pasa `filteredChannels` y `EPGGrid` muestra su empty state.

Follow-up del mismo día (mismo ciclo, commit aparte):
- **`PublicEPGSources()` → `var publicEPGSources`**: la función devolvía un
  slice nuevo en cada llamada. Ahora variable de paquete, read-only por
  convención, zero-alloc en el hot path del handler del catálogo.
- **`capitalize(s)` compartido en `epgHelpers.ts`**: antes duplicado en
  `LiveTV.tsx` y `PlayerOverlay.tsx`.

Split por concern (commit siguiente):

**Backend — `internal/iptv/service.go` (904 loc → 8 ficheros)**:
```
service.go              struct + NewService + Shutdown + fetchURL + maybeDecompress + generateID/assignNumber
service_favorites.go    Add/Remove/Is/ListIDs/ListChannels
service_m3u.go          RefreshM3U + overrides re-apply + EPG auto-trigger
service_epg.go          RefreshEPG + refreshOneSource + buildChannelLookups + matchChannel + nameVariants + qualityRE + GetSchedule/BulkSchedule/NowPlaying + CleanupOldPrograms
service_channels.go     GetChannels/GetChannel/GetGroups/SetChannelActive
service_health.go       RecordProbe* + sanitiseProbeError + ListUnhealthy + ResetHealth
service_overrides.go    ChannelWithoutEPGWindow + ListChannelsWithoutEPG + SetChannelTvgID
service_epg_sources.go  List/Add/Remove/Reorder + PublicEPGCatalog
```
Go permite métodos sobre `*Service` repartidos en varios ficheros del
mismo paquete, así que la API pública no cambia. Callers intactos.
El mayor nuevo es 346 loc (service_epg.go, coherente: refresh + query +
matcher), el menor 40 (service_channels.go).

**Frontend — containers partidos**:
```
LiveTV.tsx                763 → 299 loc (solo orquestación)
PlayerOverlay.tsx         485 → 278 loc (layout + primitivas)
components/livetv/
  LiveTvTopBar.tsx    new  título + counts + search + tabs + hero gear
  DiscoverView.tsx    new  hero + chips + rails + "Apagados"
  FavoritesView.tsx   new  grid derivado de favoriteSet
  OverlayHeader.tsx   new  close + identity + favorite toggle
  NowPlayingCard.tsx  new  "Ahora en antena" + progress + up-next
```
Cada fichero nuevo 70–146 loc, SRP estricto, testeable sin montar
páginas enteras. Barrel `components/livetv/index.ts` re-exporta.

Extra follow-up:
- **`CountrySelector` lint-limpio**: antes `useEffect → setState` para
  hidratar la auto-detección de país, lo que el `react-hooks/set-state-
  in-effect` del proyecto señala como cascading render bajo React 19.
  Refactor: `autoDetectedCountry = useMemo(...)` + `selectedCountry =
  userPicked ?? autoDetectedCountry`. Sin effect, el click del usuario
  sigue ganando. Queda 10 errors / 4 warnings de lint (todos en
  `AppLayout`, `MediaGrid`, `FolderBrowser`, `ItemDetail` — zonas que
  no son livetv).

**Verificación total del ciclo**: `go test -race ./...` 21/21 verde ·
`pnpm test` 74/74 · `pnpm build` ok · `pnpm lint` −1 error vs HEAD.

## Último ciclo de trabajo (2026-04-24, live-TV arc)

1. **Fix 414 en EPG bulk schedule** — el usuario importó 7008 programas
   desde davidmuma/EPG_dobleM y todas las tarjetas mostraban "sin guía".
   Causa: `useBulkSchedule` serializa TODOS los channel IDs en el query
   string; una biblioteca con ~260 canales produce una URL de ~9 KB que
   nginx rechaza con 414 antes de llegar a Go. Los programas ya estaban
   en la DB — el fallo era puro transporte.
2. **POST /api/v1/channels/schedule** — el handler `BulkSchedule` acepta
   ahora GET (compat) y POST con body JSON `{channels, from, to}`. Body
   limitado a 1 MiB con `MaxBytesReader`, `DisallowUnknownFields` para
   rechazar payloads malformados, tope de 5000 canales por request.
3. **Chunker en `EPGProgramRepository.BulkSchedule`** — el IN() dinámico
   se parte en bloques de 500 ids antes de pegar a SQLite (default
   `SQLITE_LIMIT_VARIABLE_NUMBER=999`). Dedupe previo para que un id
   repetido no duplique filas al mergear chunks. Tests con 1200 canales
   verifican que ningún row se pierde en los bordes.
4. **Frontend `getBulkSchedule` a POST** — siempre. Más simple y robusto
   que una heurística por tamaño. Tests añadidos al `client.test.ts`
   validan la shape del request y el short-circuit con lista vacía.
5. **nginx `large_client_header_buffers 8 32k`** — defensa en
   profundidad. El POST ya evita el 414, pero subir el buffer previene
   regresiones si alguien añade un endpoint GET con listas grandes en
   el futuro.
6. **Fix bug en `nameVariants`** (pre-existente en 7d95d1e): nombres
   con whitespace doble ("  Canal  Sur  ") generaban una variante
   espuria "canal  sur" además de la colapsada "canal sur", que nunca
   emparejaba nada real. Ahora el folding normaliza whitespace en la
   variante base también.
7. **Fix runtime 500 en `BulkSchedule`** (descubierto por el usuario
   contra un container real): modernc.org/sqlite serializa `time.Time`
   con Location nombrada usando `time.Time.String()` — por ejemplo
   `"2026-04-24 12:00:00 +0200 +0200"`. El Scan por defecto del driver
   no es capaz de deserializar ese formato. Afecta a todos los feeds
   XMLTV que traen offset (davidmuma, iptv-org, epg.pw). Fix en dos
   capas: (a) `ReplaceForChannel` normaliza a UTC antes de insertar
   vía sqlc, y (b) `BulkSchedule` / `Schedule` / `NowPlaying` leen con
   SQL crudo + `coerceSQLiteTime`, helper que acepta tanto
   `time.Time` como strings (`RFC3339` y el legado Go-stringer), de
   modo que bases de datos pre-fix siguen leyéndose. Tests cubren el
   roundtrip XMLTV exacto + legacy row sembrada con INSERT crudo.
8. **Multi-fuente EPG + catálogo curado**: una biblioteca livetv puede
   tener N proveedores XMLTV en orden de prioridad; el refresher los
   recorre y mergea "primera fuente gana por canal" (davidmuma lleva
   las grandes, epg.pw rellena los 216 canales huérfanos). Si una
   fuente 404ea, el refresh sigue con las demás — cada fuente persiste
   `last_status`, `last_error`, `last_program_count`, `last_channel_count`
   para que la UI del admin muestre badges "✓ 3200 programas" / "✗ 404".
   Nueva migración `007_library_epg_sources.sql` con FK + UNIQUE(library,
   url); migra el antiguo `libraries.epg_url` a una fila priority-0.
   Nuevo `internal/iptv/epg_catalog.go` con 10 fuentes curadas
   (davidmuma x4, epg.pw x6 multi-idioma). Endpoints nuevos:
   `GET /iptv/epg-catalog`, `GET /libraries/{id}/epg-sources` (viewer
   con ACL), `POST` / `DELETE` / `PATCH .../reorder` admin-only. Panel
   nuevo `EPGSourcesPanel` en LibrariesAdmin con dropdown del catálogo,
   input de URL custom y reorder vía botones ↑/↓ accesibles por teclado.
   Cubierto por tests (repo + service + handler + catalog lookup).
9. **Health-check oportunista de canales** (estilo Plex): el proxy
   IPTV graba cada intento de conectar al master playlist contra la
   fila del canal. Éxito → resetea contador; fallo → incremento
   atómico. Cancelaciones del cliente (usuario cierra pestaña) se
   filtran explícitamente y NO cuentan — la DB solo refleja fallos
   reales de upstream. Nueva migración `008_channel_health.sql` añade
   columnas `last_probe_at`, `last_probe_status`, `last_probe_error`,
   `consecutive_failures` a `channels` más un índice parcial sobre
   `(library_id, consecutive_failures) WHERE consecutive_failures > 0`.
   El handler de canales del usuario llama a `ListHealthyByLibrary`
   que excluye canales con ≥ `UnhealthyThreshold` (=3) fallos
   consecutivos; el cliente normal no ve canales rotos. Endpoints
   admin nuevos: `GET /libraries/{id}/channels/unhealthy[?threshold=N]`,
   `POST /channels/{id}/reset-health`, `POST /channels/{id}/disable`,
   `POST /channels/{id}/enable`. Interfaz `iptv.ChannelHealthReporter`
   (nil-safe) desacopla proxy↔DB; implementada en `iptv.Service` con
   timeout 2s para que la escritura DB no bloquee el hot path.
   Componente `UnhealthyChannelsPanel` en LibrariesAdmin muestra el
   panel solo si hay problemas (cero ruido si la biblioteca está
   sana); poll 30s; acciones "Marcar OK" (reset counter) y
   "Desactivar" (flip is_active). Tests: repo (atomic +1 concurrente,
   trim error largo, filtro threshold, reset, ListHealthy esconde
   desactivados+unhealthy), proxy (nil reporter, éxito, fallo,
   cancelación del cliente ignorada, DeadlineExceeded cuenta),
   handler (threshold custom, ACL deny, cada acción).
10. **Edición manual de tvg-id + panel "canales sin guía"**: los IDs
    de canal son UUIDs aleatorios que se regeneran en cada M3U
    refresh, por lo que cualquier edición del admin se perdería
    inevitablemente. Solución: tabla nueva `channel_overrides(library_id,
    stream_url, tvg_id)` (migración 009) keyed por el atributo más
    estable del canal — el stream URL. Ciclo: admin hace
    `PATCH /channels/{id}` → `service.SetChannelTvgID` hace UPDATE en
    `channels` (cambio inmediato) + UPSERT en `channel_overrides` (por
    stream_url) → siguiente M3U refresh corre `ReplaceForLibrary` (wipe)
    y luego un nuevo hook `overrides.ApplyToLibrary(libraryID)` reaplica
    los overrides en una transacción. Overrides huérfanos (stream_url
    ya no en el playlist) son no-op y se quedan en la tabla esperando
    a que la URL vuelva. Endpoints nuevos:
    `GET /libraries/{id}/channels/without-epg` (LEFT ANTI-JOIN contra
    epg_programs en ventana -2h..+24h) y `PATCH /channels/{channelId}`
    admin-only con `{tvg_id}` opcional (string vacía limpia override y
    columna). Componente `ChannelsWithoutEPGPanel` con edición inline;
    solo aparece si hay orphans. Tests: repo (upsert replaza, apply
    rewritea en 1 tx, orphan no-op, delete idempotente, listar
    without-EPG), service end-to-end (override sobrevive un refresh M3U
    real contra httptest, clearing elimina la fila persistente, orphans
    detectados correctamente), handler (happy path, empty=clear,
    missing=no-op, invalid JSON 400, ACL deny 404, trim whitespace).
11. **Catálogo EPG verificado + 409 duplicados + invalidación refresh**
    (`36f36c4`): el catálogo inicial shippeaba URLs a ojo — `guiaiptvmovistar.xml`
    y `tdtsat.xml` no existen en davidmuma; todas las `epg.pw/api/epg.xml.gz?lang=*`
    daban 404 (URL scheme inventado). Ahora el catálogo es 5 entradas
    davidmuma HEAD-verificadas contra upstream (`guiatv.xml.gz`,
    `guiaiptv.xml`, `guiatv_plex.xml.gz`, `guiafanart.xml.gz`,
    `tiviepg.xml`); internacional queda a URL personalizada. Test
    `NoKnownBrokenURLs` impide que vuelvan a colarse por accidente.
    Además: `ErrEPGSourceAlreadyAttached` sentinel en el repo + 409
    Conflict + mensaje limpio (antes salía el `UNIQUE constraint
    failed` crudo de SQLite). Y `useRefreshEPG` invalida ahora
    `libraryEPGSources` + `channelsWithoutEPG` para que tras "Refrescar
    EPG" las badges se actualicen y los canales recién emparejados
    salgan del panel de huérfanos, sin recargar.
12. **Panel admin livetv unificado** (`1548dd6` + `381d5d6`): los tres
    paneles apilados (fuentes EPG + sin guía + con problemas) se comen
    la pantalla en cualquier biblioteca real. Primero hice el panel
    "sin guía" colapsable con búsqueda + paginación (20 filas inicial,
    "Mostrar más" de 40 en 40, filtro por número/nombre/tvg-id/grupo).
    Luego el refactor final: los tres paneles pasan a ser "tab bodies"
    dentro de un solo contenedor `LivetvAdminPanel` con header de
    status (dot 🟢/🟡/🔴/⚪ agregado de todas las señales), stats strip
    (`268 canales · EPG 52 (19%) ▓░ · 3 con problemas · 211 sin guía`
    con mini barra de cobertura), y pestañas que solo aparecen cuando
    tienen contenido. Tab por defecto auto-selecciona la más
    problemática. Full WAI-ARIA tabs pattern (← → Home End,
    aria-selected/controls/labelledby).
13. **Visual: tarjetas más limpias + rail "Apagados"** (`5f713ad` +
    `5baeae5`). Dos problemas reportados por el usuario:
    - Cards pintaban `logo_bg` a 67-100% opacidad → cada rail era un
      arcoíris. Ahora backdrop neutral oscuro con un radial suave de
      la marca al 20% — hint de identidad sin que compita.
    - La card envolvía thumbnail + info en una sola caja con borde,
      estilo "boxed". Ahora el borde rodeado vive solo en el
      thumbnail (el poster es la card visualmente); nombre / "Ahora"
      / up-next caen debajo como texto sobre el fondo de la página.
      Estilo YouTube/Netflix/Plex. La barra de progreso EPG pasa a
      una línea fina al borde inferior del poster.
    - Rail "Apagados" al final de Discover: tarjetas grises + badge
      "Apagado" (en lugar de LIVE), sin hover preview (no quemar
      bandwidth en canales que creemos muertos), click sigue
      funcionando (admin puede probar si volvió). Solo aparece con
      filtro "Todos". `UnhealthyChannel` extiende `Channel` para que
      pase al mismo `ChannelCard` con un flag `dimmed`.
14. **Hero personalizable por cuenta** (`5baeae5` + `ebbc64d`): el
    mosaico de 5 tiles aleatorios se sustituye por un `HeroSpotlight`
    de 1 tile grande con **auto-preview muted HLS** en el mount
    (landing feel-alive). Label claro arriba ("Tu favorito" / "En
    directo ahora" / "Recién añadidos"). Carousel dots cuando hay
    más de 1. Señal elegible por el usuario desde una rueda que
    vive **en la TopBar** (no dentro del hero — los 3 bugs que el
    usuario encontró: hero vacío con favoritos 0, dropdown clipeado
    por la aspect-ratio del poster, "Ocultar" como trampa sin vuelta).
    Auto-fallback silencioso: si favoritos está vacío, resuelve a
    live-now; si también, a newest. Label reflejando lo que está
    realmente en pantalla. Persistencia multi-dispositivo vía nueva
    tabla `user_preferences(user_id, key, value)` — migración 010 +
    `/api/v1/me/preferences[/{key}]` scoped a la sesión (sin lectura
    cruzada) + hook frontend `useUserPreference<T>(key, default)` con
    optimistic cache update + JSON encode/decode. `HoverPreview` del
    card se promueve a `StreamPreview` compartido con el hero.
    `pointer-events-none` en el `<video>` fija un bug de dwell que
    cancelaba el preview al entrar el cursor sobre el video. Tests:
    repo user_preferences (upsert, isolation por usuario, delete
    idempotente).

**Impacto operativo**: importar davidmuma ya funciona end-to-end.
Poner la URL XMLTV (o .gz) en el campo *EPG URL* de la biblioteca
livetv y darle a "Refrescar EPG". El matcher fuzzy (tvg-id → display-
name con quality strip + accent fold) junta los programas con los
canales correctos. El frontend carga la guía sin 414 hasta ~5000
canales por request.

## Ciclo anterior (2026-04-17)

1. **Auditoría IPTV** — detectados 5 problemas en backend y desajuste UX en frontend
2. **Tier 1 seguridad backend** (`85263d8`): SSRF proxy, timeouts transport,
   candado EPG refresh, relay cleanup, M3U dedup + BOM — 14 tests de regresión
3. **ACL por biblioteca** (`f22ce73`): 8 endpoints de canal/EPG ahora consultan
   `library_access`. 404 (no 403) para no filtrar existencia. Admin bypasa —
   10 tests ACL que verifican que el proxy NO se invoca en deny
4. **LiveTV refactor + EPG grid real** (`a8372fa`):
   - 828 líneas → ~420 + 8 módulos bajo `web/src/components/livetv/`
   - EPGGrid nuevo: filas de canales × columnas de tiempo con línea "ahora"
     auto-scroll, role=grid + a11y
   - Toggle Carrusel / Guía
   - `useLiveHls` hook nuevo (reemplaza HLS inline duplicado)
   - Zapping por teclado (↑↓ + números + Enter) con indicador visual
   - Logos con fallback a número en error
   - Canales inactivos filtrados
   - `aria-live` hero, `role=progressbar`, `<label>` en inputs
   - Bug fix: `useBulkSchedule` query key truncaba a 10 IDs
5. **Fix CI lint** (`a7fb54c`): eliminado código muerto de sqlc migration
   (`nullStr`, `applyFull`, `fullScanDests`, campos huérfanos)
6. **Fix CI flake + production hazard** (`3e90af9`): `library.Service` ahora
   tiene `Shutdown()` con `bgCtx` + `WaitGroup`. Auto-scan goroutines ya no
   corren con `context.Background()` detached.

## Arquitectura actualizada

### Patrón de servicios con goroutines de fondo

Servicios con Shutdown(): `stream.Manager`, `iptv.Service`, `auth.Service`,
`library.Service`. Todos siguen el mismo patrón:

```go
type Service struct {
    bgCtx    context.Context
    bgCancel context.CancelFunc
    bgWG     sync.WaitGroup
}
// goroutines: inheritan bgCtx, registran en bgWG, Shutdown cancela+espera
```

`main.go` los llama en orden de dependencia antes de cerrar DB. Tests usan
`t.Cleanup(svc.Shutdown)` para LIFO: Shutdown → DB close → TempDir rm.

## Bulk EPG — contrato del endpoint

`GET /api/v1/channels/schedule?channels=a,b,c&from=…&to=…` y
`POST /api/v1/channels/schedule` con body
```json
{ "channels": ["a","b","c"], "from": "-2", "to": "24" }
```

- `from`/`to` aceptan RFC3339 o entero (horas). Default: -2h..+24h.
- ACL por canal: cada id se valida individualmente contra
  `library_access`; canales no accesibles se ignoran silenciosamente.
  Admin bypasa.
- Dedup + chunk internos: el repo corta en bloques de 500 ids antes del
  IN() de SQLite. Tope de 5000 canales por request (400 si se excede).
- Frontend siempre POST — el GET queda para curl / back-compat.

## Lo que falta (no bloqueante)

### Frontend Live TV
- **"Continuar viendo"** — último canal visto requiere tracking backend
  (tabla `channel_watch_history(user_id, channel_id, last_watched_at,
  seconds_watched)`). Cuando exista, se añade rail en Discover por
  encima de los de categoría (Fase 2 del plan Plex-max)
- Modal de detalles de programa al clicar en el EPG grid
- Virtualización de filas del EPG (flat DOM aguanta ~200 canales bien)
- Búsqueda unificada de canales + programas
- Grid EPG con colores por categoría + filtros

### IPTV backend
- XMLTV streaming parser (bomba memoria con feeds de 2GB; `xmltv.go:70-76`)
- Matcher más agresivo para subir cobertura EPG: tabla de alias conocidos,
  matching por channel number cuando ambos lo tienen, Levenshtein fuzzy
  con threshold. Hoy es tvg-id + display-name variants + quality strip +
  accent fold — suficiente para ~52/268 con davidmuma, pero escalaría
  a 70-80% con el refuerzo

### Event bus (3 tipos reservados sin publisher)
- `ChannelAdded`/`ChannelRemoved` — requiere descomponer `ReplaceForLibrary`
- `MetadataUpdated` — necesita hook en scanner o flujo de refresh dedicado

### Frontend general
- Tests del setup wizard (0 cobertura, ruta crítica de primer arranque).
- Tests de páginas admin (mutan estado, 0 tests — los panels nuevos
  `LivetvAdminPanel` + sub-paneles entran en este hueco).
- Tests de componentes livetv restantes: `LiveTvTopBar`, `DiscoverView`,
  `HeroSpotlight`, `EPGGrid`. Los 7 iniciales ya cubiertos en el slice 1.
- Lint pre-existente `react-hooks/set-state-in-effect` en **4** ficheros:
  `AppLayout`, `MediaGrid`, `FolderBrowser`, `ItemDetail` (CountrySelector
  ya barrido). Bajo React 19 + Compiler son re-renders en cascada reales,
  no nits. Patrón de fix aplicado en CountrySelector: derivar con
  `useMemo` + `user pick ?? auto`, sin effect → setState.

### Arquitectura / deuda estructural (senior review 2026-04-24)
- **`library.Service`**: por auditar. Probablemente misma forma god-service
  que tenía `iptv.Service` (scan + schedule + metadata + image refresh en
  un struct). Aplicar la misma receta de split por ficheros
  (`service_*.go` sobre el mismo struct).
- **`internal/api/handlers/`** — si crece a 15+ ficheros sueltos, partir
  en subpaquetes por dominio. Hoy no urgente: foco en tests.
- **`web/dist/` tracked en git**: PRs ruidosos cada build. Alternativa:
  `.gitignore` + CI build + embed vía tag. ~0.5 día.

### Escalado / producción real
- **IPTV proxy 1:1 upstream-por-viewer**. Límite práctico ~100 concurrentes
  por canal; un viral tumba el CDN free con bans. Fan-out relay pendiente.
- **Circuit breaker en proxy** — retries infinitos contra upstreams
  caídos. Operacional más que funcional.
- **E2E tests** (Playwright) — cero hoy. Para media-server la secuencia
  play → transcode/direct → stream tiene tres seams; unit tests no los
  cubren.

### Ops (fuera de repo puro, pero parte de "producción real")
- Rate-limit: verificar scope (por-IP, por-user, por-endpoint).
- Backup automatizado de SQLite (WAL copy).
- Rotación JWT signing key ejercitada end-to-end.
- Dashboards Prometheus / alertas.

## Próximo paso sugerido

Sin prisa. Candidatos ordenados por impacto.

**Si foco = estabilidad**:
1. ✅ **Slice 1 coverage hecho** (`claude/frontend-livetv-coverage`):
   7 ficheros, +64 tests. Queda `LiveTvTopBar` / `DiscoverView` /
   `HeroSpotlight` / `EPGGrid` — mismo patrón, ~1 día.
2. **Tests del setup wizard** — ruta crítica de primer arranque con 0
   cobertura. Si se rompe, nadie llega a usar el producto.
3. **Tests de páginas admin** (`LivetvAdminPanel` + sub-paneles).
4. **Burndown del lint debt restante** (4 ficheros).
5. **Split de `library.Service`** siguiendo la receta del iptv.

**Si foco = experiencia LiveTV**:
1. **Matcher EPG más agresivo** — sube cobertura 52/268 → 150-200+.
2. **"Continuar viendo"** + tabla de historial.
3. **Modal de detalle de programa** en EPG grid.
4. **Streaming parser del XMLTV** (bomba memoria con feeds 2 GB).
