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
