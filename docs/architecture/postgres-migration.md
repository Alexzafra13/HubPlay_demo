# PostgreSQL backend — notas de la migración dual-dialect

Memoria interna del trabajo dual SQLite/PostgreSQL. Cuando retomemos
el tema en una sesión futura, leer esto primero para no repetir
decisiones.

---

## Decisión arquitectural en una frase

Mantenemos SQLite como backend por defecto (zero-ops self-hosted
sweet spot). PostgreSQL es el opt-in para escalar usuarios
concurrentes. Ambos backends acceden por interfaces Go idénticas; la
implementación se elige al boot vía `cfg.Database.Driver`. sqlc
genera dos paquetes paralelos (`internal/db/sqlc/` para SQLite,
`internal/db/sqlc_pg/` para Postgres). El puente de drivers es
`pgx/v5/stdlib` para mantener `*sql.DB` en toda la base sin
reescribir cada repo a la API nativa de pgx.

---

## Layout en disco

```
migrations/
  sqlite/           ← fuente de verdad (41 ficheros) ✅
  postgres/         ← traducciones paralelas (41 ficheros) ✅ smoke-tested

internal/db/
  queries/          ← queries SQLite, input sqlc ✅
  queries-postgres/ ← queries Postgres, input sqlc — vacío (Sesión D)
  sqlc/             ← bindings SQLite generados ✅
  sqlc_pg/          ← bindings Postgres generados (cuando Sesión D plante queries)
  *_repository.go   ← repos — Sesión E refactor a interface dual
```

`sqlc.yaml` tiene dos bloques `sql`, uno por engine. El bloque de
postgres está actualmente comentado hasta que `queries-postgres/`
tenga ficheros reales (si no, `sqlc generate` falla en dir vacío).
Descomentar en Sesión D al colocar el primer query traducido.

---

## Reglas de traducción — schema (.sql en migrations/)

### Tipos

| SQLite | PostgreSQL | Cuándo | Nota |
|---|---|---|---|
| `DATETIME` | `TIMESTAMPTZ` | siempre | TZ-aware es el default seguro. El código lee `time.Time` en ambos. |
| `BOOLEAN DEFAULT 0` | `BOOLEAN DEFAULT FALSE` | siempre | SQLite guarda bool como 0/1; Postgres exige keyword. |
| `BOOLEAN DEFAULT 1` | `BOOLEAN DEFAULT TRUE` | siempre | Idem. |
| `REAL` | `DOUBLE PRECISION` | siempre | El REAL de SQLite es float de 8 bytes; equivalente. |
| `INTEGER` (pequeño) | `INTEGER` | siempre | int de 32 bits. |
| `INTEGER` (tick / size / grande) | `BIGINT` | cuando es campo `*_ticks` o `size` | SQLite INTEGER es dinámico, Postgres fijo. Cualquier valor que pueda superar `2^31` (ticks de pelis, tamaños de archivo) DEBE ser BIGINT. |
| `TEXT` | `TEXT` | siempre | Idéntico. |
| `BLOB` | `BYTEA` | si aparece | Sólo en `server_identity.private_key` / `public_key` y `federation_peers.public_key` (Ed25519, 32 bytes) — migración 020. |

**Regla rápida de escaneo**: en cada `CREATE TABLE`, busca columnas
con nombre `*_ticks`, `size`, `duration_ticks`, `position_ticks`,
`bytes`. Esas tienen que ser `BIGINT` en Postgres.

### Constraints + defaults

| SQLite | PostgreSQL |
|---|---|
| `PRIMARY KEY` | igual |
| `PRIMARY KEY (a, b)` | igual |
| `REFERENCES … ON DELETE CASCADE` | igual |
| `REFERENCES … ON DELETE SET NULL` | igual |
| `UNIQUE(a, b)` | igual |
| `CHECK (col IN ('a','b'))` | igual |
| `DEFAULT CURRENT_TIMESTAMP` | igual |
| `STRICT` (modificador de tabla) | quitar — Postgres es strict por defecto |
| `WITHOUT ROWID` | quitar |

### Índices

