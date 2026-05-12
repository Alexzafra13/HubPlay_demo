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
  sqlite/           ← fuente de verdad (41 ficheros)
  postgres/         ← traducciones paralelas (en construcción)

internal/db/
  queries/          ← queries SQLite, input sqlc
  queries-postgres/ ← queries Postgres, input sqlc
  sqlc/             ← bindings SQLite generados
  sqlc_pg/          ← bindings Postgres generados (cuando se uncomment sqlc.yaml)
  *_repository.go   ← repos — interface igual, constructor con branch
```

`sqlc.yaml` tiene dos bloques `sql`, uno por engine. El bloque de
postgres está actualmente comentado hasta que `queries-postgres/`
tenga ficheros reales (si no, `sqlc generate` falla en dir vacío).

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
| `BLOB` | `BYTEA` | si aparece | Ninguno en HubPlay hoy. |

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

### Sesión B — Schema part 1 (~1 día)

- [ ] Traducir `migrations/sqlite/002_fts_search.sql` a tsvector +
      GIN (la más dura)
- [ ] Traducir `003_add_indexes.sql` hasta `019_app_settings.sql`
- [ ] Correr `sqlc generate` y confirmar que ambos paquetes emiten
      interfaces `Querier` idénticas (se ven los errores cuando no)
- [ ] Smoke test: contenedor Postgres via testcontainers, `goose up`
      contra `migrations/postgres/`

### Sesión C — Schema part 2 (~1 día)

- [ ] Traducir migraciones 020 a 041 (federation, IPTV, household)
- [ ] Smoke test TODAS las migraciones via testcontainers
- [ ] Job CI que corre migraciones para SQLite (existente) Y Postgres

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

### Sesión G — CLI `migrate-db` + UI admin (~2 días)

- [ ] Subcomando CLI que use `pgloader` (pull `dimitri/pgloader` o
      vendor un binario estático)
- [ ] Sección "Base de datos" en el panel admin con "Probar conexión"
      + instrucciones copy-paste para el comando CLI
- [ ] Marker file post-migración + banner "Migración completa" en el
      panel que desaparece cuando se quita el marker

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

| Sesión | Horas | Acumulado |
|---|---:|---:|
| A — Foundation (hecha) | 3 | 3 |
| B — Schema parte 1 | 7 | 10 |
| C — Schema parte 2 | 7 | 17 |
| D — Queries | 7 | 24 |
| E — Repos | 14 | 38 |
| F — Wiring | 3 | 41 |
| G — Migration CLI + UI | 14 | 55 |
| H — Worker pool | 21 | 76 |
| I — Extensiones / índices | 7 | 83 |
| J — Production hardening | 7 | 90 |

**~90 horas de trabajo enfocado**, repartibles en 10 sesiones. No
tenemos que hacerlas todas — sesiones A a F entregan "modo
Postgres funcional", y G–J son la capa de producción.

v1 pragmático = A a F + G (UX de migración). H–J pueden venir
después.
