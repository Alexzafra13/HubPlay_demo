# Estado del proyecto

> **Entrypoint de cada sesión.** Lo viejo (todo lo previo a la sesión
> 2026-05-19/20) vive en `archive/`. No se pierde nada — sólo se mueve
> de sitio para que este fichero sea legible de un vistazo.

---

## 🔭 Estado actual (2026-05-21, cierre)

- Branch principal: `main`, working tree limpio.
- Última release pública: `nightly` rolling tag (workflow `release.yml`).
- Tests: `go test -race ./...` verde en CI; frontend **616/616** vitest verdes; `tsc -b` limpio; production build limpio (~25 s con React Compiler + LazyMotion activados).
- **React Compiler activado** + `eslint-plugin-react-compiler` como hard gate. `react-compiler-healthcheck`: 542/542 componentes compatibles. Quality gates en CI: `typecheck` (hard), `react-compiler-healthcheck` (hard), **`knip` (hard desde 2026-05-21)**, `react-doctor` (visibility-only con comentarios inline en PRs).
- **Score React Doctor: 75/100 ("Great")**. 16 reglas únicas eliminadas (13 + 3 mecánicas hoy: `no-array-index-as-key`, `no-long-transition-duration`, `rendering-hydration-mismatch-time`). 6 issues estructurales pendientes — todos en `VideoPlayer.tsx` (sesión dedicada).
- **knip: 0 unused files / 0 unused deps / 0 unused exports / 0 unused types** tras #375 + #377 (cleanup −1131 líneas en 59 ficheros). Hard gate en CI.
- HubPlay distribuible "descargar y usar" en los tres targets (desktop / Linux server / NAS-via-Docker) — flujo cerrado en la sesión 2026-05-19/20.

---

## 🧹 Sesión 2026-05-21 — Cleanup knip a 0 + React Doctor quick wins + hard gate

