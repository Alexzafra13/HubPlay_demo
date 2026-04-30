# Architecture Decision Records

> ADRs cortos para HubPlay. Formato: **Contexto → Decisión → Consecuencias → Alternativas**.
> Un ADR se añade; no se edita. Si la decisión cambia, se crea uno nuevo que supersede.
> Las "decisiones de no hacer" también cuentan — si algo se descartó con razón, registrar aquí ahorra el debate en dos meses.

---

## ADR-001 — Query layer: sqlc sobre `database/sql`

- **Fecha**: 2026-04-15
- **Estado**: Aceptado
- **Supersede**: —
- **Contexto de descubrimiento**: Auditoría `audit-2026-04-15.md`, §2.1

### Contexto

HubPlay tenía `sqlc.yaml` + `make sqlc` + una línea en `CLAUDE.md` declarando `sqlc (generated)`, pero los 16 repos en `internal/db/` estaban escritos a mano con `database/sql`, `Scan()` manual, helpers `nullStr()` duplicados, y el directorio `internal/db/queries/` no existía. Estado inconsistente detectado durante la auditoría del 2026-04-15.

Requisitos del proyecto:
- Un binario, pocas dependencias, self-hosted.
- SQLite como DB por defecto (usando `modernc.org/sqlite`, pure-Go, sin CGO).
- Goose para migraciones.
- FTS5 para búsqueda.
- Pre-launch → ventana abierta para romper APIs internas.

### Decisión

Adoptar **`sqlc`** como única fuente para la capa de consultas.

- Las queries viven en `internal/db/queries/<dominio>.sql`.
- El código generado se emite a `internal/db/sqlc/` (ya configurado en `sqlc.yaml`).
- Los repositorios en `internal/db/*_repository.go` se convierten en **adaptadores delgados** que:
  - Envuelven `*sqlc.Queries`.
  - Mapean errores SQL a sentinels/`AppError` del paquete `domain`.
  - Convierten `sql.Null*` ↔ tipos del dominio cuando añade claridad.
  - Preservan las interfaces estrechas que ya consumen servicios (ej. `signingKeyRepo` en `internal/auth/keystore.go`) para no tocar llamadas arriba.
- `emit_interface: true` en `sqlc.yaml` genera un `Querier` que los tests pueden mockear.
- Migraciones siguen gestionadas por **`goose`**; `sqlc` solo lee el schema para inferir tipos.

### Driver

Se mantiene **`modernc.org/sqlite`** (pure-Go). La penalización de rendimiento vs `mattn/go-sqlite3` (CGO) es aceptable para el perfil de carga de un servidor self-hosted, y a cambio:
- Binario único, sin toolchain C.
- Cross-compile trivial (amd64/arm64 nativos).
- `Dockerfile` multi-arch limpio.
- No complica el target `hwaccel`.

### Consecuencias

**Positivas**
- Type-safety compile-time en todas las queries (un cambio de schema rompe `go build`, no runtime).
- ~40% menos código en `internal/db/` esperado tras migrar los 16 repos.
- `nullStr()` y otros helpers duplicados desaparecen.
- Nuevas queries se añaden en SQL, no en boilerplate Go.
- Mockeo trivial en tests vía el `Querier` interface.

