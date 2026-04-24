# Estado del proyecto

> Snapshot: **2026-04-24** (matcher EPG + scheduler IPTV + review fixes + continuar viendo) · Rama: `claude/review-pending-tasks-9Vh6U` · **tests: verde · lint: 0**

---

## 👉 HANDOFF PARA LA PRÓXIMA SESIÓN

> **Lee esto primero.** Resume qué cerramos, qué decidimos y qué toca.

### Lo que cerramos esta rama (`claude/review-pending-tasks-9Vh6U`)

Cuatro commits: las dos candidatas del handoff anterior + pass de
fixes tras review senior + "Continuar viendo" en LiveTV.

**Commit 1 — Matcher EPG agresivo**. Sin cambios de schema, sin UI,
solo backend.

Cambios en el algoritmo del matcher (antes: tvg-id exacto + name-
variants con quality strip + accent fold):

1. **Alias table curada** (`internal/iptv/epg_aliases.go`): 50+ entradas
   normalizadas observadas en davidmuma vs iptv-org. Maps `"la uno" →
   "la 1"`, `"tele cinco" → "telecinco"`, `"antena tres" → "antena 3"`,
   `"movistar laliga" → "movistar la liga"`, etc. Aplicada
   bidireccionalmente: al indexar hub channels se registra tanto la
   variante original como su canonical, y al emparejar se alias-folds
   cada display-name del XMLTV antes del lookup.
2. **Channel-number match** — si el XMLTV trae un `<display-name>` que
   es un entero y la M3U carga números de canal reales (no los
   posicionales que pone `assignNumber` por defecto), bind por número.
   Detecta posicionales con la heurística `channel.Number == i+1 ∀ i`
   y salta limpiamente en ese caso. Además ignora números ambiguos
   (mismo dial en dos canales).
3. **Fuzzy Levenshtein** como último recurso — edit distance ≤ 3
   absoluto y ≤ 15 % del string más largo, mínimo 5 runas. Pool = solo
   la variante base (stripped) de cada canal. Empates entre dos
   canales a la misma distancia **no bindean** (fail-closed). Cubre
   typos ("Teleciinco"), elisiones ("Disovery Chanel").

Además (refactor limpieza, sin cambio de comportamiento):

4. **Extracción del matcher** a `internal/iptv/matcher.go`. `service_epg.go`
   queda 90 líneas más corto y solo orquesta. El struct `channelIndex`
   reemplaza los `(tvgMap, nameMap)` sueltos — ahora contiene también
   el numberMap y el fuzzy pool.
5. **Cache de resolución por XMLTV channel id** en `refreshOneSource`:
   antes cada uno de los ~7 k programas llamaba a `matchChannel`.
   Ahora se resuelve una vez por `<channel>` y el loop de programas es
   un map lookup. Para feeds grandes (davidmuma trae ~300 canales, 7 k
   programas) es ~300× menos trabajo del matcher.

**Riesgos gestionados**:
- Aliases solo se añaden cuando hemos visto el mismatch real contra
  una fuente en uso. No añadir "por si acaso" — cada alias es una
  oportunidad de falso match si otro canal aparece en el futuro.
- Channel-number se desactiva en playlists posicionales (caso por
  defecto en iptv-org sin `tvg-chno`). Solo ve dial reales.
- Fuzzy exige 5+ runas y ≤15 % distancia. Testeado: "Cuatro" vs
  "Telecinco" NO matchea, "Teleciinco" (1 edit) SÍ, tie a dos canales
  devuelve empty.

**Commit 2 — Scheduler IPTV con UI admin**. Saca al producto de
"herramienta manual" con botón Refrescar a servicio que se
autosostiene.

Backend:

1. **Migración 011**: tabla `iptv_scheduled_jobs(library_id, kind,
   interval_hours, enabled, last_run_at, last_status, last_error,
   last_duration_ms, created_at, updated_at)` con PK compuesta
   (library_id, kind) y CHECK de kind ∈ {m3u_refresh, epg_refresh}.
   Índice parcial sobre `last_run_at WHERE enabled=1` para la query
   del worker.
2. **`db.IPTVScheduleRepository`** (raw SQL pattern): `ListByLibrary`
   / `Get` / `Upsert` / `ListDue` / `RecordRun` / `Delete`.
   - `Upsert` es `ON CONFLICT DO UPDATE` sobre interval_hours +
     enabled + updated_at — preserva los last_* para que reconfigurar
     no resetee el histórico.
   - `ListDue` filtra por `enabled=1` en SQL, calcula due-ness en Go
     (multi-format SQLite time coercion). Never-run rows (last_run_at
     NULL) son siempre due en el próximo tick.
   - `RecordRun` trunca last_error a 512 chars para que un stack
     trace upstream no infle la tabla.
3. **`iptv.Scheduler`** worker en su propio goroutine. Tick = 1 min
   (responsivo al admin sin spam). runTimeout = 10 min por job.
   Ejecuta secuencial (serializa intra-library vía el lock del
   service y evita burst al CDN). Interface `jobRunner` inyectada →
   tests con fake sin red.
4. **Wire en `main.go`**: Phase 4d nuevo (después de IPTV service,
   antes de Setup). Shutdown llama `iptvSched.Stop()` antes de
   `iptvSvc.Shutdown()` para que el último run grabe su outcome antes
   de cerrar la DB.