`CREATE INDEX …` igual. **Oportunidad** (NO hacer a ciegas — esperar
al perf pass):
- Índices parciales: `WHERE deleted_at IS NULL` etc.
- Índices BRIN para columnas time-series (epg_programs.start_time,
  federation_audit_log.created_at) — mucho más pequeños que B-tree.
- Índices covering con cláusula INCLUDE.

Estas optimizaciones NO van en la traducción 1:1. Van en una PR de
performance separada cuando el dual-dialect compile.

### FTS (full-text search)

`migrations/sqlite/002_fts_search.sql` usa la extensión `fts5` de
SQLite. **La divergencia más grande**. El equivalente Postgres es
`tsvector` + índice GIN. Misma idea (buscar texto, devolver items
rankeados por relevancia) pero API distinta.

Para la traducción inicial:
1. La versión Postgres de `002_fts_search.sql` NO debe usar `fts5`
   (no existe). Reemplazar por:
   ```sql
   ALTER TABLE items ADD COLUMN search_vector tsvector;
   CREATE INDEX idx_items_fts ON items USING GIN(search_vector);
   -- Más un trigger que populate search_vector desde title +
   -- original_title + (metadata.overview con join) en insert/update.
   ```
2. La query SearchItems necesita variante Postgres con:
   `WHERE search_vector @@ plainto_tsquery('english', $1)
    ORDER BY ts_rank(search_vector, plainto_tsquery('english', $1)) DESC`.
3. Multilingüe: considerar `tsvector('simple', ...)` para tolerancia
   a acentos, o cargar diccionarios por idioma.

Es la migración más dura. Reservar medio día.

### UPSERT

| SQLite | Postgres |
|---|---|
| `INSERT OR IGNORE INTO t (a,b) VALUES (?,?)` | `INSERT INTO t (a,b) VALUES ($1,$2) ON CONFLICT DO NOTHING` |
| `INSERT OR REPLACE INTO t (a,b) VALUES (?,?)` | `INSERT INTO t (a,b) VALUES ($1,$2) ON CONFLICT (a) DO UPDATE SET b = EXCLUDED.b` |
| `INSERT … ON CONFLICT(col) DO UPDATE SET …` | igual — ambos lo soportan |
| `INSERT … ON CONFLICT(col) DO NOTHING` | igual |

**Cuidado**: Postgres exige nombrar EXPLÍCITAMENTE el target del
conflicto (constraint o lista de columnas). El `INSERT OR IGNORE` de
SQLite lo infiere de cualquier violación. Si una fila viola dos
constraints, el comportamiento difiere — ser deliberado al traducir.

### Fechas

| SQLite | Postgres |
|---|---|
| `datetime('now', '-7 days')` | `NOW() - INTERVAL '7 days'` |
| `strftime('%Y-%m-%d', col)` | `TO_CHAR(col, 'YYYY-MM-DD')` |
| `SUBSTR(col, 1, 10)` | igual — ambos soportan SUBSTR |
| `EXTRACT(YEAR FROM col)` | igual, pero más idiomático en Postgres |

Las queries de home/system con buckets diarios (en
`internal/api/handlers/system.go:StreamActivity` y trending) usan
fecha-aritmética. Cuidado al traducir — hay margen para drift
semántico silencioso aquí.

### Placeholders

| SQLite | Postgres |
|---|---|
| `?` (posicional, anónimo) | `$1`, `$2`, … (posicional, numerado) |

sqlc lo maneja automáticamente según `engine`. No escribimos `$N` a
mano; escribimos `?` y sqlc reescribe para postgres. **Sin embargo**:
si reusas el mismo parámetro dos veces, SQLite repite `?` mientras
Postgres referencia el mismo `$N`. sqlc lo gestiona vía
`sqlc.arg(name)` si hace falta claridad.

---

## Reglas de traducción — queries (.sql en queries-postgres/)

Para cada fichero en `internal/db/queries/`, crear un hermano en
`internal/db/queries-postgres/` con el mismo nombre. Los nombres
de función emitidos por `-- name: FunctionName :return-shape` DEBEN
ser idénticos para que las interfaces `Querier` generadas coincidan.