**Negativas / trade-offs**
- `sqlc` requiere un paso extra en el flujo: `make sqlc` tras cambiar `.sql`. Mitigación: target de Makefile ya existe; `sqlc-check` en CI bloquea PRs con queries no regeneradas.
- FTS5 `MATCH` y joins con parámetros dinámicos requieren patrones específicos (nullable params, `COALESCE`, o raw queries como último recurso). Se documentará caso por caso en `conventions.md` según vayan apareciendo.
- Añade dependencia de build: developers necesitan `sqlc` instalado (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest` o Docker `sqlc/sqlc`). Se documenta en README del paquete y en `docs/architecture/tooling.md`.

### Alternativas descartadas

- **`sqlx`**: buen middle-ground (structs con tags, sin codegen), pero no da compile-time safety sobre SQL. Descartada por perder la garantía más valiosa de `sqlc`.
- **`ent`**: ORM schema-first potente, pero pesado para un schema que ya está gobernado por migraciones a mano. Introduce una segunda fuente de verdad del schema.
- **`GORM`**: ORM completo con reflection runtime. Incompatible con el criterio "limpio, eficiente, comprensible" del proyecto.
- **`ncruces/go-sqlite3` (driver WASM)**: prometedor pero menos batallado que `modernc.org/sqlite`. Revisitable en el futuro si el perfil de rendimiento lo pide.
- **`mattn/go-sqlite3`**: más rápido pero requiere CGO. Rompe distribución como binario único.
- **Mantener `database/sql` + aceptar el boilerplate**: rechazado. El schema tiene tamaño suficiente (28+ tablas) para que la repetición sea una fuente real de bugs.

### Plan de migración

Incremental, una tabla por commit. Orden propuesto (de menos a más tocado arriba):

1. **Piloto**: `signing_keys` (6 queries, sin callers complejos). Valida patrón.
2. `sessions`, `api_keys`, `providers`, `webhook_configs` (tablas pequeñas, independientes).
3. `users`, `libraries`, `library_paths`, `library_access`.
4. `channels`, `epg_programs`, `media_segments`, `trickplay_info`.
5. `items` + `ancestor_ids` + `metadata` + `external_ids` + `people` + `item_people` + `item_values` + `item_value_map` + `media_streams` + `chapters` (bloque de biblioteca, máxima superficie).
6. `images`, `user_data`, `activity_log`.
7. Una vez todos migrados: borrar `scan_helpers.go`, helpers `nullStr()`, y cualquier import de `database/sql` residual en repos.

Cada fase mantiene la interfaz consumida por servicios estable → no hay cambio en handlers ni en packages arriba.

### Verificación

- `go build ./...` compila tras cada commit.
- Tests existentes pasan sin modificarse (cuando los haya).
- `golangci-lint run ./...` limpio.
- Cobertura ≥ nivel actual.

### Referencias

- [sqlc docs](https://docs.sqlc.dev/)
- `sqlc.yaml` en raíz del repo
- `internal/db/queries/signing_keys.sql` (piloto)

### Reaffirmation 2026-04-25 — sqlc sweep + reglas duras

Tras el ADR original migramos los 16 repos heredados a sqlc, pero las
4 tablas nuevas que aparecieron en branches posteriores
(`library_epg_sources` mig 007, `channel_overrides` mig 009,
`iptv_scheduled_jobs` mig 011, `channel_watch_history` mig 012)
volvieron a escribirse en raw SQL. El comentario inline ("the sqlc
adapter isn't regenerated as part of this change") justificaba un
atajo individual, pero el patrón se propagó: cada nueva tabla seguía
el precedente raw. El review senior de 2026-04-25 lo identifica como
el debt-compound oculto más serio del proyecto — una predicción
concreta: "alguien renombra una columna en `iptv_scheduled_jobs`,
ejecuta `make sqlc` (no-op para el repo raw), tests pasan en
fixtures, runtime falla en producción".

Estado tras el sweep (commit `<<this-commit>>`):
- Las 4 tablas tienen su `internal/db/queries/<tabla>.sql`.
- Los 4 repos son ahora adaptadores delgados sobre `*sqlc.Queries`,
  con la misma interface pública que antes (cero cambio en callers).
- Tests existentes pasan sin modificación.
- Quedan 0 raw repos post-ADR.

Reglas duras a partir de aquí (a la vista en este ADR para que el
próximo PR las cumpla sin debate):

1. **Toda tabla nueva exige su query file en `internal/db/queries/`
   y su repo como adaptador sqlc.** No se aceptan `r.db.QueryContext`
   crudos en repos nuevos.
2. **Excepción explícita y documentada**, NO precedente. Si un repo
   necesita raw SQL (TX multi-paso, EXPLAIN, FTS5 dinámico), se
   añade un comentario al método indicando exactamente por qué sqlc
   no aplica AHÍ — y solo ahí.
3. **`make sqlc` corre antes de cada PR que toque schema o
   queries.** El target ya existe; el desarrollador es responsable
   de regenerar y commitear `internal/db/sqlc/`.
4. **Caracteres no-ASCII en query files rompen el parser de sqlc
   v1.29** (em-dashes, ∈, …). Mantener queries y comentarios en
   ASCII puro. Bug aprendido durante el sweep.
5. **`COALESCE(MAX(x), -1)` en agregados** que sqlc no puede
   tipar — el wrapper devuelve `interface{}`, el repo casts a
   int64 y normaliza el sentinel "-1 → no rows yet". Documentado
   en `library_epg_sources_repository.go` para que el patrón sea
   reusable.

Sentinel-to-AppError mapping sigue siendo job del repo
(`ErrEPGSourceAlreadyAttached`, `ErrIPTVScheduledJobNotFound`,
`ErrChannelNotFound`). sqlc no lo hace por nosotros.

---

## ADR-002 — Imágenes: descarga a disco siempre, URL remota nunca al cliente

- **Fecha**: 2026-04-27
- **Estado**: Aceptado
- **Supersede**: —
- **Contexto de descubrimiento**: Auditoría del pipeline de imágenes durante el review senior de movies/series.

### Contexto

El scanner persistía `db.Image.path = img.URL` (URL TMDb cruda) en lugar
de descargar el binario al disco. El handler `ServeFile` aceptaba paths
HTTP y respondía con `307` al cliente, así cada vista de poster era un
round-trip browser → HubPlay → TMDb.

Implicaciones:
- **Privacidad**: cada poster filtra la IP / User-Agent del cliente al
  proveedor externo. Para una app self-hosted "como Plex" eso traiciona
  el modelo mental del usuario.
- **Disponibilidad**: si TMDb cae o rate-limita, todos los posters
  rompen. España bloquea ciertos hosts vía orden judicial; el problema
  no es hipotético.
- **Cache + backoff layer (ADR del cache transport TMDb/Fanart) es
  inservible aquí**: ese RoundTripper solo cubre llamadas server-side,
  no los redirects que sigue el navegador.

### Decisión

**Toda imagen que va a la DB pasa antes por disco.** El campo
`db.Image.path` siempre tiene la forma `/api/v1/images/file/{id}`,
nunca un URL externo.

Implementación:
1. `internal/imaging/IngestRemoteImage(dir, kind, url, logger)` es la
   única vía de entrada. Runs SafeGet (SSRF + size + content-type),
   `EnforceMaxPixels`, `ComputeBlurhash`, escribe atómico
   (`AtomicWriteFile = write-tmp + rename`).
2. **Scanner** y `ImageRefresher` ambos usan este helper. Un cambio
   futuro en el pipeline solo se hace una vez.
3. Test de regresión `TestFetchAndStoreImages_PersistsLocalPathNotURL`
   falla si alguien re-introduce un `Path` con `http://`.
4. El handler `ServeFile` redirige a URL externo solo si el path
   empieza con "http" — esto queda como **fallback de migración**
   para datos pre-existentes, no como contrato vivo. Una migración
   futura podría limpiar esos paths legacy.

### Consecuencias

- Scan más lento (descarga sincrónica de imágenes nuevas), pero
  scaning ya era I/O-bound. El delta es ~10-15s por 3000 items con
  cache+backoff transport. Aceptable.
- Disk usage crece con la librería. Bound por `MaxUploadBytes` (10 MB
  por imagen) × items × kinds. Para una librería de 5000 películas con
  primary + backdrop + logo eso es ~1.5 GB upper bound; en la práctica
  ~300 MB. Plex tira más.
- Atomic writes evitan el clásico "fichero corrupto si crash en mitad
  de escritura". El `.tmp` queda solo si rename falla, y la próxima
  ingest lo sobrescribe.

### Alternativas descartadas

- **Mantener URLs remotos como fallback "lazy"**: descartado. El
  contrato "URL siempre apunta a HubPlay" simplifica todo el resto
  de la pila (cache headers, blurhash placeholder, dedup futuro).

---

## ADR-003 — Image lock: per-image, auto-set en cualquier acción manual

- **Fecha**: 2026-04-27
- **Estado**: Aceptado
- **Supersede**: —
- **Migración**: `migrations/sqlite/013_image_lock.sql`

### Contexto

Sin un mecanismo de "lock", cualquier refresh (manual o programado)
sobrescribía la curación del admin. El flow era:
1. Admin sube poster custom o elige uno específico de TMDb candidates.
2. Scheduler / next-scan / manual refresh corre.
3. ImageRefresher decide "no tengo nada para esta kind" → descarga
   nueva candidata top-scored → la marca primary → la del admin queda
   no-primary y eventualmente se borra.

Plex y Jellyfin ambos resuelven esto con un flag "lock" per-imagen.

### Decisión

**Flag `is_locked` en `images` (default 0).** Tres reglas:

1. **Auto-lock en cualquier acción manual**: tanto Upload como Select
   (de candidatos) marcan la fila resultante con `is_locked = true`.
   El admin que CHOOSES algo está expresando voluntad explícita de
   curación.
2. **Refresher gate per-kind, no per-item**: si existe **alguna**
   imagen locked para `(item, kind="primary")`, el refresher salta
   "primary" para ese ítem. Otras kinds (backdrop, logo) siguen
   refrescándose. Per-kind preserva el caso "tengo el poster que
   quiero pero el backdrop puede actualizarse".
3. **Toggle endpoint público**: `PUT /items/{id}/images/{imageId}/lock`
   con `{locked: bool}`. El admin puede liberar el lock cuando quiera
   permitir el refresh otra vez.

Schema (migración 013):
```sql
ALTER TABLE images ADD COLUMN is_locked BOOLEAN NOT NULL DEFAULT 0;
CREATE INDEX idx_images_item_type_locked ON images(item_id, type, is_locked);
```

El índice es la pata caliente: el refresher consulta
`HasLockedImageForKind(item_id, kind)` para cada `(item, kind)` en el
batch. Sin índice, full-scan por consulta.

### Consecuencias

- Un admin que quiere "que se actualice todo" tiene que des-lockear
  manualmente. Tradeoff aceptable: el caso común es "que no se
  actualice lo que yo elegí".
- Tests verifican el contrato: `TestImageRefresher_SkipsLockedKinds`
  pin tanto el skip de la kind locked **como** el flujo normal en
  otra kind del mismo item.

### Alternativas descartadas

- **Lock per-item** (un solo flag que bloquea todas las kinds):
  rechazado. El usuario que sube un poster custom raramente quiere
  bloquear también backdrop y logo — esos pueden mejorar con un
  refresh.
- **Lock implícito por provider** (provider `local`/`upload` siempre
  inmune al refresher): rechazado por ser frágil. El admin que hace
  Select de un candidate TMDb también está manualmente eligiendo, y
  con lock implícito no quedaría protegido.

---

## ADR-004 — Continue Watching filtra near-complete y abandoned

- **Fecha**: 2026-04-27
- **Estado**: Aceptado

### Contexto

El SQL original de Continue Watching era literal:
```sql
WHERE ud.completed = 0 AND ud.position_ticks > 0 AND i.is_available = 1
```

Resultado tras 6+ meses de uso real: rail de 30+ "zombies" — episodios
medio vistos hace dos años, películas paradas a falta de 2 minutos pero
nunca marcadas como vistas, intentos abandonados de la S1E1 de tres
series distintas. La señal real ("¿qué estoy viendo ahora?") quedaba
sepultada bajo el ruido.

Plex y Jellyfin ambos tienen heurísticas similares; ninguno las
documenta bien.

### Decisión

Dos filtros adicionales en el SQL, ambos integer-safe (sin floats):

1. **Near-complete drop**: `position_ticks * 100 >= duration_ticks * 90`.
   Si vimos ≥90% del runtime, asumimos que terminamos. El último 5-10%
   son créditos / outro que el usuario suele saltar; nunca llegan a
   `completed = 1`.

2. **Abandoned drop**: `last_played_at < threshold AND
   position_ticks * 2 < duration_ticks`. El threshold se pasa como
   parámetro desde Go (`db.AbandonedAfter = 30 * 24h` por defecto,
   var package-level para futura config / per-user). Si pasaron 30
   días Y vimos <50%, el usuario pasó página.

3. **Items con `duration_ticks = 0` bypassan ambos filtros**. Sin
   duración no podemos razonar sobre progreso; preferimos surfacearlo
   a hacerlo desaparecer silente.

Implementación: query en `internal/db/queries/user_data.sql` (sqlc
hand-edited a `sqlc/user_data.sql.go` porque el binario sqlc no está
en el build env, pero la forma es idéntica al regen).

### Consecuencias

- En cuenta real con historial denso: el rail pasa de ~30 zombies a
  ~3-5 reales sin ninguna acción del usuario.
- Los thresholds (90%, 30d, 50%) no son configurables por usuario hoy.
  `AbandonedAfter` se puede sobrescribir desde main.go como package-
  level var; la mitad-de-progreso y el 90% están hardcoded en SQL.
- Tres tests dedicados: drop near-complete, drop abandoned, keep
  unknown-duration. Las constantes son visibles desde Go.

### Alternativas descartadas

- **Filtro client-side**: rechazado. Cargaríamos al cliente filas que
  el server tendría que devolver en el rail Y que después el cliente
  esconde — ancho de banda inútil, peor scrolling.
- **Heurísticas más complejas** (¿es serie? ¿hace cuánto vio el último
  episodio? ¿es viernes?): rechazado por ahora — añade ruido
  diagnóstico ("¿por qué no aparece esto?") sin valor demostrable
  hasta que tengamos datos de uso.

---

## ADR-005 — Show hierarchy: derivada de estructura de directorios

- **Fecha**: 2026-04-27
- **Estado**: Aceptado

### Contexto

El scanner procesaba shows como ficheros sueltos: una fila
`type=episode` por `.mkv`, sin parent. La query de `/series` filtra
`type='series'` → 0 resultados. El usuario intentó usar la app y vio
una pantalla vacía.

Plex / Jellyfin / Kodi todos derivan jerarquía del filesystem
(convención de facto):
```
<libRoot>/<Series Name>/<Season N>/<file>.ext
```

### Decisión

**El scanner detecta jerarquía desde el path del fichero.**

1. **Parser puro** (`internal/scanner/show_parser.go`):
   - Extrae `{SeriesName, SeasonNumber, EpisodeNumber, EpisodeTitle, OK}`
     del path.
   - Reconoce `SxxExx`, `NxN`, `S01.E05`, dirs en 4 idiomas
     (Season/Temporada/Saison/Staffel) + `S01` / `Season01` corto.
   - Cuando dir tiene número de season y filename solo el de episode
     (`Season 03/05.mkv`), combina los dos.
   - Cuando ambos están y discrepan (Doctor Who 2005 numerado por año
     en filename pero por temporada real en dir), **el dir gana** —
     fuente más estable.
   - `OK = false` cuando el path no encaja (file en raíz, layout
     extraño): el caller crea la fila episode sin parent. Mejor
     surfacear sin jerarquía que perder el fichero.

2. **Cache pre-poblado por scan** (`internal/scanner/show_hierarchy.go`):
   - `showCache` mantiene los IDs de series + season ya conocidos.
   - Al inicio de `ScanLibrary`, la pasada de
     `iterateLibraryItems` (que ya existía para detectar removidos)
     siembra el cache desde filas existentes en DB.
   - Durante el walk, `ensureSeriesRow` y `ensureSeasonRow` consultan
     el cache — solo escriben a DB en miss. Re-scan idempotente: 0
     queries de inserción.
   - Mutex defensivo aunque ScanLibrary sea single-threaded (libs
     paralelas tienen caches separados, pero el patrón es free).

3. **`db.Item.ParentID` linka**. No hay `series_id` propio en
   `db.Item` — la relación es:
   `episode.parent_id = season.id`,
   `season.parent_id = series.id`,
   `series.parent_id = ""`.
   El handler reconstruye la cadena cuando un cliente la pide.

### Consecuencias

- Una librería con flat layout (todos los ficheros en raíz) crea
  episodios sin parent. No hay /series para esa librería.
  Workaround: re-organizar manualmente a `<root>/<Show>/<Season N>/...`.
- La detección **no consulta TMDb**. Eso es deliberado: el scanner
  inicial es offline-friendly. El `MetadataMatcher` (paso siguiente
  del pipeline) puede mejorar el `Title` después.
- 25+ casos de test cubren los formatos comunes. Patrones nuevos
  (release scenes con tags raros) caerán por defecto al lado seguro
  (`OK = false` → fichero sin parent).

### Alternativas descartadas

- **Parsing por TMDb match**: rechazado. Acopla creación de filas a
  disponibilidad de internet + API key configurada. Plex tiene
  exactamente este problema y produce librerías rotas en setup
  inicial.
- **Series-id explícito en `db.Item`** (en vez de derivar via parent
  chain): rechazado. Duplica información que ya está en el grafo.
  La query "todos los episodios de esta serie" vive en SQL con un
  JOIN doble — tolerable.

---

## ADR-006 — HW accel: input-side `-hwaccel` sin `-hwaccel_output_format`

- **Fecha**: 2026-04-27
- **Estado**: Aceptado

### Contexto

`stream.DetectHWAccel` ya probaba VAAPI / NVENC / QSV / VideoToolbox al
arranque y verificaba con un frame de prueba. Pero el resultado se
loggeaba en main.go y se descartaba — el transcoder seguía usando
`libx264`. Cualquier máquina con GPU tenía 0% utilización durante
transcodes.

Wire correcto requiere decidir qué tan agresivo se va con el
hardware path.

### Decisión

**Encoder swap + input-side `-hwaccel <kind>` para VAAPI/QSV/NVENC.
NO `-hwaccel_output_format`. NO `-vf scale_xxx` HW-specific.**

```
ffmpeg -hwaccel cuda -i input.mkv -c:v h264_nvenc -vf scale=W:H:... ...
```

Versus el "modo agresivo":
```
ffmpeg -hwaccel cuda -hwaccel_output_format cuda -i input.mkv \
  -vf scale_cuda=W:H -c:v h264_nvenc ...
```

El segundo es más rápido (frames nunca tocan RAM del sistema) pero
exige rewriting el filter graph entero por backend HW. El primero
deja los frames bajar a RAM tras decode, escala con el filter SW
existente, y el encoder HW reuploadeа antes de codificar.

VideoToolbox **no recibe `-hwaccel`** (Apple solo provee encoder, no
decoder pipeline declarable así).

### Consecuencias

- Speedup real en máquinas con GPU: típicamente 2-3× vs libx264, no
  el 5-8× del modo agresivo. CPU baja del 100% al 20-30%, lo que ya
  resuelve el caso "el ventilador parece un secador".
- Filter graph se mantiene idéntico al SW path → mantenibilidad +
  testeabilidad. Tres tests pin la concatenación de args por
  encoder.
- El "modo agresivo" queda como follow-up cuando alguien tenga
  apetito por el rewrite.

### Alternativas descartadas

- **Modo agresivo desde el inicio**: rechazado. ~200 LOC de filter
  por backend × 4 backends = 800 LOC de mantener, con bugs sutiles
  (formato de pixel post-decode varía, el `format=nv12` antes de
  hwupload depende del backend).
- **Solo encoder swap, sin `-hwaccel`**: rechazado. El decode SW de
  HEVC 10-bit es CPU-pesado; no aprovechar NVDEC / QSV decode es
  dejar la mitad del beneficio sobre la mesa.

---

## ADR-007 — Security headers como middleware compartido (no nginx-only)

- **Fecha**: 2026-04-28
- **Estado**: Aceptado

### Contexto

El servidor de producción detrás de nginx (`deploy/nginx/hubplay.conf`)
ya emitía X-Frame-Options, X-Content-Type-Options, Referrer-Policy y
HSTS. La opción "barata" era dejarlo todo ahí y no tocar el binario
Go. Pero `make dev` y `make web-dev` arrancan el binary directo en LAN
sin nginx delante; cualquier deployment que prescinda del proxy
(Tailscale, Cloudflare Tunnel, port-forward casero) corre **sin un
solo header de seguridad**. La instalación typical de un usuario
self-hosted no es la nuestra con nginx — es Tailscale + binary
directo.

CSP además no estaba en ningún lado, ni en nginx — porque construir
una whitelist correcta requiere conocer qué hosts externos cargan
imágenes (TMDb, Fanart, YouTube ytimg) y qué dominios embebimos
(YouTube nocookie, Vimeo, Google Fonts). Ese conocimiento vive en el
código del SPA, no en la config del proxy.

### Decisión

Middleware Go en `internal/api/security_headers.go` que emite el
juego completo (CSP/X-Frame/X-Content-Type/Referrer/CORP) en cada
respuesta del API y del SPA bundle. Se monta antes de CORS para que
los preflights también lleven los headers.

HSTS es **condicional sobre TLS real**: `r.TLS != nil` o
`X-Forwarded-Proto: https`. Nunca sobre HTTP plain LAN — los
browsers lo ignoran ahí pero el log queda más limpio sin él.

CSP se mantiene en el binary, NO se duplica en nginx, porque la
fuente de verdad sobre "qué hosts externos cargo" es el frontend.

### Consecuencias

- Cobertura uniforme: cualquier deployment está protegido sin
  depender del proxy. Tests propios en `security_headers_test.go`
  pin la presencia de los headers críticos.
- CSP estrecha por defecto. Añadir un host externo nuevo (CDN, embed
  platform) significa tocar `security_headers.go` — explícito por
  diseño. La alternativa "permitir todo" sería un blanket `*` que
  invalida el sentido de tener CSP.
- Nginx sigue añadiendo X-Frame/HSTS/etc. — defensa-en-profundidad,
  no conflicto. Browsers toman el header del proxy o del backend
  indistintamente.

### Alternativas descartadas

- **Solo nginx**: rechazada. Tailscale / Cloudflare Tunnel / dev mode
  quedan sin headers — y son la mayoría de instalaciones.
- **CSP en nginx**: rechazada. La whitelist de hosts externos vive
  en el frontend; mantenerla en dos sitios garantiza divergencia.
- **CSP report-only**: rechazada. Self-hosted single-tenant no tiene
  un endpoint de reportes, y sin enforce el header no compra nada.

---

## ADR-008 — Event bus: política "no recover de handlers que cuelgan"

- **Fecha**: 2026-04-28
- **Estado**: Aceptado, supersede política implícita previa

### Contexto

`internal/event/bus.go` antes envolvía cada handler en dos
goroutines: una externa con `select { <-done / <-timer.C }` y una
interna corriendo el handler. En timeout, la externa hacía `<-done`
incondicional — esperando indefinidamente a la interna. El comentario
decía "the goroutine is NOT leaked". En la práctica, eso es leak
disfrazado: si el handler nunca retorna, dos goroutines viven para
siempre.

Hoy nadie subscribe con un handler bloqueante (el SSE handler usa
`select { case eventCh <- e: default: drop }`), así que no hay leak
activo. Pero el patrón es frágil: el primer subscriber bloqueante
que entre rompe la garantía.

### Decisión

Una sola goroutine por handler con `recover` para panics. Watchdog
separado que loggea una vez si excede `slowHandlerThreshold` (30s) y
**siempre sale** — sin esperar al `done`. Si un handler cuelga
indefinidamente, leakea exactamente UNA goroutine (la del handler);
el bus no intenta abortar código de caller arbitrario porque no hay
forma segura de hacerlo.

Comentario explícito en `Publish`: "subscribers are responsible for
not blocking inside their handler". Es un contrato, no una sugerencia.

### Consecuencias

- Garantía clara: dispatch jamás bloquea sobre el handler. Watchdog
  tampoco — siempre sale. Nuevo subscriber bloqueante leakea su
  propia goroutine pero no la del bus.
- El test suite existente (panic recovery, multiple subscribers,
  unsubscribe idempotente, type isolation) sigue pasando sin cambios.
- Se gana simplicidad: una capa de goroutine en vez de dos.

### Alternativas descartadas

- **`context.Context` por handler con cancel**: rechazada. Go no
  permite cancelar goroutines arbitrarias; un handler que ignora el
  ctx queda igual de colgado. Solo añade complejidad de API.
- **Restringir Subscribe a handlers no-bloqueantes vía type system**:
  imposible; cualquier `func(Event)` es una closure arbitraria.
  Comentario + revisión de PR es la única defensa real.

---

## ADR-009 — Refresh dedup en cliente, no en servidor

- **Fecha**: 2026-04-28
- **Estado**: Aceptado

### Contexto

Discover y otras pantallas disparan ~5 queries en paralelo. Cuando la
cookie de access ha expirado, las cinco reciben 401 simultáneamente y
las cinco llaman a `ApiClient.refresh()`. El servidor es idempotente
(`internal/auth/service.go` no rota el refresh token), así que las
cinco cookies regrabadas son iguales — pero hay desperdicio (5 round-
trips, 5 writes a `last_active`, 5 onAuthFailure si el refresh falla
disparando 5 navegaciones a /login en el mismo tick).

Dos sitios donde se puede deduplicar:

1. **Servidor**: cache de "refresh in flight per session" → primera
   llama hace el trabajo, las demás reciben la misma respuesta.
2. **Cliente**: `ApiClient` cachea la promise in-flight → primera
   llama dispara el fetch, las demás `await` la misma promise.

### Decisión

**Dedup en cliente.** En `web/src/api/client.ts`, `ApiClient.refresh()`
guarda la promise in-flight en `this.refreshInFlight`. Llamadas
posteriores devuelven la misma. Slot se limpia con `.then(clear,
clear)` (no `.finally(clear)`, que crea una promise que mirror la
rejection y vitest la flagea como unhandled).

### Consecuencias

- Una llamada de red en lugar de N por refresh oportunista. Listener
  `onTokenRefresh` se invoca exactamente una vez. Failure-path llama a
  `onAuthFailure` exactamente una vez también — el usuario aterriza
  en /login una vez en lugar de N.
- Server-side queda intocado: simple, idempotente, sin estado nuevo.
  Si un cliente legacy no deduplica, el servidor sigue manejando los
  N requests sin corrupción.

### Alternativas descartadas

- **Dedup en servidor**: rechazada. Requiere lock per-session +
  cache de respuestas in-flight + invalidación correcta. Para self-
  hosted single-tenant es over-engineering.
- **Cliente dispara una sola query principal y deja que las otras
  esperen**: rechazada. Acopla TanStack Query a la mecánica de
  refresh; cualquier caller fuera de Query (websocket reconnect,
  beacon) quedaría sin protección.

---

## ADR-010 — Configuración runtime: YAML para boot, `app_settings` para preferencias

- **Fecha**: 2026-04-29
- **Estado**: Aceptado
- **Contexto de descubrimiento**: Sesión 2026-04-29 noche (commit `b1a84da`)

### Contexto

El panel `/admin/system` mostraba dos cards diciendo literalmente "Sin configurar (define `server.base_url` en `hubplay.yaml`)" y "Activa `hardware_acceleration.enabled` en `hubplay.yaml`". Para un producto self-hosted estilo Plex / Jellyfin / Sonarr, pedirle al admin SSH-earse y editar un fichero es una abstracción rota — la UI prometía ser el panel de control y luego derivaba al usuario a un editor de texto.

Las opciones eran:

1. **Quick win parcial**: cambiar default de `hardware_acceleration.enabled` a `true` (autodetect ya existe). Quita un prompt sin código nuevo, deja `base_url` como estaba.
2. **Solución completa**: tabla `app_settings` con overlay sobre YAML, endpoints admin, UI editable. Cierra los dos prompts y deja la arquitectura abierta.

El usuario invocó Torvalds explícitamente: si el defecto es arquitectural, parchearlo a la mitad es peor que no parchear — la mitad lleva a parches futuros para los mismos casos.

### Decisión

Splitear la configuración en dos capas con autoridad clara:

```
YAML / env (boot-time, immutable):
  server.bind, server.port, database.path, streaming.cache_dir,
  auth bootstrap secret. Cualquier cosa que requiera reinicio
  porque cambiarla invalidaría listeners ya bound, file handles,
  signing keys, etc.

app_settings (runtime-mutable, admin-editable desde el panel):
  server.base_url, hardware_acceleration.enabled,
  hardware_acceleration.preferred. Preferencias del operador que
  nunca debieron requerir SSH al host.
```

Authority chain en lectura:

```
app_settings row (si existe) → YAML / env value (fallback) → effective
```

Componentes:

- **Migración 019** crea la tabla `app_settings(key TEXT PK, value TEXT, updated_at)`. Schema mínimo; los valores son strings raw, validados en el handler boundary.
- **`internal/db/settings_repository.go`**: `Get / GetOr / Set / Delete / All`. Sin tipos de dominio.
- **`internal/api/handlers/settings.go`**: `GET / PUT / DELETE /admin/system/settings`. **Whitelist hardcoded**, NO un KV genérico — un const + un caso en `validateSettingValue` por cada nueva key editable. Validación normaliza el valor (trim, lower-case bool variants, strip trailing slash de URLs) antes de persistir.
- **Handlers que leen runtime values** (System, Stream) reciben un `SettingsReader` interface y consultan `GetOr(ctx, key, yamlFallback)` al servir la request. Sin `Runtime` overlay struct, sin caching layer, sin goroutine que vigile cambios. Una SQLite point query por hit, invisible en perfil.
- **HWAccel se aplica al boot**: `cmd/hubplay/main.go` lee del settings repo justo antes de construir el `streamManager`. La UI muestra "Reinicia para aplicar" cuando hay override pendiente. El detector tiene estado capturado al boot; replicarlo en runtime sería ruido.
- **BaseURL es runtime**: `effectiveBaseURL(ctx)` en cada request del System y Stream handler. Save en panel → próximo request lo usa.

### Consecuencias

**Positivas**

- El operador edita configuración runtime desde la UI, no desde un editor de texto en otra ventana.
- Una sola autoridad por valor (DB > YAML), sin segunda fuente de verdad.
- Whitelist hardcoded protege contra "modificar cualquier setting" — ni siquiera con admin auth se puede tocar lo que no debería ser runtime-editable.
- Añadir un cuarto setting es: const en `settings.go` + caso en `validateSettingValue` + par i18n + (opcional) `allowed_values` en el descriptor. Sin scaffolding nuevo.
- El YAML sigue siendo source-of-truth para los valores boot-time; cualquier deploy nuevo arranca con el comportamiento del YAML por defecto.

**Negativas / trade-offs**

- HWAccel requiere reinicio para aplicar cambios. La UI lo dice claramente; el detector se llama una sola vez al boot por diseño y replicarlo runtime no merece la pena.
- Una SQLite query extra por request en handlers que leen runtime values. Negligible (puntual, índice por PK).
- La tabla `app_settings` es un acoplamiento entre repo y handler — el handler conoce las keys hardcoded. Pero esto es a propósito: la whitelist es la red de seguridad.

### Alternativas descartadas

- **Mantener YAML único + flippar defaults útiles** (camino A): rechazado. Resuelve la mitad del problema para `hwaccel` pero deja `base_url` con el mismo prompt; la abstracción seguía rota.
- **Endpoint KV genérico (`PUT /settings/:any-key`)**: rechazado. El gate de la whitelist es lo que hace seguro este patrón en un producto self-hosted donde el admin auth es la única barrera.
- **`Runtime` struct overlay** (`Runtime` que envuelve `Config + SettingsReader`): rechazado. Una capa de indirección sin función — los handlers consumen `SettingsReader` directamente y se enteran del fallback en una línea.
- **Hot-reload de HWAccel** (re-detect cuando el toggle cambia): rechazado. El `stream.Manager` captura el resultado del detector en su estado. Mutarlo runtime obliga a sincronizar el transcoder y las sesiones activas; mucho riesgo para un beneficio marginal frente a un mensaje "Reinicia para aplicar".
- **Bus de eventos "settings changed"** que invalide caches: rechazado. No hay caches que invalidar — los handlers leen del repo en cada request.

---

## ADR-011 — Show hierarchy: UNIQUE partial indexes, sin recovery code en el scanner

- **Fecha**: 2026-04-29
- **Estado**: Aceptado
- **Supersede**: parte del enfoque "dedupe en read-time" de ADR-005

### Contexto

El usuario reportó series duplicadas en el rail. Análisis del schema `items`:

- PK es `id` (uuid), no hay UNIQUE constraint en `(library_id, type, title)` para series ni en `(parent_id, season_number)` para seasons.
- `internal/scanner/show_hierarchy.go` tiene un `showCache` per-scan que evita dups en condiciones normales, pero la key del map es title-exact. Cualquier diferencia (case, whitespace, accents, una mejora del path-parser entre versiones) → cache miss → row nuevo → dup.
- Cuando ya hay dups en DB, el seeding del cache (`rememberSeries(title, id)` por cada existing row) machaca el id anterior en el map; las filas anteriores quedan huérfanas para siempre.
- Para seasons existía dedupe en read-time (`DedupeSeasonsByChildCount` en `library/service.go`), para series no había equivalente — por eso aparecían dos veces en el rail.

El usuario verificó empíricamente con un wipe + rescan que el scanner actual **no recreaba dups** en su workflow. El bug era residuo histórico, no una regresión activa. Pero el schema lo permitía estructuralmente, y cualquier futura regresión o cambio del path-parser podía recrear el problema sin ninguna alarma.

### Decisión

Migración 019 hace **dos cosas**, ambas estructurales y sin código en el scanner:

1. **Cleanup defensivo**: para cada grupo de dups en `(library_id, 'series', title)` y `(parent_id, 'season', season_number)`, elige canónico = `MIN(id)` (determinista, lex order de uuids). Re-parenta los hijos de las no-canónicas a la canónica antes de borrar las demás. No-op en una DB ya limpia.

2. **UNIQUE INDEX parciales**:
   - `uniq_series_per_library ON items(library_id, title) WHERE type = 'series'`
   - `uniq_season_per_series ON items(parent_id, season_number) WHERE type = 'season' AND season_number IS NOT NULL`

   Filtrados por `type` para que movies / episodes / audio no participen — en episodes es legal tener varios files con el mismo `episode_number` (variantes de calidad), y movies pueden compartir título entre libraries.

**Lo que NO se hizo deliberadamente**:

- No se introdujo `ErrItemConflict` tipado en el repo.
- No se añadieron `FindSeriesByTitle` / `FindSeasonByNumber` helpers.
- No se añadieron recovery branches en `ensureSeriesRow` / `ensureSeasonRow` que capturen UNIQUE conflict y resuelvan via re-fetch.

Esa última pieza era atractiva como "defensa en profundidad" pero violaba Torvalds-simple aplicado consistentemente: el usuario ya había verificado que el scanner no genera dups; añadir silent-recovery code defendía escenarios hipotéticos y, peor aún, **disimularía un bug futuro real** en vez de exponerlo. Si la migración alguna vez salta en producción será porque el scanner regresionó (cambio de path-parser, race nuevo, lo que sea), y queremos que falle ruidosamente con `UNIQUE constraint failed: items.title` para enterarnos, no recuperar implícitamente.

### Consecuencias

**Positivas**

- Imposible estructuralmente que vuelvan a coexistir dos series con `(library_id, title)` igual o dos seasons con `(parent_id, season_number)` igual. La invariante está en el schema, no en código que se puede regresionar.
- Cualquier DB upgradeada con dups históricos se limpia automáticamente al aplicar la migración. Sin acción del operador.
- `DedupeSeasonsByChildCount` queda como red de seguridad puramente defensiva — tras la migración no debería tener nada que dedupar.

**Negativas / trade-offs**

- Si en el futuro un cambio del scanner introduce dups (cache key incorrecta, race), el scan falla con un error opaco para el usuario final. **Esto es exactamente lo que queremos** — fail loud, fix root cause — pero requiere que los operadores sepan interpretar `UNIQUE constraint failed: items.title` como "el scanner tiene un bug, no toques mi DB".
- La cleanup pass en la migración usa `MIN(id)` como canónico (lex order de uuid4), no "row con más hijos". En el caso patológico esto puede preservar la fila menos completa; el re-parenting de hijos lo compensa. Decisión consciente: simplicidad > optimización del canónico.

### Alternativas descartadas

- **Dedupe en read-time para series** (espejo del season pattern): rechazado. Hide-not-fix, además de pagar el coste en cada GET. La migración estructural lo cierra una sola vez.
- **`ErrItemConflict` + recovery en el scanner**: rechazado. Defendía edge cases (case/whitespace/accents drift) que el usuario verificó empíricamente que no ocurren en su workflow. Si alguna vez ocurriera, queremos saberlo, no taparlo.
- **Migración separada para cleanup, otra para UNIQUE**: rechazado. La cleanup es no-op si no hay dups; combinarlas es atómico (tx única de goose) y el operador sólo ve "se aplicó la migración 019".
- **Backfill `external_id` o `tmdb_id` como discriminator** en lugar de title: rechazado. El title viene del filesystem y muchos series no tienen tmdb_id matched aún (TMDb deshabilitado, no match). El title es el único campo siempre presente; las dos heurísticas son ortogonales pero el title es el primario.

---

## ADR-012 — Federación P2P: capa de integración sobre primitivos existentes, no infraestructura paralela

- **Fecha**: 2026-04-30
- **Estado**: Aceptado
- **Supersede**: —
- **Contexto de descubrimiento**: Diseño en `docs/architecture/federation.md` §13 ("Implementation reuse map"). Aplicado en commits `f8a9e3a` → `73b749c` (Phases 1-4 + plug-and-play + UX).

### Contexto

HubPlay diferencia frente a Plex/Jellyfin con federación peer-to-peer **sin servicio central**: dos servidores se enlazan vía invite code, exchangean keypairs Ed25519, y cada admin elige libraries que comparte. Naive: implementarlo como un módulo independiente con su propia capa de auth, sus propios session secrets, su propio rate limiter, su propio fetcher con SSRF guards. Ese camino habría duplicado ~5 piezas que ya están battle-tested en otros lugares del codebase.

El proyecto ya tiene:
- `internal/auth/keystore.go` — encriptación-at-rest de signing keys con rotación (usado para JWT signing keys de usuarios).
- `internal/auth/jwt.go` — issuance + validation de JWTs con claims plumbing (HS256 para usuarios).
- `internal/auth/ratelimit.go` — token bucket primitives (usado para login throttling).
- `internal/event/bus.go` — pub/sub interno con SSE downstream (`/events`, `/me/events`).
- `internal/imaging/safe_get.go::SafeGet` — outbound HTTP con SSRF guard + decompression-bomb cap (usado para IPTV channel logos, image refresh).
- `internal/stream/manager.go::StartSession` — session lifecycle con `MaxReencodeSessions`, hwaccel, idle reaper (usado para todo streaming local).
- `internal/iptv/transmux.go` — shared ffmpeg session per-channel, breaker integration.
- `internal/domain/errors.go` — `AppError` con `Kind` sentinel, mapping a HTTP status.

### Decisión

Federation **no introduce primitivas nuevas donde existen las útiles**. La nueva área del codebase (`internal/federation/`) es una capa de orquestación delgada que enchufa estos componentes en una secuencia distinta:

| Concern federación | Reusa (no reinventa) |
|---|---|
| Server Ed25519 identity key | `auth/keystore.go` extendido con accessor; misma encriptación at-rest. |
| Per-request peer JWT (EdDSA en vez de HS256) | `auth/jwt.go` con `EdDSA` algorithm variant añadido; mismo claims plumbing. |
| Token bucket per-peer | Estructura nueva `RateLimiter` en `federation/ratelimit.go` que **mira igual** que `auth/ratelimit.go` — la simplicidad del primitive (~50 LOC) hace que copy + adaptar sea más limpio que generic-ifyingla original. Ambas implementaciones convergen al mismo shape. |
| Federation events (peer.linked, peer.audit, etc.) | `event/bus.go` con tipos nuevos (`federation.peer_linked`, etc.). Subscribers existentes consumen sin special-casing. |
| Stream session para peer requests (Phase 5) | `stream/manager.go` con `scope=peer` y `remote_user_pk` (Phase 5+ no shipped todavía). |
| Live TV stream proxying (Phase 6) | `iptv/transmux.go::TransmuxManager.StartLocked` — federation viewers como readers adicionales en la sesión compartida (Phase 6+ no shipped). |
| Error kinds | `domain/errors.go` extendido con `ErrPeerNotFound`, `ErrPeerKeyMismatch`, `ErrPeerRevoked`, `ErrPeerUnauthorized`, etc. Mapping a HTTP es uniforme con resto del API. |

Lo único **realmente nuevo**:

1. `internal/federation/identity.go` — keypair Ed25519 + fingerprint hex/words. Necesita ser fresh code porque el formato (fingerprint groups, BIP-39 wordlist) es específico al threat model SSH-style del feature.
2. `internal/federation/invite.go` — códigos `hp-invite-<10-char>` con TTL + canonical form. No hay primitivo equivalente.
3. `internal/federation/manager.go` — orquestador. Wraps repo + clock + identity + ratelimit + auditor.
4. `internal/federation/middleware.go::RequirePeerJWT` — chi middleware. Reusa el shape de `auth.RequireUserJWT` pero distinto suficiente que generic-ifyingsería abstracción prematura.
5. `internal/federation/audit.go` — async audit writer con queue + batch flush. Específico del feature; el audit log local de usuarios funciona distinto (sync, less volume).

### Consecuencias

**Positivas**

- Surface nueva ~3000 LOC vs. estimación inicial de 8000+. La diferencia es código que NO se escribió porque ya existía.
- Capabilities, hwaccel, transcode budget, idle reaper — todo aplica AUTOMÁTICAMENTE a streaming federation (Phase 5+) sin cambios. Un peer A streaming desde peer B usa el mismo `MaxReencodeSessions` cap que un usuario local.
- Audit log + ratelimit + JWT validation share testing patterns con auth — el patrón de `withClock` injection, in-memory fakes, AssertJSON helpers se reusa.
- Observability gratis: `event.Bus` ya tiene SSE downstream, así que admin UI live-updates al pair/revoke sin poll.
- Bugfixes en primitives (mejorar `imaging.SafeGet`, refinar token bucket) benefician federation sin tocar federation code.

**Negativas / trade-offs**

- **Acoplamiento de versiones**: actualizar `auth/jwt.go` para soportar EdDSA forzó schema de claims updates en federación al mismo tiempo. Trade-off menor pero real — los packages no son independientes.
- **`federation/ratelimit.go` no fue genericified contra `auth/ratelimit.go`**: ~50 LOC duplicados. Decisión consciente: el coste de un generic abstraction (interface design, refactor de tests, pérdida de claridad en cada call site) excede el de mantener dos copias paralelas para un total de ~100 LOC.
- **El repo `internal/db/federation_repository.go` no es sqlc todavía** (ADR-001 dice sqlc-only). Ship-first decision: la urgencia de cerrar Phases 1-4 ganó sobre la consistencia. Backfill tracked en deuda P3.
- **Surface API peer-to-peer crece sin OpenAPI spec** (ver P1.5). Para un feature donde la promesa es "tu server habla con otros HubPlays" o "alguien podría escribir un cliente Kotlin/Swift que consuma esta API", la falta de un schema versionado se siente más urgente que para los endpoints user-facing.

### Alternativas descartadas

- **Implementación 100% nueva en `federation/` sin reusar nada**: rechazado. Habría requerido reimplementar JWT signing, ratelimit, SSRF guards. Cada uno con su propia matriz de bugs sutiles.
- **Microservicio separado** (`hubplay-federation-daemon` con su propia DB y proceso): rechazado. Self-hosted target user es alguien con un servidor; pedirles operar dos procesos es violar el "una imagen Docker, un binario" del proyecto. Y la federación necesita acceso a `items`, `libraries`, `users` — separar la DB es non-trivial.
- **Genericizar `auth.JWTValidator` para soportar EdDSA + HS256 detrás de una interface**: rechazado por ahora. La superficie de tests crecía 2x sin claridad neta. Si en el futuro Phase 7 introduce más algoritmos (Ed25519+SHA256, etc.), revisitar.
- **Mantener `federation/ratelimit.go` y `auth/ratelimit.go` como un solo paquete reutilizado**: rechazado. Las semánticas divergen sutilmente — auth ratelimit es per-IP con burst muy bajo (login attempts), federation es per-peer con bursts permisivos (catalog sync). Una abstracción unificada habría llevado opciones que oscurecen call sites.
- **Auditor síncrono in-line** (write per-request en `RequirePeerJWT` antes de devolver): rechazado. El SQLite write añade ~5-10ms al hot path peer-to-peer; el audit es por definición no-critical; mejor async + tolerate-drop documentado.
- **Persistencia del rate-limit state cross-restart** (`federation_rate_limit_state` table existe pero no se usa): pospuesto a Phase 2.5+. Self-hosted con un puñado de peers es ok perdiendo el estado en restart; un peer hostil que aprovechaba el restart para una nueva burst window queda frente a un cap más tarde por el resto del minuto.

---

## ADR-013 — Federation Phase 5: streaming proxy + sintetic stream-manager identity

- **Fecha**: 2026-04-30
- **Estado**: Aceptado
- **Supersede**: extiende ADR-012 (federación como capa de integración) con la pieza de streaming.
- **Contexto de descubrimiento**: implementación Phase 5 según `docs/architecture/federation.md` §8.

### Contexto

Phase 5 cierra la promesa de federación: usuario A pulsa play en peli del peer B, video llega. Diseño §8 fija la regla "user solo habla con A" — no direct client→B. Razones recapituladas: privacidad de IP del user, A puede aplicar caps de bandwidth, NAT de B funciona mientras A pueda alcanzarle. La pregunta arquitectural era cómo modelar la sesión de streaming en el origin sin reescribir `stream.Manager`.

### Decisión

**Reuso completo de `stream.Manager` con identidad sintética en lugar de un `peer-stream` paralelo.** El handler peer-side llama `stream.Manager.StartSession` con `userID = "rmt-{peerID}-{remoteUserID}"`. Tres consecuencias concretas:

1. **Dedupe**: el manager keya por `(userID, itemID, profile)`. Mismo peer-user pidiendo mismo item dos veces obtiene la misma sesión — comportamiento idéntico al de un user local con dos pestañas en el mismo player. Sin código nuevo.

2. **Concurrencia y transcode budget**: federation streams compiten en el mismo `MaxTranscodeSessions` global con local users. Si tu CPU está al 100% transcoding las pelis de tu hijo, una request del peer-user de Pedro encuentra el cap igualmente. Justo: tu transcode budget = tu CPU.

3. **HW accel + idle reaper + decision waterfall**: heredados sin tocar. La decisión `Decide()` corre con las capabilities forwarded del cliente original; el idle reaper limpia sesiones peer-originated después de 5 min sin actividad igual que las locales.

**Capa adicional `peerStreamGate` con cap per-peer**, separada del global. Razón: un peer hostil con un cliente que crashea-y-respawnea podía abrir N sesiones que TODAS pasan el dedupe del manager (cada una con un `remoteUserID` distinto fabricado). Sin un counter per-peer, eso drena el budget global. El cap per-peer (default 3) bounds el blast radius.

**Master playlist URL rewriting** se queda como string transform en `internal/federation/`. La función pura sobre strings no necesita HTTP context y será reusable cuando Phase 6 (Live TV peering) traiga la misma necesidad de proxiar HLS.

**`peer_item_progress` separado del local `user_data`**. Watch progress de "MI user X en peli REMOTA Y" se persiste en una tabla aparte. Razón: la PK del local user_data es `(user_id, item_id)` donde item_id es UN local UUID. Un peer item id no existe en local items. Tres opciones para encajarlo:

- (a) Añadir columnas `peer_id` + `is_remote` al user_data. Polluye un schema bien definido.
- (b) Cachear el peer item localmente con un fake row + tabla aparte. Romper el contrato "items son content que tu has scaneado".
- (c) Tabla nueva con su propia PK `(user_id, peer_id, remote_item_id)`. **Elegida**.

(c) deja user_data intocada, separa los dos conceptos cleanly, y permite que un futuro Continue Watching unificado haga UNION explícito si el producto lo necesita.

### Consecuencias

**Positivas**:
- ~3000 LOC menos vs reimplementar transcoding peer-side (HLS, ffmpeg invocation, profile management, idle reap...). Solo orquestación.
- Capability forwarding "gratis": `stream.Decide()` recibe los caps reales sin que el código de federación los entienda. Tu Chromecast hace DirectPlay de la peli de Pedro si la podía hacer en local.
- HW accel funcionará automáticamente cuando se enable en server B — la decisión se toma con las flags del config local de B, no del peer A.

**Negativas / trade-offs**:
- Federation streams **compiten** con local users por el global cap. En servidores muy cargados (homelab con 100+ items reproduciendo), un peer puede hacer que un local user reciba "transcode busy". El cap per-peer mitiga el escenario hostil, no el escenario "carga real". Si llega a doler, future ADR puede partir el budget en dos pools (local-reservados + peer-disponibles).
- Direct-Play vía federación **no soportado en v1**. El `directUrl` siempre se devuelve null para peer streams; un item que SERÍA direct-playable cae a HLS via DirectStream. Pierde un pelín de calidad y CPU. Phase 5.5 puede añadir un endpoint que range-streamea bytes originales del peer.
- `peerStreamGate` es 100% in-memory. Restart del server pierde los counters; un peer hostil que aprovechara el restart para una nueva burst no encuentra resistencia hasta que el siguiente proceso boot pille la primera request. Aceptable para self-hosted (servidores no se restartean cada minuto).

**Específicas del enfoque "rewrite master URL"**:
- Variant manifests siguen usando URLs RELATIVAS, que el browser resolve contra la URL del playlist (= proxy local). No necesitamos rewriting recursivo. Simple.
- Si el origin emitiera URLs absolutas en el variant playlist (ahora mismo no lo hace), el rewrite sería más invasivo. El test pin captura el formato actual; si cambia upstream, el test rompe ruidoso.

### Alternativas descartadas

- **Direct client→peer-B (no proxy)**: violaría el contrato de privacidad del §8 (B vería la IP del user de A) Y rompería el caso NAT donde B está detrás de un firewall. Rechazado.
- **CDN-style 302 redirect del A al user, apuntando al peer**: simplifica el proxy pero rompe lo mismo (privacidad + NAT). Rechazado.
- **Implementar un nuevo `peer.StreamManager` paralelo a `stream.Manager`**: 5x el código, divergencia inevitable, dos sitios donde ajustar HW accel cuando se mejora. Rechazado en favor de identidad sintética.
- **Persistir `peerStreamGate` counters en SQLite**: 99% de los restarts en producción son updates rolling con 0 sesiones activas; persistir solo paga por el escenario de "crash with active sessions" que es raro. Aceptamos la ventana.
- **Progress en `user_data` con un row sintético "peer:..." como item_id**: discutido en sección de decisión; rechazado por polluir un schema con FKs explícitas a items.id.