5. **HTTP handlers** en `internal/api/handlers/iptv_schedule.go`:
   - `GET /libraries/{id}/schedule` — lista las dos filas (M3U/EPG);
     sintetiza placeholders `{enabled:false, interval:default}` para
     kinds sin fila → la UI siempre pinta 2 filas.
   - `PUT /libraries/{id}/schedule/{kind}` admin-only — valida
     `interval_hours ∈ [1, 720]`; `enabled` pointer-like (omitir =
     conservar actual) → el UI puede guardar solo el intervalo sin
     cambiar el toggle.
   - `DELETE /libraries/{id}/schedule/{kind}` admin-only.
   - `POST /libraries/{id}/schedule/{kind}/run` admin-only — dispara
     síncrono vía `Scheduler.RunNow` que **bypasea** enabled + due
     (el botón funciona aunque la programación esté apagada).
6. **ACL**: mismo patrón que los demás endpoints livetv
   (library access → 404, admin bypasa).

Frontend:

7. **Tipos + client + hooks** (`web/src/api/{types,client,hooks}.ts`):
   `IPTVScheduledJob`, `UpsertScheduledJobRequest`, métodos
   `listScheduledJobs` / `upsertScheduledJob` / `deleteScheduledJob`
   / `runScheduledJobNow`, y hooks React Query
   `useScheduledJobs` / `useUpsertScheduledJob` /
   `useDeleteScheduledJob` / `useRunScheduledJobNow` con invalidación
   que refleja las mismas keys que `useRefreshM3U`/`useRefreshEPG`
   — tras Run now la UI de canales/EPG se refresca sola.
8. **`ScheduledJobsPanel`** nuevo en `web/src/components/admin/`.
   Dos filas, cada una con toggle + dropdown (1/3/6/12/24/72/168 h)
   + "Ejecutar ahora" + badge de status + línea "hace 3 h"
   relativa. Sin estado local espurio — el dropdown lee
   `job.interval_hours` directamente para respetar la regla
   senior "no set-state-in-effect con Compiler" (el primer draft
   tenía `useEffect → setState` para mirror optimista; lint lo
   pilló, refactor a source-of-truth directa).
9. **Tab nueva "Programación"** en `LivetvAdminPanel` entre
   "Fuentes EPG" y "Sin guía". Siempre visible (el endpoint sinteti-
   za filas). Badge count = nº jobs enabled; tone=warning si alguno
   tiene `last_status === "error"`.

Tests nuevos (todos verdes):

- `internal/db/iptv_schedule_test.go` — 9 tests (upsert crea,
  preserva last_*, get sentinel, list by library, list due: drops
  disabled / respects interval / incluye never-run, rechazo de
  interval inválido, trim de last_error > 512, delete idempotente).
- `internal/iptv/scheduler_test.go` — 8 tests con fakeRunner
  (tick ejecuta due / salta disabled / salta not-yet-due, record
  failure, RunNow bypasea schedule + row-less, surface error,
  Start/Stop corre loop, Stop es síncrono).
- `internal/api/handlers/iptv_schedule_test.go` — 13 tests con
  fakeScheduleRepo + fakeScheduleRunner (list sintetiza missing
  kinds, deny sin access, upsert crea, invalid kind, out-of-range
  interval, keeps enabled cuando se omite, unknown field rechaza,
  delete happy, run-now happy, run-now surface error, run-now
  sin row devuelve 204, run-now deny).

**Commit 3 — Fixes de review senior**. Un sub-agente revisó todo con
ojos frescos. 1 blocker + 7 should-fix + 4 nits arreglados. Sin cambios
de comportamiento visibles salvo el path de recuperación de panic.

Blocker arreglado:

- **Scheduler goroutine no muere silenciosamente en panic**
  (`scheduler.go:runOne`). `defer/recover` convierte el panic en un
  outcome "error" registrado y mantiene el goroutine vivo para el
  siguiente tick. Antes: un panic en el XMLTV parser paraba TODOS los
  refreshes programados hasta reiniciar el binario, sin log visible.
  Cubierto por `TestScheduler_RunNowRecoversFromPanic` +
  `TestScheduler_TickLoopSurvivesPanic`.

Should-fix arreglados:

- **`Stop(ctx)` respeta el deadline del caller**. `Start` envuelve el
  ctx con un cancel propio; `Stop` recibe el `shutdownCtx` de main.go
  y, si éste expira antes de que drene el run in-flight, fuerza
  cancel del runCtx. Antes: `Stop()` podía bloquear hasta 10 min
  (runTimeout) aunque el shutdownCtx fuera de 30 s — el supervisor
  mandaba SIGKILL antes de drenar.
- **`iptv.ErrRefreshInProgress` sentinel** en lugar de `fmt.Errorf`
  opaco. `RefreshM3U` / `RefreshEPG` lo wrappean con `%w`. El
  scheduler lo detecta con `errors.Is` y lo trata como benigno —
  log info, NO actualiza `last_status`. Sin esto, una race entre
  "Ejecutar ahora" y el tick grababa un `last_status="error"`
  spurious sobre un refresh que en realidad había funcionado en el
  otro path. Cubierto por `TestScheduler_ConcurrentRefreshIsBenign`.