La mayoría de queries serán **casi idénticas** entre los dos
dialectos — solo sustituir UPSERTs y fecha-math donde toque. Hacer
diff entre los dos ficheros; si difieren más de ~5 líneas,
verificar dos veces la semántica.

### Lo que SÍ requiere atención por query

1. **Parámetros boolean**: SQLite acepta `0/1`; Postgres quiere
   `TRUE/FALSE`. Sqlc suele resolverlo vía tipos Go.
2. **Orden con NULL**: `ORDER BY col` pone NULLs LAST en Postgres,
   FIRST en SQLite. Añadir `NULLS LAST` explícito donde importe.
3. **`COALESCE` con strings**: SQLite suele devolver string vacío
   desde `COALESCE(col, '')`; Postgres exige tipado consistente.
   No-op para las queries actuales, pero saberlo.
4. **`LIMIT` con subqueries**: Postgres exige paréntesis en
   contextos que SQLite no.
5. **Holdouts raw SQL**: `internal/db/library_repository.go` y
   varios otros tienen SQL raw bypaseando sqlc por el bug del
   parser 1.31.1 (ver `docs/memory/architecture-decisions.md`).
   Necesitan DOS versiones: SQLite (existente) y Postgres.
   El branching va en el método del repo.

---

## Reglas de traducción — capa repo (Go)

Hoy cada repo es un struct con `*sql.DB` y `*sqlc.Queries`
embebidos. Patrón post-migración:

### Opción A — interface interna (recomendada)

```go
type settingsQueries interface {
    GetSetting(ctx context.Context, key string) (string, error)
    SetSetting(ctx context.Context, args SetSettingParams) error
    // ...
}

type SettingsRepository struct {
    db *sql.DB
    q  settingsQueries  // *sqlc.Queries o *sqlc_pg.Queries
}

func NewSettingsRepository(driver string, db *sql.DB) *SettingsRepository {
    r := &SettingsRepository{db: db}
    if driver == "postgres" {
        r.q = sqlc_pg.New(db)
    } else {
        r.q = sqlc.New(db)
    }
    return r
}
```

La interface se escribe a mano (vive en el fichero del repo). Ambos
tipos `Queries` generados la satisfacen porque las signaturas son
idénticas (lo aseguramos manteniendo los nombres de query + return
shape iguales entre dialectos). Esta es la opción limpia.

### Opción B — generics

Más elegante en teoría pero complicado con method sets sobre
pointer types. No.

---

## Plan multi-sesión

### Sesión A — Foundation ✅ HECHA

- [x] Añadido engine postgres a `sqlc.yaml` (staged comentado)
- [x] `migrations/postgres/001_initial_schema.sql` traducido
- [x] `internal/db/queries-postgres/` directorio creado
- [x] `github.com/jackc/pgx/v5` añadido a `go.mod`
- [x] Esta guía escrita

### Sesión B — Schema part 1 ✅ HECHA

- [x] Traducida `002_fts_search.sql` a tsvector + GIN + trigger pl/pgsql
- [x] Traducidas `003` a `019` con las reglas mecánicas del doc
- [x] Smoke test: contenedor `postgres:16-alpine` + `goose up` ejecuta
      las 19 migraciones sin errores. Índice GIN tsvector verificado.

### Sesión C — Schema part 2 ✅ HECHA

- [x] Traducidas migraciones `020` a `041` (federation core, audit,
      shares, cache, device_codes, hot-path indexes, studios,
      collections, profiles, segments, refresh-hash, household,
      primary admin)
- [x] Smoke test: las 41 migraciones aplicadas en secuencia contra
      Postgres 16 real → "successfully migrated database to version: 41"
- [x] 46 tablas creadas (45 nuestras + goose_db_version)
- [ ] Job CI que corre migraciones para SQLite (existente) Y Postgres
      → diferido a Sesión J (production hardening)

### Sesión D — Query files (~1 día)

- [ ] Copiar todo de `queries/` a `queries-postgres/`
- [ ] Aplicar traducciones UPSERT + fecha-math + booleans según
      las tablas de arriba
- [ ] `sqlc generate` y confirmar paridad de interface
- [ ] Test que `len(sqlc.Querier methods) == len(sqlc_pg.Querier methods)`