Sesión corta y limpia. Tres PRs encadenadas para cerrar la deuda de dead code que arrastraba desde la integración inicial de knip (PR #355) y atacar los 3 issues mecánicos de React Doctor que aparecieron tras el cleanup.

### PRs cerradas

| PR | Tema | Diff |
|---|---|---|
| [#375](https://github.com/Alexzafra13/HubPlay_demo/pull/375) | 5 unused files + 2 unused deps (`@radix-ui/react-dialog`, `@radix-ui/react-tooltip`) | −739 |
| [#377](https://github.com/Alexzafra13/HubPlay_demo/pull/377) | 7 hooks + 9 huérfanos + 30+ exports/types: `export` → interno o borrado entero | −327 |
| [#378](https://github.com/Alexzafra13/HubPlay_demo/pull/378) | 3 mecánicos React Doctor: skeleton keys, animation 1.8s→900ms, `new Date()` JSX → helper | +19/−12 |
| Este branch | `pnpm knip` elevado a hard gate en CI + memoria actualizada | — |

### Cleanup knip: lo que se aprendió

- **`import("./types").Foo` se cuela sin que knip lo detecte.** Patrón usado en `client.ts` y `media.ts` para algunos types — knip los marca como unused aunque sí se usen. Solución: sustituir por imports normales al top. Detectados 2 (PeerStreamSessionResponse, StudioDetail) y migrados.
- **Hooks "anti-pair" hibernando.** `useEnableChannel` existía como complemento de `useDisableChannel` que sí está conectado a UI; el enable nunca se conectó. Mismo patrón: `useSetChannelVisibility` admin sin UI. Si vuelven a hacer falta cuando se implemente la feature, son 5 líneas — borrarlos hoy fue safe.
- **El barrel `*/index.ts` no añade valor si todos los consumers importan archivos directos.** Casos: `components/layout/index.ts` y `pages/admin/index.ts` (5 líneas cada uno, 0 importadores). Borrados enteros.
- **`*Props` types nunca se importan**, aunque el component se importa miles de veces. Limpieza: quitar `export` keyword del type, mantener como tipo interno usado por el `function Component(props: Props)`. Cero impacto en consumers.

### Falsos positivos React Doctor — documentados, NO se tocan

Convención del repo: cuando una regla react-doctor entra en conflicto con un patrón oficial de React 19 o con `react-hooks/refs`, se prefiere el patrón oficial y se suprime el aviso con justificación inline.

| Regla | Archivo | Por qué se deja |
|---|---|---|
| `rerender-state-only-in-handlers` + `no-derived-useState` | MediaGrid.tsx:43, UserAvatar.tsx:64 | Patrón "[Adjusting state when a prop changes](https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes)" (React 19 oficial). El `useState` de tracking se mueve a render-time guarded `setState`. Usar `useRef` aquí dispara `react-hooks/refs` (no asignar `ref.current` en render). Es el único patrón que satisface las dos reglas estrictas de react-hooks. |
| `no-derived-useState` | ExternalSubsModal.tsx:39 | El state es **edición local del usuario** tras inicializar desde prop. Derivar en render reiniciaría su selección de idiomas cada vez que el padre se re-renderice. |

### Issues React Doctor estructurales pendientes — todos en `VideoPlayer.tsx`

VideoPlayer es 1003 líneas, 12 `useState`, un `useEffect` con 9 `setState` en cadena, y un `useEffect` que resetea state cuando un prop cambia (debería ser `key` prop). Las reglas implicadas:

- `no-giant-component` (1003 LoC)
- `prefer-useReducer` (12 useState)
- `no-cascading-set-state` (línea 509 — 9 setState en un effect)
- `no-derived-state-effect` (línea 628 — reset por prop change)

Para atacar de forma sensata hay que **split de VideoPlayer en subcomponentes** y consolidar el state en `useReducer`. Es refactor estructural, no auto-fix mecánico. Requiere backend corriendo en preview para verificar playback / seek / quality switching tras el split — sesión dedicada.

Mismas reglas también disparan en `HeroSection.tsx` (192 LoC) y `SeriesHero.tsx` (61 LoC) pero más manejables.

---

## 🩺 Sesión 2026-05-20 (noche) — React Doctor onboarding + 9 reglas eliminadas

**[React Doctor](https://github.com/millionco/react-doctor)** (de millionco, MIT, basado en Oxlint) audita 60+ reglas en performance, correctness, accessibility, architecture, security y bundle size, y resume todo en un score 0-100. Integrado en CI como visibility-only (PR #358) — la GitHub Action comenta inline en cada PR con las regresiones/mejoras del score. Fórmula: `score = 100 - (errores únicos × 1.5) - (warnings únicos × 0.75)`. Bandas: 75+ "Great" / 50-74 "Needs work" / <50 "Critical".

**Baseline al integrar**: 67/100, 645 issues, 47 reglas únicas.
**Final tras 5 PRs de quick wins + el fix del squash bug**: **75/100 ("Great"), 166 issues, 34 reglas únicas**.

### PRs cerradas

| PR | Reglas eliminadas | Casos resueltos |
|---|---|---|
| [#358](https://github.com/Alexzafra13/HubPlay_demo/pull/358) | — | Integración CI (job nuevo `react-doctor`, visibility-only con `continue-on-error`) |
| [#359](https://github.com/Alexzafra13/HubPlay_demo/pull/359) | `js-tosorted-immutable`, `js-combine-iterations`, `design-no-redundant-size-axes` | ~430 reemplazos en ~140 archivos |
| [#360](https://github.com/Alexzafra13/HubPlay_demo/pull/360) | `design-no-redundant-padding-axes`, `design-no-bold-heading`, `no-autofocus`, `design-no-em-dash-in-jsx-text`, `use-lazy-motion`, `rendering-hydration-mismatch-time` | 6 reglas mecánicas, −30 KB bundle, helper `dateFormat` nuevo. **Squash merge perdió 54 líneas del LazyMotion — fixed en #364** (ver abajo) |
| [#363](https://github.com/Alexzafra13/HubPlay_demo/pull/363) | `click-events-have-key-events`, `no-static-element-interactions` | 17 + 13 casos de a11y. Backdrops de modal con `onKeyDown` (Escape), bodies con `role="presentation"`, VideoPlayer container con `role="application"` + `aria-label` |
| [#364](https://github.com/Alexzafra13/HubPlay_demo/pull/364) | `use-lazy-motion` (otra vez) | Re-aplicar los 54 reemplazos `motion` → `m` que perdió el squash de #360 |
| [#365](https://github.com/Alexzafra13/HubPlay_demo/pull/365) | — | Memoria: bug del squash + regla "audit del merge" en conventions |
| [#367](https://github.com/Alexzafra13/HubPlay_demo/pull/367) | `no-array-index-as-key` (×2), `no-render-in-render`, `js-set-map-lookups`, `prefer-use-effect-event` (×2), `advanced-event-handler-refs`, `no-react19-deprecated-apis` (`forwardRef`), `no-scale-from-zero` | **Cruza el umbral 75 ("Great")**. Patrón "latest value via ref" (sustituto pragmático de `useEffectEvent`), `<ProfilePicker />` extraído del closure inline, `forwardRef` → `ref` prop |

### ⚠️ Bug del squash merge — 54 líneas de LazyMotion perdidas en #360

Descubierto el 2026-05-20 noche por el CI report de React Doctor en PR #363: la regla `use-lazy-motion` reapareció en `WhoIsWatching.tsx:27` aunque PR #360 supuestamente la había eliminado.

**Qué pasó**: el commit `LazyMotion + helper de fechas` migró 7 archivos de `motion` → `m` (54 reemplazos verificados localmente). Pero el squash merge de la PR a main aplicó SÓLO las modificaciones de otro commit anterior (autoFocus → useEffect+ref, ~8 líneas) y descartó silenciosamente las 54 líneas de LazyMotion sobre los mismos 7 archivos. `App.tsx` SÍ se mergeó con `LazyMotion strict` activado, dejando los archivos rotos: el primer usuario que llegase a Login, WhoIsWatching, MainNav, etc, vería `Error: You are rendering "motion" without LazyMotion features loaded`.

**Cómo se cazó**: React Doctor en CI dice "Score unavailable in offline mode" pero sí lista los issues. La regla `use-lazy-motion` apareció DE NUEVO en `WhoIsWatching.tsx:27` cuando ya debería estar resuelta. Auditados los 7 archivos del PR original: ninguno tenía la migración. Re-aplicado el mismo script en PR #363.

**Lección aprendida** (en `conventions.md`):
- Tras cualquier squash merge donde un script modifica decenas de archivos, **verificar en main** que los cambios reales coinciden con el diff del commit local. El visor de PR de GitHub puede ocultar conflict resolutions silenciosas.
- Si una regla del lint que se eliminó vuelve a aparecer en un PR posterior, NO asumir que es un regreso nuevo del autor — comprobar si el bug del lint ya estaba en main por un merge previo malo.

### Patrones nuevos en el proyecto

- **`[arr].toSorted()` en lugar de `[...arr].sort()`** (ES2023): ya el patrón canónico en el repo, evita el spread allocation.
- **`.flatMap()` para combinar filter+map**: en JSX o reductores rápidos. Evita el doble recorrido sobre listas grandes (cientos de canales).
- **Tailwind `size-N` y `p-N` consolidados**: nunca `w-4 h-4` o `px-2 py-2`. Convención del proyecto a partir de ahora.
- **`<m.*>` de framer-motion** en lugar de `<motion.*>`. `LazyMotion strict` en `App.tsx` carga sólo `domAnimation` por defecto (~30 KB menos en el bundle base).
- **`react-compiler/react-compiler: 'error'`** en ESLint. Las regresiones de compatibilidad con el compiler rompen el CI.
- **Helper centralizado [`src/utils/dateFormat.ts`](../../web/src/utils/dateFormat.ts)**: `formatDateTime`, `formatDate`, `formatTime`, `epochOf`. Nunca `new Date(...).toLocale*()` directo en JSX.
- **`localId: crypto.randomUUID()`** en entradas de listas dinámicas (LibrariesStep del setup wizard) para que React keys no usen índices (evita perder foco al reordenar).
- **`font-semibold` (no `font-bold`)** en headings `<hN>`: peso 700+ aplasta las contraformas a display sizes.
- **Em-dash (—) en JSX text NUNCA**: usar en-dash (–) para "sin valor" o bullet (·) para separadores inline. Em-dash lee como output AI.
- **`autoFocus` evitado**: interfere con lectores de pantalla. Cuando es UX-crítico (UpNextOverlay, WhoIsWatching PIN), patrón `useEffect + ref.current.focus()`.
- **Patrón "latest value via ref"** (sustituto pragmático de `useEffectEvent`, que aún es experimental en React 19): cuando un effect monta un listener cuyo handler depende de un prop/state pero el effect NO debería re-suscribirse al cambiar esa identidad:
  ```tsx
  const cbRef = useRef(onClose);
  useEffect(() => { cbRef.current = onClose; }, [onClose]);
  useEffect(() => {
    if (!isOpen) return;
    const handle = (e: Event) => { /* … */ cbRef.current(); };
    el.addEventListener("event", handle);
    return () => el.removeEventListener("event", handle);
  }, [isOpen]); // onClose ya NO es dep
  ```
  Aplicado en BottomSheet (escape), VideoPlayer (onEndedCallback) y ImageManager (escape). Sin esto, cada re-render del padre re-suscribiría el listener y perdería eventos durante el churn.
- **`forwardRef` eliminado de Button/Input** (React 19): ahora `ref` es prop normal en componentes función. Declarar `ref?: Ref<HTML*Element>` en la interfaz de props y desestructurar en el componente.
- **Closures de render → componente real**: ejemplo `WhoIsWatching`, donde `const renderPicker = (...)` inline se extrajo a `<ProfilePicker />` con props explícitas. Mejora reconciliación y satisface `react-doctor/no-render-in-render`.
- **Set para lookups en bucles**: `ACCEPTED_EXTENSIONS_SET = new Set(ACCEPTED_EXTENSIONS)` en `Uploads.tsx`. `.includes()` en loop = O(n²); Set = O(1).
- **Backdrops de modal accesibles**: `<button>` semántico cuando es posible, o `<div role="dialog">` con `onClick`+`onKeyDown` (Escape) + body interno `role="presentation"` con `stopPropagation` en ambos handlers.
- **`<video>` container con `role="application"`**: comunica al lector de pantalla que es un widget interactivo; añade `aria-label`.

### Reglas no eliminadas (decisiones documentadas como deuda técnica)

| Regla | Casos | Por qué se deja |
|---|---|---|
| `no-pure-black-background` | 7 | `bg-black` en contenedores de video. Cambiar a `bg-bg-base` dejaría borde gris alrededor. |
| `query-mutation-missing-invalidation` | 12 | Falsos positivos: mutations read-only (probe peer, test DB, preflight M3U, deviceAuth tres) o invalidación vía helper indirecto que el lint no detecta (images.ts). |
| `rerender-state-only-in-handlers` | 23 | **Conflicto irreconciliable** entre `react-hooks/refs` (no asignar `ref.current` en render) y `react-doctor` (que pide ese patrón). El `useState` de tracking del patrón "Adjusting state when a prop changes" satisface las dos reglas de react-hooks aunque viole esta de react-doctor. |
| `no-derived-useState` | ~6 falsos positivos | Casos donde `useState` se inicializa de un prop PERO el estado representa edición local del usuario (CollisionPicker decisiones, ExternalSubsModal langs). Derivar en render reiniciaría el trabajo del usuario en cada re-render del padre. Suprimidos narrow con justificación. |

### Reglas pendientes para próxima(s) sesión(es)

**Decisión 2026-05-20 noche**: aplazar la segunda ola **hasta que la web
esté en estado "terminada"** (menos churn de componentes). El criterio de
entrada para la segunda ola es:

1. **Subir el gate del CI** a `min-score: 80` o `85` en
   `.github/workflows/ci.yml` y quitar `continue-on-error`. Convierte
   visibility-only en hard gate y bloquea regresiones.
2. **Refactor mayor de los grandes**: aquí es donde hay rendimiento real,
   no en los micro-quick-wins.
   - `no-giant-component` (15): UsersAdmin, VideoPlayer, AuditLogPanel,
     WhoIsWatching, LogsPanel — split por sub-componentes con
     responsabilidad única.
   - `prefer-useReducer` (22) + `no-cascading-set-state` (7): consolidar.
     **useHls (23 setState en un effect)** es el más urgente — cada
     setState dispara un render durante la carga del stream, un reducer
     único lo colapsa a uno.
3. **`no-array-index-as-key` restantes** (13) cuando el backend exponga
   IDs estables o se generen con `crypto.randomUUID()` al ingest.
4. **`rendering-hydration-mismatch-time` complejos** (15): callbacks de
   Recharts y datos paginated. No hacemos SSR; riesgo real bajo, pero
   limpia.

### Reglas que NUNCA se van a eliminar (falsos positivos legítimos)

Documentar aquí para que ningún PR futuro pierda tiempo:

- `rerender-state-only-in-handlers` (23): **conflicto irreconciliable**
  con `react-hooks/refs`. El patrón "Adjusting state when a prop changes"
  viola esta regla pero satisface las dos de react-hooks (que son
  hard-gates). Ganan los hooks.
- `query-mutation-missing-invalidation` (12): falsos positivos —
  mutations read-only (probe peer, test DB, preflight M3U, deviceAuth) o
  invalidación vía helper indirecto que el lint no detecta (images.ts).
- `no-derived-useState` (6 de 8): casos donde el `useState` representa
  edición local del usuario (CollisionPicker decisiones, ExternalSubsModal
  langs). Derivar en render reiniciaría el trabajo del usuario en cada
  re-render del padre. Suprimidos narrow con justificación.
- `no-pure-black-background` (7): `bg-black` en contenedores de video.
  Cambiar a `bg-bg-base` dejaría borde gris alrededor.
- `label-has-associated-control` (6): asociación implícita (`<label>`
  envolviendo `<input>`) **ES a11y válida**. Falso positivo del lint.
- `async-await-in-loop` (2): stream reader (DatabasePanel SSE) + retry
  loop con backoff (api/client). Secuencial por diseño.
- `async-defer-await` (2): la awaited value SÍ se usa después del
  early-return (ItemDetail onClose, useVibrantColors swatches).
- `no-derived-state-effect` (1): VideoPlayer:628 con `key={itemId}` re-
  montaría hls.js. Documentado in-line con eslint-disable.
- `client-localstorage-no-version` (2) + `js-cache-storage` (1): test
  files. Cambiar la key rompe migración real (production usa
  `hubplay_user`).

### PRs dependabot abiertas (estado)

Las que sobrevivieron a la limpieza de tarde (#247, #289, #352 ya mergeadas, #330 cerrada por redundancia):

| PR | Tema | Estado |
|---|---|---|
| Nuevas dependabot semanales | — | A revisar la próxima vez que se abran (lunes) |

---

## 🏗️ Sesión 2026-05-20 (tarde) — Limpieza profunda: lockfile, React Compiler y quality gates

Sesión larga (~10 PRs en una tarde) iniciada como "revisar PRs dependabot" y derivada hacia un saneamiento general del frontend. Catch importante: **main estaba rojo** en CI / Lint y CI / Test Backend, y casi todas las PRs de dependabot heredaban esos fallos sin que tuviera nada que ver con ellas.

### Cadena de causa → efecto descubierta

1. **CI / Lint roto** en main: 8 issues que aparecieron cuando `golangci/golangci-lint-action@v7` empezó a resolver al binario `v2.5.0` con reglas más estrictas (ineffassign, ST1023, SA9003, ST1019, QF1001, unused). Lo bloqueaba cualquier dependabot trivial (postcss bump, picomatch bump, etc).
2. **CI / Test Backend roto** en main: dos flakes en `internal/iptv` aparecían bajo `-race -coverprofile` cuando el watcher goroutine se preempta más de lo previsto. `TestTransmuxManager_PromotesToReencodeOnCodecCrash` y `TestTransmuxManager_Touch_KeepsSessionAlive`.
3. **Workflow Release roto**: `git describe` se calculaba en dos jobs distintos (`build` y `windows-installer`), y entre ambos `release-nightly` reescribía el tag `nightly`. Resultado: nombres de zip desalineados, `unzip exit 9` rompiendo el instalador Windows.
4. **`pnpm-lock.yaml` corrupto**: `lru-cache@11.5.0` triplicado en `packages:` y `snapshots:` tras mergear 4 PRs de dependabot npm en sucesión rápida en la mañana (#290 → #338 → #248 → #141 en ~30 min). GitHub squash-merge no detecta colisiones semánticas de YAML.
5. **`web/package-lock.json` huérfano** commiteado desde el inicio del repo aunque el proyecto usa pnpm. Nunca se sincronizaba con `pnpm-lock.yaml`.

### Lo que se cerró (mergeado a main esta tarde)

- **[#345](https://github.com/Alexzafra13/HubPlay_demo/pull/345)** — Release workflow: nuevo job `meta` centraliza `git describe` y el resto consume `needs.meta.outputs.*`. Race del tag `nightly` desaparece.
- **[#346](https://github.com/Alexzafra13/HubPlay_demo/pull/346)** — Lint cleanups (8 issues) + deadline interno del flake `PromotesToReencodeOnCodecCrash` 5s → 15s.
- **[#347](https://github.com/Alexzafra13/HubPlay_demo/pull/347)** — `git rm web/package-lock.json` + añadir al `.gitignore` (junto con `yarn.lock`).
- **[#348](https://github.com/Alexzafra13/HubPlay_demo/pull/348)** — Flake `Touch_KeepsSessionAlive`: IdleTimeout 200ms → 500ms, Wait 700ms → 1500ms, ctx 5s → 10s, + Touch sincrónico antes de lanzar la goroutine.
- **[#349](https://github.com/Alexzafra13/HubPlay_demo/pull/349)** — Fix quirúrgico del lockfile corrompido (12 líneas borradas, sin tocar versiones).
- **[#350](https://github.com/Alexzafra13/HubPlay_demo/pull/350)** — `dependabot.yml`: `open-pull-requests-limit` npm 5 → 1 para prevenir futuras colisiones de lockfile.
- **6 PRs de dependabot mergeadas en cadena**: #137 (golangci-lint-action), #138 (setup-qemu), #141 (jsdom 28→29 major), #248 (go-deps group, 7 paquetes), #290 (postcss), #338 (tough-cookie 2→6, transitive lockfile-only). Más #280 (vite dev), #351 (go-deps), #353 (download-artifact) en la última pasada.

### Lo que queda en flight (abierto al cierre de sesión)

- **[#354](https://github.com/Alexzafra13/HubPlay_demo/pull/354) — Activación del React Compiler**. Resultado final: 0 errors de lint, 616/616 vitest, build limpio, healthcheck 542/542 compatibles. Cambios:
  - `babel-plugin-react-compiler@1.0.0` integrado vía `react({ babel: { plugins: [...] } })` en `vite.config.ts`.
  - `eslint-plugin-react-compiler@19.1.0-rc.2` con regla `react-compiler/react-compiler: 'error'`.
  - `eslint-plugin-react-hooks` 7.0.1 → 7.1.1 (las reglas estrictas que destaparon los anti-patterns).
  - **15 anti-patterns refactorizados** + 5 extras que sólo aparecían en CI:
    - **`set-state-in-effect` → render-time guarded setState** (patrón oficial React 19 "Adjusting state when a prop changes"): MainNav (route change), SearchBar (URL ?q=), LinkDevice (URL code), LibraryNewPage (validation reset), UsersAdmin (×2: access draft + libraryIds seed), useVibrantColors (palette swap), PairThisDevice (×2: mount-once + qrSvg reset).
    - **`refs during render`**: useLiveHls (ref-assign movido a useEffect), PlayerControls (`reportMenu` envuelto en useCallback), MainNav (`clearTimers` con disable narrow + justificación porque cancelar el timer es necesario funcionalmente).
    - **`immutability`**: HeroTrailer (`handleDismiss` declarado antes del useEffect que lo usa, envuelto en useCallback).
    - **`exhaustive-deps`**: VideoPlayer (`BURNABLE_CODECS` a module scope), MyNotifications + PeerLibraryItemsPage (derivar dentro del useMemo).
    - **5 fixes extra del CI** (no aparecieron en mi lint local porque mi script de filtrado descartó `react-compiler/*` como derivados, cuando algunos son detecciones primarias): useHls + usePlayerKeyboard (suppress narrow para mutar `HTMLMediaElement.src` / `.currentTime` que es API estándar del DOM, no state); useProgressReporter (añadir `[videoRef, itemId, peerId]` a deps reales del effect del cleanup); PairThisDevice (añadir `[poll, navigate]` a deps SSE); Uploads (patrón "latest value via ref" con useEffect dedicado para actualizar el ref + cleanup que lo lee al desmontar — sin esto, añadir `active` a deps abortaría uploads en cada cambio de estado).
- **[#355](https://github.com/Alexzafra13/HubPlay_demo/pull/355) — Quality gates extra en CI**. Tres nuevos steps:
  - **`pnpm typecheck`** (`tsc -b`) — hard gate. Antes el typecheck sólo corría en `pnpm build`, que CI no ejecuta para frontend.
  - **`pnpm dlx react-compiler-healthcheck`** — hard gate. Falla si la compatibilidad baja del 100%. Funciona independientemente de si el compiler está activado.
  - **`pnpm knip`** con `--no-exit-code` — info-only (job separado). Hoy reporta 5 unused files + 2 unused deps + 38 unused exports + 115 unused types. Cuando lleguemos a 0, elevamos a hard gate.

### Aprendizajes que se aplican a futuras sesiones

- **Antes de "arreglar" una PR de dependabot que falla en lint o test backend, comprobar primero si main está rojo**. La mitad del tiempo el problema no es del bump, es heredado.
- **`open-pull-requests-limit: 1` para npm** es la respuesta correcta a "GitHub squash-merge corrompe lockfiles cuando dos PRs lo tocan en paralelo". El precio (bumps serializados) es barato comparado con `ERR_PNPM_BROKEN_LOCKFILE` en producción.
- **`eslint-plugin-react-compiler` es estricto pero correcto**. Cuando reporta "Component skipped because rules were disabled", suele ser señal de que el `eslint-disable react-hooks/*` esconde un anti-pattern real, no que el plugin sea pedante.
- **No te fíes de tu lint local si filtra reglas**. Cinco anti-patterns aparecieron sólo en CI porque mi script filtraba `react-compiler/*` como "derivados" — pero el compiler también detecta cosas primarias.

---

## 🧹 Sesión 2026-05-20 — Pendientes pequeños + limpieza

Después de cerrar la fase de distribución (sesión 2026-05-19/20), barrido de los pendientes "pequeños y autocontenidos" + housekeeping del repo. Cinco PRs creados, una mergeada en caliente.

### Lo que cambió

- **Opt-out del update checker en runtime** ([#339](https://github.com/Alexzafra13/HubPlay_demo/pull/339)). `Service.SetUserEnabled(bool)` togglable; persistido en `app_settings` con key `updates.check_enabled`. Bootstrap en `main.go` lee el setting antes de arrancar el ticker. Nuevo campo `user_disabled` en el wire de `/admin/system/updates`. Endpoints `GET`/`PUT /admin/system/updates/config`. Banner ahora pinta 4 estados (capability off / user-disabled con "Activar" / has_update / al día con "Comprobar" + "Deshabilitar"). 4 unit + 6 handler + 6 vitest.
- **URL mDNS en `/admin/system`** (#340, **mergeada**). Nuevo campo `mdns_url` (omitempty) en `serverStats`. El router lo computa cuando `cfg.MDNS.Enabled`. Fila condicional en la card "Conexión" del System status con valor mono + `CopyToClipboardButton`. Componente nuevo en `components/common/` (reutilizable, tolerante a HTTP plano sin clipboard API).
- **LICENSE GPL-3.0-or-later** ([#341](https://github.com/Alexzafra13/HubPlay_demo/pull/341)). Texto canónico de gnu.org en root. `LicenseFile=` añadido a `installer.iss` bajo `#if FileExists`. Campo `"license"` en `web/package.json`. Cumple sección 4 de la GPL-3. FFmpeg LGPL mantiene su `LICENSE-ffmpeg.txt` aparte.
- **Fixes cross-platform de `go test ./...`** ([#342](https://github.com/Alexzafra13/HubPlay_demo/pull/342)). Cinco fallos pre-existentes en main que NO eran bugs de producción — eran asumptions de test:
  - `upload/sanitize.go`: `filepath.Base("a:::b.mkv")` en Windows trata `a:` como drive prefix → cambiado a `path.Base` (POSIX).
  - `stream/manager_test.go`: `newTestManager` pasaba `ffmpegPath=""` que cae a PATH lookup; en hosts con ffmpeg `cmd.Start()` arrancaba el proceso y los tests del coalesce veían `nil` error → ffmpeg sentinel inexistente.
  - `config/preflight_test.go`: `stubPathWith` no añadía `.exe` para Windows (`exec.LookPath` requiere extensión en `%PATHEXT%`).
  - `db/sqlc/*models.go`: drift por migraciones `audit_log` + `cors_origins` que añadieron tablas sin regenerar — `make sqlc` regen.
- **`Service.BaseURL` inyectable en updates** ([#343](https://github.com/Alexzafra13/HubPlay_demo/pull/343)). Setter `SetBaseURL(url)` thread-safe. Reemplaza los 2 `t.Skip` con tests E2E reales contra `httptest`: `DetectsNewerVersion`, `SamePinsHasUpdateFalse`, `PrereleaseSkippedSilently`, `RecordsLastErrorOn500`, `HTTPIntegration_ETagRoundTrip` (verifica round-trip de `If-None-Match`, cabeceras `Accept` + `User-Agent`).
- **Limpieza de memoria** (esta sesión). 8 ficheros .md sueltos de sesiones cerradas movidos a `archive/`. `project-status.md` reescrito (de 441 KB a manejable) con las sesiones viejas en `archive/2026-04-29-to-05-04.md` y `archive/2026-05-05-to-05-19.md`.

### Métricas al cierre

- Backend: `go test ./...` verde en mi máquina (Windows + ffmpeg en PATH + sqlc 1.31.1).
- Frontend: 622/622 vitest (+18 sobre la sesión anterior: 4 service + 6 handler updates + 6 banner + 3 CopyToClipboardButton, menos 1 skip placeholder).
- TypeScript: `tsc -b` limpio.
- Producción: build limpio.

---

## 📦 Sesión 2026-05-19/20 (continuación) — Distribución y empaquetado

Cerramos el flujo "descargar y usar" para tres públicos: PC desktop, servidor Linux y NAS. 8 PRs mergeadas. Nada toca la lógica de negocio — todo es plumbing alrededor del binario.

**Audit panel — usernames en vez de UUIDs.** LEFT JOIN a users en el SELECT del audit log. Pinta `alice` y `bob → carol` en lugar de `bd91…/4def…`; UUID truncado en gris debajo, tooltip con el ID completo para copiar.

**Versión inyectable.** `version`, `commit`, `buildDate` como `var` en main package, inyectadas por `-ldflags` en Makefile y CI vía `git describe`. Se exponen en `/admin/system/stats` y en `hubplay --version`. Builds locales muestran `v0.1.0-N-gSHA-dirty`, build de tag muestra el tag limpio.

**PATH prepend del exe-dir al arranque.** Una línea en `main.go` que mete `filepath.Dir(os.Executable())` al inicio del `$PATH`. Con eso, cualquier `exec.LookPath("ffmpeg")` o `exec.Command("ffmpeg", …)` (probe, stream, imaging, iptv, library) encuentra el `ffmpeg` bundleado en la misma carpeta que `hubplay.exe` sin tocar ningún call-site.

**Release workflow cross-platform** (`.github/workflows/release.yml`):
- Matrix 5 targets: linux/darwin × amd64/arm64, windows × amd64. `CGO_ENABLED=0` (modernc.org/sqlite es pure-Go).
- `scripts/fetch-ffmpeg.sh` descarga ffmpeg LGPL por plataforma — BtbN/FFmpeg-Builds (Linux/Windows), evermeet.cx (macOS).
- Empaqueta `.tar.gz`/`.zip` + `.sha256` con `hubplay` + `ffmpeg` + `ffprobe` + `hubplay.example.yaml` + `LICENSE-ffmpeg.txt`.
- Job `release-nightly`: en cada push a `main` borra el tag `nightly` y lo recrea con los binarios del último commit. Pre-release público, visible en la pestaña Releases sin esperar a taguear.
- Job `release-tag`: en push de `v*` publica release ESTABLE (no draft, no prerelease).
- `fetch-depth: 0` en todos los checkouts que llaman a `git describe` — sin esto el job downstream calculaba una versión distinta y el unzip fallaba.

**Installer Windows** (`scripts/installer.iss` + `Minionguyjpro/Inno-Setup-Action@v1.2.8`):
- Genera `HubPlay-Setup-vXXX-windows-amd64.exe`.
- Bundlea NSSM (BSD) para registrar `HubPlay` como servicio Windows; arranca con el PC, sin consola.
- Icono multi-resolución (16/32/48/64/128/256), banner del wizard con marca integrada — todo generado desde `web/public/hubplay_icon_mark.svg` con `web/scripts/gen-installer-assets.mjs` (sharp + to-ico).
- Shortcut Escritorio + Menú Inicio apunta a `launch-hubplay.vbs` que intenta `msedge --app`, luego `chrome --app`, fallback al navegador por defecto → abre como ventana standalone sin chrome del browser.
- Textos del wizard en español sobrescritos en `[Messages]`, sin tono de marketing.
- Caveats: pin `@v1.2.8`, `fetch-depth: 0`, NSSM con retry+fallback a archive.org (nssm.cc da 503), env vars en lugar de `/D`. El `LICENSE` ya no es opcional (PR #341).

**Install script Linux** (`scripts/install.sh` + `scripts/hubplay.service`):
- One-liner estilo Tailscale/k3s: `curl -fsSL https://github.com/Alexzafra13/HubPlay_demo/releases/latest/download/install.sh | sudo bash`.
- Detecta arch (amd64/arm64), exige systemd, refusa con instrucciones a Docker en Synology/Unraid/QNAP/TrueNAS/Alpine.
- Resuelve "latest" vía GitHub API, descarga `.tar.gz`, verifica `sha256`, crea usuario sistema `hubplay`, instala binarios en `/usr/local/bin/`, config en `/etc/hubplay/`, datos en `/var/lib/hubplay/`, registra systemd unit endurecido, `enable --now`.
- Idempotente: correrlo dos veces hace upgrade in-place. Preserva el `hubplay.yaml` editado por el operador.

**PWA** (`vite-plugin-pwa` + `@vite-pwa/assets-generator`):
- Manifest + service worker injectados automáticamente. Workbox precachea JS/CSS/assets, no HTML (evita stale tras swap de server).
- Iconos generados desde `public/hubplay_icon_mark.svg` con `pnpm gen:pwa-assets`.
- Browser detecta "instalable" en `localhost` o HTTPS → botón "Instalar" → icono propio en escritorio/home + ventana standalone.
- **Caveat conocido**: en LAN sobre HTTP plano (192.168.x.y) los browsers NO ofrecen instalar la PWA — requieren secure context.

**Update notifier** (`internal/updates/`):
- Goroutine background con ticker de 24h y jitter inicial 0-30min.
- GET a `api.github.com/repos/.../releases/latest` con `If-None-Match` (ETag) → 304 cuando no hay versión nueva, ~200B por check.
- Auto-deshabilitado en `version=="dev"` o `repo==""`.
- Endpoints: `GET /admin/system/updates`, `POST /admin/system/updates/check` (rate-limit 1/min), `GET`/`PUT /admin/system/updates/config` (opt-out runtime — sesión 2026-05-20).
- Banner en `/admin/system` (`UpdateBanner.tsx`): 4 estados (ver sesión 2026-05-20 arriba).

**mDNS auto-anuncio** (`internal/mdns/` con `grandcat/zeroconf`):
- El server registra `_http._tcp` con hostname forzado `<cfg.MDNS.Hostname>.local` (default `hubplay.local`).
- Cualquier dispositivo de la LAN resuelve `http://hubplay.local:8096` sin tocar router ni DNS.
- Config `mdns.enabled` (default true) y `mdns.hostname` en `hubplay.yaml`.
- La URL se expone en `/admin/system/stats` (sesión 2026-05-20, PR #340) con botón copiar.

**PRs mergeadas en esta sesión:** #327 → #328 → #329 → #331 → #332 → #333 → #334 → #335-#337.

---

## 🎯 Cola priorizada para la próxima sesión

### Cerrar React Doctor: split de VideoPlayer
- VideoPlayer.tsx (1003 LoC, 12 useState, useEffect con 9 setState) tiene 4-5 reglas react-doctor pendientes que sólo se resuelven dividiendo el componente. Spec en la sesión de hoy (arriba). **Requiere preview con backend corriendo** para verificar playback / seek / quality switching / next-up tras el split. Plan tentativo: extraer `<PlaybackControls />`, `<TrickPlayBar />`, `<NextUpOverlay />`; consolidar state con `useReducer` agrupado por dominio (playback, network, ui). HeroSection y SeriesHero comparten reglas pero son más manejables — atacar después.

### Medianos (vale la pena cuando arranque la siguiente sesión)

1. **Tests frontend de páginas grandes**. Coverage Vitest cubre admin panels + componentes comunes; faltan **Home**, **LiveTV**, **Search**, **Movies**, **Series**. Son las páginas con más LOC del repo y las más visibles para el usuario.
2. **Refactor estructural pendiente** del audit 2026-05-14 ([audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md) + [intervention-2026-05-14.md](intervention-2026-05-14.md)). Iteraciones 4-7 abiertas: split de god-handlers/services (P, Z, QQ), refactor estructural de `iptv/` (CC), composition root (G, H, V, Q, LL, JJ), schema + cosmética (D, X, W, BB, UUU-mig).
3. **Per-user channel order + hide en Live TV**. Spec en [per-user-channel-order-pending.md](per-user-channel-order-pending.md). Migración + servicio + API + UI. ~1 sesión.

### Grandes (requieren ventana dedicada)

4. **Firma del installer Windows con SignPath Foundation**. **Es gratis para OSS** (verificado en signpath.org el 2026-05-20). Apply via `apply.signpath.io` — la verificación tarda días/semanas pero la integración con el workflow es directa (action `signpath/github-action-submit-signing-request`). Mientras llega el approval, SmartScreen sigue avisando — no urgente.
5. **Auto-update one-click + cert TLS en LAN**. Estilo `*.plex.direct`: el server obtiene un cert real para `<hash>.hubplay.direct` o similar, lo sirve en LAN sin warnings, y el client comprueba el feed de updates y aplica binarios firmados in-place. Feature grande, sin presión de calendario.

---

## 📚 Documentos vivos en `docs/memory/`

- **[architecture-decisions.md](architecture-decisions.md)** — ADRs cerrados (AppError, observability, keystore, sink pattern, preflight, sqlc adapter, etc.). Sólo se añaden ADRs nuevos; nunca se edita un ADR cerrado.
- **[conventions.md](conventions.md)** — patrones del codebase (anti-ciclos, sqlc adapter, helpers de test, gotchas, reglas de dependencia entre paquetes).
- **[audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md)** — review vivo por fases. Iteraciones 4-7 pendientes (ver intervention).
- **[intervention-2026-05-14.md](intervention-2026-05-14.md)** — tracker de iteración del review 2026-05-14. Marca olores cerrados por commit.
- **[perf-benchmarks-2026-05-17.md](perf-benchmarks-2026-05-17.md)** — baseline benchmarks dual-backend (SQLite + Postgres) para repos del hot-path.
- **[per-user-channel-order-pending.md](per-user-channel-order-pending.md)** — spec de feature pendiente Live TV.

## 🗄️ Archivo (`docs/memory/archive/`)

Sesiones cerradas, conservadas para arqueología:

- `2026-pre-04-28.md` — orígenes del proyecto.
- `2026-04-27-to-04-29-pre-detail-ux.md` — sesiones pre-detail UX.
- `2026-04-29-to-05-04.md` — federación P2P, OpenAPI, hardening federation.
- `2026-05-05-to-05-19.md` — senior reviews intermedios, fixes player/seek, mDNS bringup, uploads + permisos granulares.

Audits cerrados + sesiones específicas:
- `audit-2026-04-15.md`, `audit-2026-04-28.md`, `audit-2026-05-05.md`, `audit-plan.md`
- `manual-qa-movies-series-2026-04-27.md`
- `player-seek-bugs-2026-05-07.md` (los bugs ya están arreglados en main)
- `session_2026-05-10_audit_p0_fixes.md`
- `topbar-redesign-2026-05-06.md`