- **`canAccessLibrary` compartido** (`iptv_access.go` nuevo). Los dos
  handlers (`IPTVHandler.canAccessLibrary`, `IPTVScheduleHandler
  .canAccess`) delegan al helper de paquete. Antes eran dos copias
  idénticas que podían drift-ear en wording o semántica.
- **Fuzzy pruner byte-vs-rune**. `absDiff(len(cand), len(pool))`
  comparaba longitudes en BYTES contra un budget rune-based; para
  strings que sobreviven diacritic folding con chars multi-byte
  ("Movistar Plus+", "3/24") el pruner podía saltarse matches
  legítimos. Ahora `runeCount()` helper en todas las comparaciones.
  Cubierto por nuevo caso "non-ASCII survives folding".
- **CASCADE de `iptv_scheduled_jobs` pin-eado por test**
  (`TestIPTVSchedule_CascadesOnLibraryDelete`). Borrar una biblioteca
  debe limpiar sus schedule rows. Depende de `ON DELETE CASCADE` en
  la migración **Y** de `PRAGMA foreign_keys=ON` en el DSN del
  driver — ambas cosas pueden regresionar silenciosamente.
- **Tab "Programación" no parpadea al cargar** (`LivetvAdminPanel
  .tsx`). Antes `showSchedule={schedule.length > 0}` la hacía
  aparecer tarde porque `schedule` arranca como `[]`. Ahora
  `showSchedule={true}` — el backend sintetiza placeholders así que
  la tab siempre tiene contenido estable.

Nits liquidados:

- `bestCand` muerto fuera de `fuzzyMatch`.
- `var _ = errors.Is` dead-anchor reemplazado por un test real
  (`TestFakeRepo_GetMissingReturnsSentinel`).
- `tickOnce` + `setTickInterval` movidos a `export_test.go` (Go solo
  los compila durante `go test`, invisibles al binario).
- Comentario de coste O(N × fuzzy) en el path de programmes huérfanas
  de `refreshOneSource`.

Tests nuevos de este commit (6 casos): `RunNowRecoversFromPanic`,
`TickLoopSurvivesPanic`, `ConcurrentRefreshIsBenign`,
`CascadesOnLibraryDelete`, "non-ASCII survives folding",
`FakeRepo_GetMissingReturnsSentinel`.

**Commit 4 — "Continuar viendo" en LiveTV**. Feature completa
(schema + backend + frontend + beacon + rail), 18 tests nuevos.

Decisión clave de schema:

1. **Migración 012 `channel_watch_history(user_id, stream_url,
   last_watched_at)`** con PK compuesta. Se keyea por **stream_url**,
   NO por channel_id, porque los channel UUIDs se regeneran en cada
   M3U refresh (lección aprendida de `channel_overrides`, migración
   009). Sin esto el rail se vaciaría cada mañana tras el refresh
   programado. El JOIN del read path resuelve por stream_url contra
   la tabla `channels` actual → entradas sobreviven refreshes y,
   bonus, orphans (URL retirada del playlist) reaparecen si la URL
   vuelve más tarde. Índice dedicado sobre
   `(user_id, last_watched_at DESC)` para el ORDER BY del rail.

Backend:

2. **`db.ChannelWatchHistoryRepository`** (raw SQL):
   `RecordByStreamURL` (upsert), `ListChannelsByUser` (JOIN con
   is_active=1 + dedupe stream_url entre libraries), `DeleteByStreamURL`.
3. **`iptv.Service`**: `RecordWatch(userID, channelID)` busca el
   stream_url y upserta; `ListContinueWatching(userID, limit,
   accessibleLibraries map[string]bool)` con filtro ACL (admin pasa
   `nil`, usuario pasa su set de libraries).
4. **HTTP endpoints** en `IPTVHandler`:
   - `POST /channels/{id}/watch` — beacon del player. Requiere ACL
     (verifica que el usuario puede ver la biblioteca del canal para
     evitar leak de existencia). Devuelve `{channel_id, last_watched_at}`.
   - `GET /me/channels/continue-watching?limit=N` — rail. Límite
     default 10, cap 20. Admin bypasa ACL; usuario filtra vía
     `libraries.ListForUser`.
5. Nueva entrada en la interface `LibraryRepository` del paquete
   handlers (`ListForUser`) para materializar el access set.

Frontend:

6. **Beacon en `useLiveHls`**: nueva prop opcional `onFirstPlay`.
   Se dispara exactamente UNA vez por streamUrl cuando el primer
   frame juega (flag `beaconFired` local al effect). Pause+resume
   NO re-disparan. **No afecta a `StreamPreview`** que usa hls.js
   directo sin el hook → el hover preview no contamina el historial.
   `onFirstPlay` se pasa vía ref para no tear-down el HLS en
   re-renders del caller.
7. **`ChannelPlayer`** llama al beacon vía
   `useRecordChannelWatch().mutate(channelId)` con `onError` que
   loga a consola y sigue — fallos del beacon son no-fatales.
8. **Rail "Continuar viendo"** en `DiscoverView` entre los chips y
   los rails de categoría. Solo aparece en `category === "all"`
   (scoping a una categoría rompería el filtro del usuario) y solo
   si `continueWatching.length > 0`.
9. **React Query**: `queryKeys.continueWatchingChannels` nuevo,
   `useContinueWatchingChannels(limit)` con `staleTime: 60s`,
   `useRecordChannelWatch()` invalida la key tras éxito (el canal
   salta al top del rail sin recargar).