### Sesión E — Repos (~2 días)

- [ ] Refactorizar cada repo a la opción A. 14 repos: settings,
      users, sessions, libraries, items, media_streams, images,
      metadata, user_data, home, providers, federation, channels,
      epg_programs.
- [ ] Actualizar `main.go` para construir repos con el driver
- [ ] Los tests SQLite existentes deben pasar sin cambios
- [ ] Añadir `_postgres_test.go` paralelos con testcontainers para
      los repos críticos (settings, users, items, user_data)

### Sesión F — Wiring + pgx (~medio día)

- [ ] En `main.go`, cuando `driver == "postgres"`, registrar el
      driver `pgx/v5/stdlib` y usar el DSN
- [ ] Configurar `pgxpool` (MaxConns, MinConns, MaxConnLifetime)
      desde un nuevo bloque de config
- [ ] Health-check probe en panel admin que distinga SQLite ping vs
      Postgres ping
- [ ] Documentar el formato del DSN en `hubplay.example.yaml`

### Sesión G — Migración plug-and-play con SSE (~2-3 días)

Decisión arquitectural (2026-05-12): la migración se hace 100%
desde el panel admin, sin terminal, sin polling. Usa SSE con
auto-reconnect nativo para sobrevivir al restart del contenedor.

#### Plan en cuatro sub-fases

**G.1 — CLI base (no UI todavía)**
- [ ] Subcomando `hubplay migrate-db --from … --to …` que invoca
      `pgloader` como subproceso (vía docker.sock si está
      disponible, o exec local si pgloader está en PATH)
- [ ] State machine en `/config/migrations/{id}.json` con fases:
      `starting` → `pgloader-running` → `schema-verified` →
      `restarting` → `complete` (o `failed` con last_error)
- [ ] Marker file `/config/migrations/active` apunta al id en curso
- [ ] Tests integración: docker-in-docker, postgres:16-alpine,
      verifica round-trip de datos sample

**G.2 — Endpoint SSE para progress + reconnect-survival**
- [ ] `POST /admin/system/db/migrate` → 202 Accepted con `migration_id`
- [ ] `GET /admin/system/db/migrate/events?id={id}` SSE stream
- [ ] Pre-restart: stream live progress del subproceso pgloader
      (tail stdout, parse "X of Y rows copied" lines)
- [ ] Post-restart: el binario nuevo lee el marker en boot;
      cuando llega la reconexión SSE replaya el estado completo
      del state machine + emite `complete` event
- [ ] **No polling** — EventSource auto-reconnect del browser cubre
      el window de restart (el protocolo lo hace nativo, mismo patrón
      que `useUserDataSync` ya en uso)

**G.3 — Auto-detect docker.sock + capability flag**
- [ ] Al boot, detectar si `/var/run/docker.sock` está mounted +
      accesible. Exponer como `system.migrate_capability`:
      `in_process` o `advisory`
- [ ] Panel admin renderiza según capability:
      - `in_process` → botón "Migrar ahora" con SSE progress live
      - `advisory` → bloque copy-paste con el comando CLI
- [ ] Mismo backend, mismo wire shape SSE — solo el endpoint activable
- [ ] docker-compose ejemplo en `deploy/` con `docker.sock` opt-in
      mounted + warning de security

**G.4 — Read-only window + rollback automático**
- [ ] Mientras pgloader copia, API rechaza POST/PUT/DELETE con 503
      y `Retry-After: 60`. Reads contra SQLite siguen funcionando.
- [ ] Si pgloader falla mid-run, estado se queda `failed`, server NO
      se mata, sigue en SQLite. Panel muestra error + tail logs.
- [ ] Idempotencia: si el browser se cierra y vuelve mientras hay
      migración en curso, panel detecta marker activo, ofrece
      "Ver progreso" y se engancha al SSE existente.

**G.5 — Migración programada (ventana de mantenimiento)**

El admin no siempre quiere migrar AHORA. Algunos preferirán "esta
noche a las 3am" para no afectar a la familia. Añadimos esa opción
con un campo extra en el state machine, sin reabrir el resto del
diseño:

- [ ] Panel admin: dropdown junto al botón "Migrar":
      `[Ahora ▾]` con opciones `Ahora`, `Programar...` (date+time picker)
- [ ] Cuando es programada, `POST /admin/system/db/migrate` con body
      `{"scheduled_at": "2026-05-13T03:00:00Z"}` → 202 + migration_id,
      estado en marker: `scheduled`
- [ ] Background goroutine (mismo retention runner pattern) tick cada
      1 min: si hay marker con `scheduled` y `scheduled_at <= now`,
      flipea a `starting` y dispara el flujo normal de G.1
- [ ] Email/notificación in-app al admin 5 min antes del kickoff
      (banner persistente en el panel; sin email para no añadir
      dependencia SMTP)
- [ ] Cancelable: si el admin cambia de opinión, botón "Cancelar"
      borra el marker `scheduled` antes del kickoff
- [ ] Persistente entre restarts: si el servidor se reinicia por
      otra razón antes de la hora programada, al boot lee el marker
      `scheduled` y la goroutine sigue esperándola
- [ ] Auto-detect "horas valle": cuando el admin abre el dropdown,
      sugerimos un horario basado en las últimas 24h de actividad
      ("Sin sesiones activas entre las 2 y las 6 AM — ¿programar
      para las 03:00?"). Opcional, calidad de UX.

Coste extra sobre G.1-G.4: ~3h. Lo añadimos al estimado de Sesión G.

#### UX final que verá el operador

```
Panel → Base de datos
Pulsa "Probar PostgreSQL" → ✓
Pulsa "Migrar ahora"
Modal: "Reiniciará el servidor. ~30s para 130MB."
[Confirmar]
Spinner + barra de progreso live (SSE):
  "Copiando users → users... 12/45 tablas"
  "Copiando items → items... 28/45 tablas"
  "Verificando schema..."
  "Reiniciando servidor..."
[Spinner queda — SSE reconectando]
"✓ Migración completa. Conectado a PostgreSQL."
```

Cero terminal. Cero pgloader docs. Cero polling. Reinicio implícito.

### Sesión H — Worker pool (~3 días)

- [ ] Nuevo `internal/workerpool/` con el patrón pipeline-stages
- [ ] `AutoTuneWorkers(driver)`: SQLite → 1 writer; Postgres → cores
- [ ] Migrar scanner / image processor / provider fetcher a etapas
- [ ] Canal `LISTEN/NOTIFY` para eventos cross-instance cuando hay
      Postgres (no-op silencioso en SQLite)

### Sesión I — Extensiones + índices (~1 día)

- [ ] Migración que corra `CREATE EXTENSION IF NOT EXISTS pg_trgm`
      y añada índice GIN en items.title para búsqueda fuzzy
- [ ] Migración que active `pg_stat_statements` (el operador debe
      añadirlo a `shared_preload_libraries`; documentarlo)
- [ ] Añadir índices parciales + BRIN identificados en el perf audit
      (solo después de Sesión H)

### Sesión J — Producción (~1 día)

- [ ] Ejemplo de docker-compose con `postgres_exporter` para
      Prometheus
- [ ] Backup basado en `pg_dump` que reemplace el endpoint de backup
      SQLite cuando el driver es Postgres
- [ ] Doc de operaciones: cómo subir de versión Postgres con
      seguridad, cómo restaurar desde `pg_dump`, tunings recomendados
      de `postgresql.conf` para self-hosted

---

## Disciplina de mantenibilidad — evitar drift entre dialectos

El mayor riesgo del dual-dialect a largo plazo es que SQLite y
Postgres se desincronicen silenciosamente: alguien añade una
migración SQLite, olvida la gemela Postgres, los repos compilan,
los tests SQLite pasan, y la rama de Postgres queda rota durante
semanas hasta que alguien intenta migrar.

Las cuatro defensas que tenemos / vamos a tener:

### 1. Convención de gemelas obligatorias

Toda migración SQLite tiene su gemela Postgres con el MISMO número
y nombre. Si añades `migrations/sqlite/042_x.sql`, debes añadir
`migrations/postgres/042_x.sql`. Sin excepciones, ni siquiera para
migraciones triviales.

Lo verificamos con un test (a montar en Sesión J):

```go
// internal/db/migrations_test.go
func TestMigrationParity(t *testing.T) {
    sqliteFiles := mustListSQL(t, "migrations/sqlite")
    pgFiles := mustListSQL(t, "migrations/postgres")
    if !slices.Equal(sqliteFiles, pgFiles) {
        t.Fatalf("migration filename drift: sqlite=%v postgres=%v",
            sqliteFiles, pgFiles)
    }
}
```

Falla en CI si alguien commitea solo una mitad.

### 2. Schema-fingerprint test (Sesión J)

Smoke test que arranca un Postgres + SQLite ambos vacíos, corre
`goose up` contra cada uno, extrae el shape de las tablas
(`information_schema.columns` en Postgres, `pragma table_info` en
SQLite), y compara. Errores tolerados:

- Tipos equivalentes (BIGINT vs INTEGER cuando ambos almacenan
  ticks): el test conoce un mapeo manual `equivalents.go` que
  documenta los pares aceptados.
- Defaults equivalentes (`1` vs `TRUE` para boolean): igual.

Falla en CI si una columna existe en un dialecto y no en el otro,
o si un PK / UNIQUE / FK referencia diferentes columnas.

### 3. Query-parity test (después de Sesión D)

Test que carga ambos paquetes generados sqlc + sqlc_pg y compara
los method sets de `Querier`. Falla si los nombres / signaturas
difieren.

```go
func TestQuerierMethodParity(t *testing.T) {
    sqliteMethods := reflectMethods(reflect.TypeOf((*sqlc.Querier)(nil)).Elem())
    pgMethods := reflectMethods(reflect.TypeOf((*sqlc_pg.Querier)(nil)).Elem())
    if diff := cmp.Diff(sqliteMethods, pgMethods); diff != "" {
        t.Fatalf("Querier method set drift:\n%s", diff)
    }
}
```

### 4. CI matrix (Sesión J)

```yaml
strategy:
  matrix:
    database: [sqlite, postgres]
```

Cada commit corre TODOS los tests dos veces, una por backend.
Imposible mergear PRs que rompen Postgres pero pasan en SQLite.

### El sistema en una frase

**Convención** (gemelas obligatorias) → **test programático** (parity
checks) → **CI matrix** (ejecución real en ambos backends). Tres
capas. Si una falla, las otras dos cazan el drift.

---

## App de TV — impacto en este trabajo

La app Android TV nativa que estás construyendo no afecta a la
arquitectura dual-dialect en sí (es un cliente que habla HTTP, le
da igual qué hay debajo). Pero hay tres consideraciones reales:

### 1. La migración a Postgres ES BUENA para tener TVs

Cada TV reproduciendo escribe progreso cada ~10 s. En un hogar
con 3 TVs activas:
- SQLite: 3 writes/10s = 0.3 writes/s — fácil
- En un escenario más fuerte (familia extendida + amigos, 8 TVs):
  0.8 writes/s — ya en territorio donde SQLite empieza a apretar

Postgres maneja esto sin pestañear porque los writes a filas
distintas paralelizan. **La app TV es exactamente el caso de uso
que justifica Postgres** para usuarios con >5 dispositivos
activos.

### 2. La ventana read-only durante migración

Mientras pgloader copia (Sesión G.4), la API rechaza writes con
503 + `Retry-After: 60`. Las TVs que estén reproduciendo:
- Reads (segmentos HLS, manifiestos): SIGUEN funcionando contra
  SQLite hasta el restart. Playback no se interrumpe en la fase
  de copia.
- Writes (progress updates): rechazados durante ~30s-2min según
  tamaño DB.
- El reinicio en sí: 5-10s de blip donde nada responde. La app TV
  debe reintentar con backoff exponencial.

**Lo que necesita la TV app**: handler que, ante 503 con
`Retry-After`, ponga el progress en cola local y lo flushee al
volver. Mismo patrón que las apps móviles para conectividad
flaky. El web frontend ya hace algo similar para optimistic
updates.

### 3. El device pairing flow ya existe — no toca

El `/pair` con QR + SSE (commit dcb23cd, sesión 2026-05-11) es
exactamente lo que la TV app debe usar para login inicial. RFC
8628 estándar. La TV muestra el QR + user_code, el operador
escanea desde móvil → `/link?code=…` → aprueba → la TV recibe el
token via su poll a `/auth/device/poll`.

**La TV app no necesita nada nuevo del backend**. El flujo está
diseñado precisamente para clientes sin teclado.

### Cero acoplamiento entre la app TV y este trabajo

Resumen: la app TV es independiente del dual-dialect. La TV
ignora qué DB hay debajo. Lo único que tiene que hacer:
- Implementar el device-code flow (estándar, ya documentado)
- Manejar 503 + Retry-After con backoff
- Reconectar SSE/HTTP tras el window de restart

Esos tres son requisitos de cualquier cliente robusto. No son
"extras por Postgres".

---

## Anti-patterns

1. **No ORMs**: GORM / ent etc. ocultan qué se ejecuta. sqlc fue la
   elección por queries typadas y visibles. Mantenerlo.

2. **No quitar SQLite**: SQLite sigue siendo el sweet spot para
   ~95% de usuarios self-hosted. El dual-dialect hace que Postgres
   esté disponible, NO obligatorio.

3. **No introducir Redis como cache**: Postgres con buenos índices
   + los caches in-process que ya hay son suficientes. Redis añade
   complejidad operacional (otra cosa que backup, monitorizar,
   fallar).

4. **No correr goose contra los dos schemas a la vez**: las
   versiones podrían driftear si una migración solo aplica a un
   dialecto. Usar el driver para elegir el dir de migraciones al
   boot.

5. **No usar la API nativa pgx en repos**: quedarse con
   `database/sql` + `pgx/v5/stdlib` para que el código de repo sea
   dialect-agnostic. Bajar a pgx nativo solo en hot paths
   concretos (worker pool con `COPY FROM` para bulk inserts).

6. **No añadir features Postgres-specific a la ligera**: cada vez
   que se escriba una query que LISTEN/NOTIFYs o usa RETURNING…INTO
   etc., se crea una divergencia que el path SQLite no puede
   seguir. Usarlas solo donde haya valor claro (worker pool, event
   bus entre instancias).

---

## Estrategia de testing

- **Tests SQLite** se quedan como están. Rápidos (in-memory),
  cubren el path por defecto que hit el 99% de instalaciones.
- **Tests Postgres** usan `testcontainers-go`. Más lentos (~2 s/test
  inicial por contenedor) pero cacheados después. Añadir solo para
  repos críticos + migraciones + queries con sintaxis específica.
- **CI** corre ambos. Github Actions matrix:
  ```yaml
  strategy:
    matrix:
      database: [sqlite, postgres]
  ```
- **Tests de migración**: uno por dialecto que corra `goose up`
  desde vacío contra el tree entero. Captura errores de sintaxis
  pronto.

---

## Estimación total

| Sesión | Horas | Acumulado | Estado |
|---|---:|---:|---|
| A — Foundation | 3 | 3 | ✅ HECHA |
| B — Schema parte 1 | 7 | 10 | ✅ HECHA |
| C — Schema parte 2 | 7 | 17 | ✅ HECHA |
| D — Queries (26 ficheros) | 7 | 24 | 🔜 siguiente |
| E — Repos refactor (14 repos) | 14 | 38 | |
| F — Wiring pgx + pool | 3 | 41 | |
| G — Migration plug-and-play (5 sub-fases con scheduling) | 24 | 65 | |
| H — Worker pool con auto-tune | 21 | 83 | |
| I — Extensiones + índices | 7 | 90 | |
| J — Production hardening + CI dual | 7 | 97 | |

**~100 horas de trabajo enfocado** (Sesión G subió a 24h con la
sub-fase G.5 de migración programada). 17h acumuladas.

**v1 mínimo viable = A a F + G**: el operador puede instalar
HubPlay con SQLite, decidir migrar a Postgres desde el panel,
volver a SQLite si algo no convence. **80h restantes.**

H–J (worker pool, extensiones, CI dual) entregan el último 20%
de robustez para deploys medianos/grandes. Pueden esperar a v1.1
sin bloquear el lanzamiento.