Tests nuevos (todos verdes, 18 casos):

- `internal/db/channel_watch_history_test.go` — 9 tests (upsert
  idempotente, orden por recency, respeto de limit, filtro
  is_active=1, limit=0 → vacío, aislamiento por usuario, **survives
  M3U refresh** (contrato principal: re-joins tras ReplaceForLibrary
  y orphan re-aparece cuando URL vuelve), cascade on user delete,
  dedupe cross-libraries, delete idempotente).
- `internal/api/handlers/iptv_watch_test.go` — 6 tests
  (beacon happy path, 401 sin auth, 404 deny por ACL, 404 race
  post-delete, list happy, list filtra por access, admin salta
  filtro, cap de limit 20, default limit 10, 401 en list).
- `web/src/components/livetv/DiscoverView.test.tsx` — 3 tests
  (rail aparece encima en category='all', oculto en categoría
  específica, oculto cuando vacío).

**Impacto UX**: el usuario abre LiveTV → ve encima de las
categorías un rail con los N canales que ha visto últimamente
(cualquier dispositivo, gracias a que el historial vive en DB).
Cambia de canal y el siguiente aparece al principio del rail sin
recarga. Sobrevive M3U refreshes programados (sin el fix de
stream_url el rail moriría cada día).

### 📊 Medición pendiente (matcher EPG)

El handoff anterior puso un target **52 → 150-200+** canales con EPG
sobre davidmuma + iptv-org (268 canales ES). No se puede medir sin
ejecutar contra la DB real; lo mide el operador en la siguiente
sesión refrescando EPG desde admin. Los tests de regresión verifican
los tres paths nuevos (aliases, number, fuzzy) con casos extraídos de
los mismos mismatches observados.

### Cómo extender el matcher más tarde

- **Añadir aliases**: solo desde pares observados. Cada entrada en
  `epg_aliases.go` tiene un comentario implícito — "visto en davidmuma
  vs iptv-org 2026-04". Si un alias se añade "por si acaso" y luego
  causa un falso positivo, difícil de desambiguar.
- **Promover a tabla DB**: si aparece demanda de aliases per-library o
  edición desde admin, la shape ya está (alias → canonical). Crear
  migración `011_epg_name_aliases.sql` con dos columnas + PK
  compuesta, y cargarlos en `channelIndex` igual que los del código.
- **Fuzzy más agresivo**: subir el umbral a 20 % es la primera palanca
  (ya probamos 15 % aquí, conservador). Añadir tokenización + drop de
  un set de stop-tokens ("canal", "tv", "channel") es el siguiente.

### Estado al abrir sesión

- `main` tiene PRs #81–#85 mergeados.
- Rama actual `claude/review-pending-tasks-9Vh6U` con **4 commits**
  encima de main (matcher + scheduler + review-fixes + continuar-
  viendo). Listo para PR una vez validado en DB real.
- Tests verdes: `pnpm test` **224/224** · `pnpm tsc --noEmit` · `pnpm
  lint` 0 · `pnpm build` ok · `go test -race ./...` 21 paquetes ·
  `golangci-lint v1.64.8` exit 0.

### Checklist al abrir siguiente sesión

- [ ] Refrescar EPG contra davidmuma y medir canales con EPG antes/
  después (52 era el baseline; target 150+).
- [ ] Activar una programación (ej. EPG cada 6 h) en admin y dejarla
  correr 6 h+ para verificar que el worker dispara y registra.
- [ ] Abrir reproducciones de canal y verificar que el rail
  "Continuar viendo" aparece en Discover con los canales vistos
  (en varios navegadores / dispositivos para validar la parte
  cross-device).
- [ ] Forzar un M3U refresh y verificar que el rail sigue poblado
  (no se vacía — contrato clave del stream_url-as-key).
- [ ] Si todo funciona → abrir PR y mergear.
- [ ] Siguiente candidata: **modal de detalle de programa** al
  clicar en el EPG grid, **streaming parser XMLTV** (bomba memoria
  con feeds 2 GB), o **split de `library.Service`** (deuda
  estructural — ya hay precedente con `iptv.Service`).
- [ ] Actualizar esta memoria.

### 🎓 Patrones senior reforzados en este ciclo

Para el siguiente arquitecto, reglas aprendidas en este pass de
revisión. Añadir a `conventions.md` si reinciden:

1. **Goroutines de fondo SIEMPRE con `defer recover()`** en el nivel
   donde llaman a código third-party-ish (HTTP clients, parsers, SQL
   contra datos no-trust). Sin ello un panic mata el worker para
   siempre sin log visible. El patrón aquí: `defer func() { if r :=
   recover(); r != nil { runErr = fmt.Errorf("panic: %v", r); log...
   }; recordOutcome() }()`.
2. **`Stop()` debe aceptar `context.Context`** cuando lleva estado
   in-flight que puede bloquear. La alternativa ("espera hasta el
   runTimeout interno") rompe el contrato de graceful shutdown: el
   supervisor mata el proceso antes de drenar. El patrón: `Start`
   wraps ctx con un cancel, `Stop(ctx)` hace select entre doneCh y
   ctx.Done() + cancela el root si necesario.
3. **Sentinels de error sobre fmt.Errorf** cuando el caller puede
   querer discriminar. `ErrRefreshInProgress` aquí es el ejemplo:
   sin él, el scheduler no podía saber si un error era benigno o
   real. Pattern: `var ErrFoo = errors.New(...)` + `fmt.Errorf("ctx:
   %w", ErrFoo)` + `errors.Is(err, ErrFoo)` en el caller.
4. **Len() en strings con multi-byte**: si la función trabaja con
   rune-based thresholds (distancias de edición, límites de
   caracteres, etc.), TODAS las comparaciones de longitud deben ir
   en runas. Mezclar `len(s)` (bytes) con `len([]rune(s))` (runes)
   es un bug silencioso en ASCII, visible solo cuando aparece un
   "+" o "/". Usar un `runeCount` local helper para que sea obvio.
5. **Test hooks van en `export_test.go`**. Go solo compila archivos
   `_test.go` durante `go test`, así que un método `TestOnly*`
   exportado ahí es invisible al binario de producción. Evita
   dudar "¿puedo llamar a esto desde fuera?" cuando lees el código.
6. **CASCADE del schema debe tener su propio test**. No asumir que
   `ON DELETE CASCADE` funciona — depende del driver Y de
   `PRAGMA foreign_keys=ON` Y del DSN. modernc.org/sqlite los respeta
   cuando están pragma-ON, pero cualquier cambio en
   `internal/db/sqlite.go` puede apagarlo sin test lo pille.

---

## Ciclo anterior (`claude/review-project-tasks-f5Rdg`)

Cinco commits limpios, sin cambios de comportamiento:

1. **Setup wizard tests**: +48 tests sobre la ruta crítica de primer
   arranque (`SetupWizard`, `AccountStep`, `LibrariesStep`,
   `SettingsStep`, `CompleteStep`). Antes 0 cobertura.
2. **LiveTV coverage slice 2**: +35 tests sobre los 4 componentes que
   quedaron fuera del slice 1 (`LiveTvTopBar`, `DiscoverView`,
   `HeroSpotlight`, `EPGGrid`).
3. **Lint debt cero**: de **14 problemas → 0**. 7 ficheros tocados,
   todos refactors a patrones canónicos React 19 + Compiler
   (`useSyncExternalStore`, `useMemo` para derivación, reset durante
   render, drop manual `useCallback`/`useMemo` cuando el compiler
   skipeaba el memo). Ver sección "Lint debt cero" abajo.
4. **`web/dist/` fuera de git**: sentinel `.gitkeep` + `.gitignore` +
   plugin Vite `preserveGitkeep` en `closeBundle` + Makefile `clean`
   que preserva el sentinel. `go:embed all:web/dist` sigue
   compilando desde fresh clone.

**Verificación final**: Vitest **138 → 221** tests (+83). `pnpm tsc
--noEmit` · `pnpm build` · `pnpm lint` · `go test -race ./...` los
cuatro en verde.

### 🎯 Decisión senior asentada — léela antes de planificar

**No más tests sobre superficies que van a cambiar.** El producto no
está terminado. Tests sobre admin panels que van a crecer es deuda
garantizada — se reescriben cuando añades scheduler, dashboard, etc.

**La regla**: test lo que ya no va a cambiar. Los tests que hemos
metido (player hooks, wizard, livetv components refactorizados) son
estables. Los admin panels NO: les falta scheduler, dashboard,
logs. Esos se testan cuando el producto pare de moverse.

### 📊 Análisis de gap vs Plex / Jellyfin (foco LiveTV)

**Lo que YA tiene HubPlay, sólido**:
- M3U source + refresh manual
- EPG multi-fuente con catálogo curado (prioridad + merge)
- Health-check oportunista (canales apagados auto-ocultos)
- Override manual de tvg-id persistente entre refreshes
- Panel admin unificado con status dot + stats strip + tabs
  (`LivetvAdminPanel` + 4 sub-paneles)
- Hero personalizable por cuenta con spotlight auto-preview HLS
- Discover con rails + categorías + rail "Apagados"
- EPG grid tipo Plex (24h ruler, now-line, auto-scroll one-shot)
- Favoritos + per-user preferences persistentes

**Lo que FALTA, por impacto real**:

1. **Matcher EPG más agresivo** ← el problema más visible del producto
   - Cobertura hoy: **52/268 canales** con davidmuma. El usuario ve
     "sin guía" en 4 de cada 5 canales. Es lo que más se nota en los
     primeros 30 segundos de uso.
   - Hoy: tvg-id exacto + display-name variants + quality strip +
     accent fold (en `internal/iptv/service_epg.go`: `nameVariants`,
     `matchChannel`, `qualityRE`).
   - Añadir: (a) tabla de alias conocidos (`epg_name_aliases`: `alias
     → canonical_name`), (b) match por channel number cuando ambos
     feeds lo traen, (c) Levenshtein fuzzy con threshold configurable.
   - Target: **52 → 150-200+ canales** con EPG.
   - Scope: ~1 día. Solo backend, sin tocar UI, sin churn de tests
     frontend.

2. **Scheduler UI M3U + EPG refresh** ← lo que separa herramienta de servicio
   - Hoy todo es botón manual ("Refrescar" en admin). Sin esto el
     producto no se autosostiene — un usuario real no abre admin
     cada mañana.
   - Tabla nueva `scheduled_jobs(library_id, kind, interval_hours,
     last_run_at, last_status, enabled)`.
   - Worker: goroutine periódica en `library.Scheduler` (ya existe
     para auto-scan, reutilizar el patrón). Ver
     `internal/library/scheduler.go` para la forma.
   - UI: panel admin con dropdown interval + timestamp última
     ejecución + botón "Run now".
   - Scope: ~1 día.

3. **"Continuar viendo" en LiveTV**
   - Requiere tabla `channel_watch_history(user_id, channel_id,
     last_watched_at, seconds_watched)`.
   - Rail nuevo en Discover encima de las categorías.
   - Fase 2 del plan Plex-max. No bloqueante.

4. **Activity / sessions dashboard** (universal, no solo LiveTV)
   - "Quién está viendo qué ahora". Plex Dashboard.
   - Backend ya expone sesiones vía SSE, hay que agregar.
   - Útil para el admin pero no lo echa de menos el usuario final.

5. **Logs viewer**
   - Debug hoy es `docker logs` / `journalctl`. No es bloqueador
     para usuarios.

6. **Override manual de channel number / group**
   - Hoy el M3U manda. El admin no puede re-ordenar.
   - Patrón: extender la tabla `channel_overrides` (ya existe para
     `tvg_id`) con `number` y `group` opcionales.

7. **Timeshift / DVR** ← Plex lo tiene. Scope enorme. Post-MVP.

**Lo que NO pondría esfuerzo ahora** (over-engineering para
self-host personal):
- Plugins catalog (Jellyfin)
- Webhooks / notifications
- Parental controls multi-usuario
- Device management UI / revocación por dispositivo
- E2E tests Playwright (no hay usuarios reales todavía)

### 🚀 Recomendación de próxima sesión

**Elige UNA cosa, no tres**. Scope discipline.

**Default recommendation — matcher EPG agresivo** (#1 arriba):
- Máximo impacto visible (EPG 19% → 60-75% cobertura).
- Solo backend, sin tocar UI, sin churn de tests.
- Scope contenido: 1 día bien enfocado.
- Se mide objetivamente: cuenta canales con programas en ventana
  -2h..+24h antes/después contra la misma URL de davidmuma.

**Alternativa si prefieres producto visible — scheduler UI** (#2):
- Saca al producto del modo "herramienta manual".
- Toca backend + UI admin. Bien de scope para 1 día.
- Patrón ya probado en `library.Scheduler` (auto-scan existente).

**Lo que NO haría en la siguiente sesión** (decidido):
- Tests de admin panels — prematuro, se reescribirían.
- Split de `library.Service` — nice-to-have, no bloquea nada.
- E2E tests — demasiado pronto.
- IPTV fan-out relay — solo cuando lleguen usuarios reales.

### Estado al abrir sesión

- `main` tiene PRs #81 (simplify + LiveTV split) + #82 (coverage
  slice 1) mergeados.
- Rama actual `claude/review-project-tasks-f5Rdg` lleva **5 commits
  encima de main, no hay PR abierto**. Decidir al abrir:
  - Opción A: abrir PR y mergear a main antes de empezar.
  - Opción B: trabajar encima de ella si la nueva tarea es continuación
    directa (no lo es — los 4 entregables de esta rama ya están
    auto-contenidos).
  - Recomendado: **abrir PR, mergear, rama nueva** para la siguiente
    tarea.
- Todos los tests verdes al final: `pnpm test` 221/221 · `pnpm tsc
  --noEmit` · `pnpm build` · `pnpm lint` 0 · `go test -race ./...`
  21 paquetes.
- No hay cambios sin commitear.

### Checklist al abrir siguiente sesión

- [ ] Leer este handoff entero.
- [ ] `git checkout main && git pull` (tras mergear PR de esta rama).
- [ ] Elegir UNA tarea del recomendado (matcher EPG o scheduler UI).
- [ ] Rama nueva `claude/<nombre-descriptivo>`.
- [ ] Ver las "reglas senior asentadas" en la sección "Lint debt
  cero" abajo — especialmente "no memoización manual" y "no
  set-state-in-effect". También las de `conventions.md` (React 19 +
  Compiler, ref capture, fast-refresh).
- [ ] Al acabar: `pnpm test` + `pnpm tsc` + `pnpm build` + `pnpm lint`
  + `go test -race ./...` verdes. Commit descriptivo. Push. PR.
- [ ] Actualizar esta memoria con el ciclo hecho.

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
- **~29** test files en frontend (añadidos `livetv/` + 5 wizard + 4 slice-2)
- **Cero** `TODO`/`FIXME`/`HACK`
- `go test -race ./...` verde en 21 paquetes; `golangci-lint v1.64.8`: exit 0
- Frontend: `pnpm build` + `pnpm test` (221/221) + `pnpm lint` + `pnpm tsc --noEmit` los cuatro en verde

## Lint debt cero (`claude/review-project-tasks-f5Rdg`)

De **14 problemas (10 errors + 4 warnings) → 0**. Sin cambios de
comportamiento, todos refactors a patrones canónicos React 19 +
Compiler. Cada fichero se verificó con `eslint <ruta>` antes de
seguir, y `pnpm test` + `pnpm tsc` siguieron en verde tras cada cambio.

| Fichero | Problemas | Patrón aplicado |
|---|---|---|
| `web/src/components/media/MediaGrid.tsx` | 1× set-state-in-effect | "Reset on prop change" durante render: `[prevItems, setPrevItems]` + comparar y `setVisibleCount(BATCH_SIZE)` antes del primer paint, en vez de `useEffect → setState`. |
| `web/src/components/layout/AppLayout.tsx` | 3× set-state-in-effect | (a) `useIsMobile` reescrito con `useSyncExternalStore` (matchMedia → React sin efecto). (b) Cierre del drawer en cambio de viewport/ruta vía clave de reset comparada en render (`prev !== curr → setMobileOpen(false)`). Bonus: tres `useCallback` huérfanos eliminados — el compiler memoiza solo. |
| `web/src/components/setup/FolderBrowser.tsx` | 2× preserve-manual-memoization + 1 dep warning | Eliminados los `useCallback` que el compiler logueaba como "Compilation Skipped" porque las deps `data?.parent` / `data?.current` no incluían `data`. Sin manual-memo el compiler memoiza la función entera del componente. |
| `web/src/pages/ItemDetail.tsx` | 1× set-state-in-effect + 2× preserve-manual-memoization + 1 dep warning | (a) `siblingEpisodes` ya no es `useState + useEffect`, sino `useMemo([siblings])` — derivación pura. (b) `menuItems` deja de envolverse en `useMemo`: las deps no listaban `queryClient` y el compiler skipeaba el memo, así que el manual era ruido. Con compiler activo, el componente se memoiza completo. |
| `web/src/components/player/TimeDisplay.tsx` | 1× react-refresh/only-export-components | `formatTime` deja de re-exportarse del fichero del componente — nadie lo importa fuera y la mezcla rompía Fast Refresh. Se quita también del barrel (`components/player/index.ts`). |
| `web/src/hooks/useProgressReporter.ts` | 1× ref-in-cleanup warning | `videoRef.current` se captura **dentro** del efecto (`const video = videoRef.current` antes del `return () => {...}`); el closure usa la variable local. La función la rige el patrón canónico que recomienda el propio mensaje del linter. |
| `web/src/api/client.ts` | 1× unused eslint-disable | El comment `// eslint-disable-next-line @typescript-eslint/no-non-null-assertion` apuntaba a `let response!: Response` — pero `!:` es definite-assignment, no non-null access; la regla nunca disparaba. Sustituido por un comentario que explica por qué TS necesita la aserción. |

**Reglas senior asentadas para el ciclo siguiente**:

1. **No memoización manual con React Compiler activo**. Si la lint
   dice "Compilation Skipped: Existing memoization could not be
   preserved", el `useCallback` / `useMemo` se borra; no se intenta
   "arreglar las deps". El compiler memoiza el componente entero
   gratis y un manual mal-depped es siempre peor que ninguno.
2. **`set-state-in-effect` se trata como bug, no nit**. Bajo React 19
   produce cascading re-renders reales (paint con valor stale, luego
   paint de la corrección). Patrones permitidos:
   - Derivación pura → `useMemo`.
   - Mirror de un store externo → `useSyncExternalStore`.
   - Reset de estado en cambio de prop → `[prev, setPrev]` + comparar
     en render (React docs "Adjusting state on prop change").
3. **`useEffect` con cleanup que lee `ref.current`**: capturar al
   inicio del efecto, no en el cleanup (el linter tiene razón aquí
   incluso cuando "parece que funciona").
4. **Mezclar component + helper exports** rompe Fast Refresh. Helpers
   van a su propio fichero o se mantienen privados. Si un helper se
   usa en sólo un componente, no se exporta.

## Livetv coverage — slice 2 (`claude/review-project-tasks-f5Rdg`)

Segunda tajada del track de estabilidad — cubre los 4 componentes
livetv que quedaron fuera del slice 1. Todos los tests aislan el
componente bajo prueba mockeando dependencias problemáticas
(`StreamPreview` por HLS en jsdom, `ChannelCard`/`HeroSpotlight` por
tamaño, `HeroSettings` por su propio estado interno).

Delta: **186 → 221 tests en Vitest** (+35), 4 ficheros nuevos.

| Fichero | Tests | Lo que pin-ea |
|---|---|---|
| `web/src/components/livetv/LiveTvTopBar.test.tsx` | 6 | Counts (total/live) desde props · 3 tabs con `role=tab` + `aria-selected` único · wiring onTab/onSearch · HeroSettings **solo** en tab `discover` (oculto en guide/favorites) · forward de heroMode + onHeroModeChange |
| `web/src/components/livetv/DiscoverView.test.tsx` | 7 | Rails en orden `CHANNEL_CATEGORY_ORDER` saltando vacíos · filtro por categoría renderiza solo ese rail · **"Apagados" solo con unhealthy && category==='all'** (no en filtro específico, no sin unhealthy) · empty state cuando no hay rails · onOpen forward · heroItems + heroLabel a HeroSpotlight |
| `web/src/components/livetv/HeroSpotlight.test.tsx` | 10 | `items.length===0` → null · 1 item sin dots, sin auto-rotate · 2+ items con dots `role=tab` + aria-selected · click-dot cambia activo · **auto-rotate 12 s wrap-around** (vi.advanceTimersByTime) · no-rotate con 1 item · click tile → onOpen(channel activo) · StreamPreview `key={channel.id}` remonta entre slides · **clamp** sobrevive shrinking items · LIVE pill condicional a nowPlaying |
| `web/src/components/livetv/EPGGrid.test.tsx` | 12 | 24 columnas en ruler + 1 corner · 1 row por channel · empty state · programas fuera de ventana (past/future) descartados · placeholder "sin guía" cuando `programs.length===0` · click programa + click sticky cell llaman onSelectChannel · `activeChannelId` → `aria-pressed="true"` exactamente en esa row · "Ahora · HH:MM" con reloj fake · **auto-scrollTo solo en mount**, re-ticks no re-scrollean · botón "Ahora" smooth-scroll · `autoScrollToNow=false` no invoca scrollTo |

**Patrones de test adicionales aprendidos en slice 2**:

1. **`role="gridcell"` tapa el rol implícito "button"**. `EPGGrid` pone
   `role="gridcell"` en los `<button>` de la celda de canal; `getByRole
   ("button")` no los encuentra. Para estos casos usar
   `container.querySelector('button[aria-pressed]')` — el selector
   CSS va al DOM crudo sin pasar por el ARIA role resolver.
2. **`HTMLElement.prototype.scrollTo` se stubea a nivel de prototype**
   antes del render para capturar llamadas del ref interno. Más
   limpio que mockear `useRef` o esperar al efecto. Siempre restaurar
   el original en `finally` para no contaminar otros tests.
3. **Nombres en `ChannelLogo` aparecen 2x** (aria-label del wrapper +
   div visible) — `getByText` falla con "multiple elements". Usar
   `getAllByText("name").length > 0` o `container.querySelectorAll`
   para contar rows.
4. **`vi.advanceTimersByTime` dentro de `act()`** para que React
   procese los setState del timer y re-renderice antes de aserr. Sin
   `act()` el dot nuevo no está marcado `aria-selected` cuando
   leemos.

## Setup wizard coverage (`claude/review-project-tasks-f5Rdg`)

## Setup wizard coverage (`claude/review-project-tasks-f5Rdg`)

Ruta crítica del primer arranque — antes 0 tests, ahora **+48**
(mismo ciclo que el slice 2 de livetv).
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
- ~~Matcher más agresivo para subir cobertura EPG~~: ✅ hecho en
  `claude/review-pending-tasks-9Vh6U`. Alias table + channel-number
  match (con guardas de posicional + ambigüedad) + Levenshtein fuzzy
  como último recurso. Ver handoff arriba. Falta medir cobertura
  real ante/después contra davidmuma.

### Event bus (3 tipos reservados sin publisher)
- `ChannelAdded`/`ChannelRemoved` — requiere descomponer `ReplaceForLibrary`
- `MetadataUpdated` — necesita hook en scanner o flujo de refresh dedicado

### Frontend general
- Tests del setup wizard (0 cobertura, ruta crítica de primer arranque).
- Tests de páginas admin (mutan estado, 0 tests — los panels nuevos
  `LivetvAdminPanel` + sub-paneles entran en este hueco).
- Tests de componentes livetv restantes: ✅ slice 2 completo
  (`LiveTvTopBar`, `DiscoverView`, `HeroSpotlight`, `EPGGrid`).
- Lint pre-existente: ✅ liquidado (14 → 0 en esta misma rama, ver
  sección "Lint debt cero" arriba).

### Arquitectura / deuda estructural (senior review 2026-04-24)
- **`library.Service`**: por auditar. Probablemente misma forma god-service
  que tenía `iptv.Service` (scan + schedule + metadata + image refresh en
  un struct). Aplicar la misma receta de split por ficheros
  (`service_*.go` sobre el mismo struct).
- **`internal/api/handlers/`** — si crece a 15+ ficheros sueltos, partir
  en subpaquetes por dominio. Hoy no urgente: foco en tests.
- ~~`web/dist/` tracked en git~~ — ✅ liquidado en esta rama (commit
  `chore(build): stop tracking web/dist build output`). Sentinel
  `.gitkeep` + `.gitignore` + plugin Vite `preserveGitkeep` en
  `closeBundle` + Makefile `clean` que preserva el sentinel.

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
2. ✅ **Tests del setup wizard** — hechos esta rama (+48).
3. **Tests de páginas admin** (`LivetvAdminPanel` + sub-paneles).
4. ✅ **Lint debt** — liquidado a cero esta rama.
5. **Split de `library.Service`** siguiendo la receta del iptv.

**Si foco = experiencia LiveTV**:
1. ✅ **Matcher EPG más agresivo** — hecho en `claude/review-pending-
   tasks-9Vh6U`. Falta medir cobertura real contra davidmuma.
2. ✅ **Scheduler UI M3U + EPG refresh** — hecho en la misma rama.
   Tabla `iptv_scheduled_jobs`, worker `iptv.Scheduler`, panel admin
   `ScheduledJobsPanel`. Falta dejar correr 6 h+ en real para validar.
3. ✅ **"Continuar viendo"** — hecho en la misma rama. Tabla
   `channel_watch_history` keyeada por stream_url, beacon en
   `useLiveHls.onFirstPlay`, rail en Discover. Falta validar
   cross-device en real.
4. **Modal de detalle de programa** en EPG grid.
5. **Streaming parser del XMLTV** (bomba memoria con feeds 2 GB).
6. **Override manual de channel number / group** — extender
   `channel_overrides` con `number`/`group` opcionales.
