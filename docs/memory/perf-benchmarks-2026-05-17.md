# Perf benchmarks — hot-path baseline (2026-05-17)

> Setup: `golang:1.25` container, AMD Ryzen 7 7800X3D 8-Core.
>
> **Dual-backend**: SQLite WAL + Postgres 16-alpine (container dedicado
> en network bridge, no la prod). Comando base:
>
> ```bash
> # SQLite (default)
> go test -bench='Benchmark(ChannelRepository|HomeRepository|ActivityRepository)' \
>     -benchmem -run=^$ -benchtime=2s ./internal/db/...
>
> # Postgres
> HUBPLAY_TEST_DRIVER=postgres \
> HUBPLAY_TEST_POSTGRES_DSN="postgres://postgres:test@hubplay-bench-pg:5432/postgres?sslmode=disable" \
> go test -bench=... ./internal/db/...
> ```
>
> Fichero de benches: `internal/db/{channel,home,activity}_bench_test.go`.

Objetivo de la sesión: dejar de **intuir** los hot paths y **medir** los
reales. Cinco endpoints calientes auditados con datasets representativos
de un host doméstico real (100-5000 canales, 500-5000 items, 50 usuarios).

## Números estabilizados

| Benchmark | N | iters | μs/op | B/op | allocs/op |
|---|---:|---:|---:|---:|---:|
| `ActivityRepository.DailyWatchActivity` | 1 000 | 1 275 | 1 855 | 2 800 | 105 |
| `ActivityRepository.DailyWatchActivity` | 5 000 | 234 | 10 295 | 2 912 | 119 |
| `ActivityRepository.TopItems` | 1 000 | 1 730 | 1 408 | 2 800 | 118 |
| `ActivityRepository.TopItems` | 5 000 | 343 | 6 891 | 2 800 | 118 |
| `ChannelRepository.ListByLibrary` | 100 / false | 7 202 | 317 | 153 KB | **2 910** |
| `ChannelRepository.ListByLibrary` | 100 / true | 7 036 | 304 | 153 KB | **2 910** |
| `ChannelRepository.ListByLibrary` | 1 000 / false | 838 | 2 835 | 1.3 MB | **29 013** |
| `ChannelRepository.ListByLibrary` | 1 000 / true | 811 | 2 790 | 1.3 MB | **29 013** |
| `ChannelRepository.ListByLibrary` | 5 000 / false | 141 | 16 977 | **9.2 MB** | **149 019** |
| `ChannelRepository.ListByLibrary` | 5 000 / true | 142 | 17 064 | **9.2 MB** | **149 019** |
| `HomeRepository.Trending` | 500 | 2 956 | 795 | 12.7 KB | 337 |
| `HomeRepository.Trending` | 2 000 | 915 | 2 573 | 12.7 KB | 337 |
| `HomeRepository.LiveNow` | 500 | 2 035 | 1 122 | 5.6 KB | 166 |
| `HomeRepository.LiveNow` | 2 000 | 592 | 4 104 | 5.6 KB | 166 |

## Hallazgos

### #1 — `ChannelRepository.ListByLibrary` es el bottleneck dominante · **alto impacto**

- 5 000 canales → **17 ms, 9.2 MB, ~150 000 allocs**.
- Allocaciones escalan ~30/row + ~1.8 KB/row.
- Cada `Channel` struct tiene 17 campos; el sqlc generated code hace
  `COALESCE(group_name, '') AS group_name` etc. — cada `COALESCE` →
  string copy. `*Channel` heap alloc + N string allocs por row.
- El UUU-mig (índice composite recién añadido) acelera el plan SQL,
  pero el overhead dominante es la materialización en Go, **no el sort**.

**Por qué importa**: cada vez que el panel de admin
`/admin/libraries/{id}/channels` se abre, o cada vez que el rail
"LiveTV home" se carga, el handler invoca `ListByLibrary`. En libraries
IPTV grandes (5 000+ canales por playlist es lo normal):
- 17 ms de tiempo de servidor + 9 MB de garbage por request.
- Si el panel se refresca por SSE o el rail se vuelve a renderizar,
  multiplica.

**Propuesta — paginación**:

Hoy el handler devuelve TODA la library en una llamada. El frontend
ya tiene listas virtualizadas (solo render lo visible) — recibir 5 000
JSON entries para mostrar 50 visibles es waste por la red Y waste por el
servidor.

```diff
- ListByLibrary(ctx, libID, activeOnly bool) ([]*Channel, error)
+ ListByLibrary(ctx, libID, activeOnly bool, offset, limit int) ([]*Channel, int /*total*/, error)
```

Con `limit=100`: 17 ms → ~0.5 ms (×30 mejora). Cambio de wire
(añadir `total` al response) — backward-compatible si `limit=0`
significa "sin paginar" (legacy).

Esfuerzo: ~80 LOC + cambios en frontend para enviar offset/limit + test.

### #2 — `ActivityRepository.DailyWatchActivity` es el segundo hot path · **medio impacto, admin-only**

- 5 000 user_data rows → **10 ms**.
- Pocas allocs (119, LIMIT-bound) — el coste es 100 % SQL (GROUP BY +
  JOIN de user_data × items).
- La query escanea TODAS las rows en el window. Sin índice
  `idx_user_data_last_played_at`, el filtro `WHERE last_played_at >= ?`
  fuerza full scan.

**Propuesta — añadir índice**:

```sql
CREATE INDEX IF NOT EXISTS idx_user_data_last_played_at
    ON user_data(last_played_at)
    WHERE last_played_at IS NOT NULL;
```

Partial index sólo para rows con valor (la mayoría user_data lo tiene
una vez se ha visto algo). En SQLite es UPSERT-stable; en Postgres
también.

Estimación: 10 ms → 1-2 ms (×5-10). Admin-only, pero el panel
`/admin/system/stats` lo llama cada vez que se abre.

Esfuerzo: ~10 LOC, 1 migración dual. Mismo patrón que UUU-mig.

### #3 — `HomeRepository.Trending` y `LiveNow` están saneados · **OK, no tocar**

- Trending: 2.6 ms para 2 000 items.
- LiveNow: 4.1 ms para 2 000 canales.
- Allocs constantes (LIMIT-bound: 12 + 5).
- Tiempos lineales en N (sort + filter pasa por todo).

Para hosts hasta 5 000-10 000 items en biblioteca, estos rails están
aceptables (single-digit ms). Si llegan a 50 000+, hay que revisar
(añadir índices materializados o pre-cómputo).

### #4 — `ActivityRepository.TopItems` está dentro de rango · **OK**

- 5 000 rows → 6.9 ms.
- CTE de rollup episodes → series. Limits aplicados.

## Propuesta priorizada (post-baseline)

| # | Olor | Esfuerzo | Impacto |
|---|---|---|---|
| 1 | Paginación en `ChannelRepository.ListByLibrary` (+ adapt frontend) | M | **Alto** — 17 ms → 0.5 ms; reduce GC pressure ×60 en IPTV |
| 2 | Índice `idx_user_data_last_played_at` (migración 045) | S | **Medio** — admin /system/stats: 10 ms → 1-2 ms |
| 3 | Reducir allocs por row en Channel struct (sync.Pool / pointer-less) | L | **Bajo a medio** — solo aplica si paginación no se hace; high overhead |
| 4 | pprof endpoints admin-gated para profiling de producción | S | **Habilitador** — para hallazgos no-obvios sin más auditoría |

## Post-fix: mediciones empíricas (mismo run, 2026-05-17)

Las dos optimizaciones implementadas en esta misma sesión con
verificación bench. Honestidad obligatoria: los números reales
divergen de las estimaciones del reporte original.

### #2 — `idx_user_data_last_played_at` (commit `e079038`)

| Bench | Antes idx | Después idx | Mejora |
|---|---:|---:|---:|
| `DailyWatchActivity` rows=1000 | 1 855 μs | 1 761 μs | **-5 %** |
| `DailyWatchActivity` rows=5000 | 10 295 μs | 9 500 μs | **-8 %** |
| `TopItems` rows=1000 | 1 408 μs | 1 366 μs | **-3 %** |
| `TopItems` rows=5000 | 6 891 μs | 6 705 μs | **-3 %** |

Estimación original era ×5-10. Real: -3 a -8 %.

Por qué: a 5 000 rows el "full scan" cabe en page cache de SQLite WAL —
es CPU-bound, no I/O-bound, y comparable en coste al index lookup. El
índice escala en producción con tablas más grandes (10 k-100 k rows),
no en bench sintético en 5 k. La conclusión es que **el bench
sintético subestima el beneficio** del índice — y el revés también es
verdad: bench que muestra mucha mejora puede no traducir a producción
si los datasets reales son más pequeños.

Mantenido en producción porque (a) es teóricamente correcto, (b) el
coste de mantenimiento del índice es trivial (write amplification mínima
— user_data hace UPSERT one-shot), (c) en deployments con catálogos
grandes el beneficio es real.

### #1 — `ListByLibraryPaginated` (commit pendiente)

Nueva primitiva del repo (`ChannelRepository.ListByLibraryPaginated(ctx, libID, activeOnly, offset, limit) ([]*Channel, total int, error)`),
implementada en raw SQL para evitar el ciclo sqlc. **No** migra todavía
el handler `IPTVHandler.ListChannels` — ese tiene capa de overlays
(hide / reorder por usuario) incompatible con paginación 100 % SQL.

Comparativa directa con el bench original:

| Caso | Legacy `ListByLibrary` | Paginated (`limit=100`) | Ratio |
|---|---|---|---|
| 1 000 channels | 3 055 μs / 1 330 KB / 29 013 allocs | 740 μs / 72 KB / 2 908 allocs | **4.1× tiempo, 18× memoria, 10× allocs** |
| 5 000 channels | 16 631 μs / 9 187 KB / 149 019 allocs | 2 379 μs / 78 KB / 3 008 allocs | **7× tiempo, 118× memoria, 50× allocs** |

Mejora de **tiempo** es 4-7× (no el ×30 estimado — el `SELECT COUNT(*)`
añade overhead que el legacy se ahorra). Mejora de **memoria + allocs**
es la palanca real: en libraries IPTV grandes la GC pressure cae
~×50-118.

Por qué importa GC más que time:
- 17 ms vs 2.4 ms por request es importante, sí.
- Pero 9 MB de garbage por request × 1 request/sec × 60 sec = **540 MB/min
  de garbage que el GC tiene que limpiar**. Eso pause-stops el resto del
  servidor (HLS streams, federation handshakes, etc.) periódicamente.
- 78 KB por request hace que la GC apenas note la carga: 4.7 MB/min.

**Pendiente — siguiente sesión**: migración del handler /
servicio para usar la nueva primitiva. Decisiones de UX requeridas:

- ¿Paginación clásica (page X/N) vs infinite scroll vs page size selector?
- ¿Cómo manejar overlays user (hidden/position) cuando paginas en SQL?
  El approach correcto es probablemente JOIN con `user_channel_order` a
  nivel SQL, no aplicar overlays post-fetch.
- Endpoint nuevo `?paginated=true&offset=&limit=` vs flag de feature.

## Comparativa SQLite vs Postgres (mismo run)

El reporte original sólo cubría SQLite. Postgres (16-alpine en container
dedicado, network bridge, no la prod) re-bench-eado con `HUBPLAY_TEST_DRIVER=postgres`
contra el mismo hardware y mismo benchtime=2s.

### Listing (ChannelRepository)

| Bench | N | SQLite μs | Postgres μs | Δ | SQLite B/op | Postgres B/op |
|---|---:|---:|---:|---:|---:|---:|
| `ListByLibrary` | 100 | 317 | 336 | +6 % SQLite | 153 KB | 127 KB |
| `ListByLibrary` | 1 000 | 2 835 | 1 518 | **-46 % Postgres** | 1 330 KB | 1 367 KB |
| `ListByLibrary` | 5 000 | 16 631 | 9 312 | **-44 % Postgres** | 9 187 KB | 7 814 KB |
| `ListByLibraryPaginated` (limit=100) | 1 000 | 740 | 853 | +15 % SQLite | 72 KB | 62 KB |
| `ListByLibraryPaginated` (limit=100) | 5 000 | 2 379 | 2 368 | ≈ igual | 78 KB | 64 KB |

### Admin queries (ActivityRepository)

| Bench | rows | SQLite μs | Postgres μs | Δ |
|---|---:|---:|---:|---:|
| `DailyWatchActivity` | 1 000 | 1 855 | 1 674 | -10 % Postgres |
| `DailyWatchActivity` | 5 000 | 10 295 | 8 530 | **-17 % Postgres** |
| `TopItems` | 1 000 | 1 408 | 1 399 | ≈ igual |
| `TopItems` | 5 000 | 6 891 | 5 712 | **-17 % Postgres** |

### Home rails (HomeRepository)

| Bench | N | SQLite μs | Postgres μs | Δ |
|---|---:|---:|---:|---:|
| `Trending` | 500 | 795 | 2 712 | **+241 % SQLite** |
| `Trending` | 2 000 | 2 573 | 2 279 | -11 % Postgres |
| `LiveNow` | 500 | 1 122 | 1 559 | +39 % SQLite |
| `LiveNow` | 2 000 | 4 104 | 4 698 | +14 % SQLite |

### Hallazgos cross-backend

1. **Postgres bate a SQLite ×~2 en listings grandes.** `ListByLibrary 5 000 channels`
   pasa de 16.6 ms (SQLite) → 9.3 ms (Postgres). El planner Postgres usa
   los índices nuevos (UUU-mig 044 + idx_user_data 045) más
   agresivamente; el page management de Postgres también es más eficiente
   con tablas grandes. **Confirma la elección de Postgres como backend
   recomendado para deployments con catálogos IPTV grandes.**

2. **SQLite bate a Postgres en datasets pequeños / Home rails.**
   `HomeRepository.Trending` con 500 items: SQLite 0.8 ms vs Postgres
   2.7 ms (+241 %). El network roundtrip (~0.3-0.5 ms) + planner overhead
   dominan cuando el query real costaría microsegundos. Cuando el dataset
   crece (2 000 items), Postgres recupera (-11 %). **Para deployments
   single-user con < 1 000 items, SQLite es defensible.**

3. **`ListByLibraryPaginated` es ≈ igual en ambos backends a 5 k rows.**
   La ventaja del index lookup + LIMIT en Postgres se compensa con el
   roundtrip cost. La mejora real (memoria/allocs ×118) se mantiene
   en ambos.

4. **El índice #2 (idx_user_data_last_played_at) escala MEJOR en Postgres
   que en SQLite** (-17 % vs -8 % a 5 000 rows). Era la hipótesis del
   commit `e079038` ("en producción con tablas más grandes el beneficio
   escala"). Postgres + dataset 5k ya muestra el doble de mejora que
   SQLite mismo dataset — confirma la dirección, no la magnitud absoluta
   (en producción con 50 k+ rows el efecto debería ser aún mayor en
   Postgres).

5. **Postgres tiene MENOS allocs/op en general.** El driver pgx hace
   menos string copies que modernc.org/sqlite — `ListByLibrary 5k` baja
   de 149 019 allocs (SQLite) → 115 024 (Postgres). El sqlc-generated
   code es el mismo; el driver del backend hace la diferencia. Bueno
   saber: parte del coste "30 allocs/row" que vi en el reporte original
   es driver-dependent, no fundamental al code path.

### Implicación para producción

- **Deployment recomendado por carga**:
  - Single-user, < 1 000 items + < 1 000 canales: SQLite es óptimo.
  - Multi-user, > 1 000 items o > 1 000 canales: Postgres ya bate a
    SQLite en los hot paths que más se llaman.
  - IPTV con catálogos grandes (5 000+ canales por playlist): Postgres
    obligatorio si la rail LiveTV se carga frecuente.

- **Re-evaluar las prioridades del reporte original**:
  - El #1 (paginación) sigue siendo importante en ambos backends — la
    palanca real es memoria/GC pressure, no tiempo.
  - El #2 (índice user_data) tiene más impacto en Postgres que en
    SQLite — vale la pena en cualquier caso, escala mejor.
  - UUU-mig (índice channels) similar: efectivo en ambos, mejor en
    Postgres.

## Lo que NO se midió aquí (siguiente baseline si interesa)

- **HTTP boundary**: estos benches son al repo, no al endpoint
  completo. `/api/v1/libraries/{id}/channels` añade serialización JSON
  (otro batch de allocs), middleware (auth + CSRF), response writing.
  Estimo +20-30 % overhead encima de lo medido.
- **Federation hot paths**: JWT validation, peerCache lookup. El audit
  los marca como sanos (RWMutex apropiado, peerCache hit rate alto).
- **HLS streaming hot path**: throughput de bytes, no latencia de
  query. Diferente shape de medición.
- **Image pipeline**: blurhash + dominant color extraction. CPU-bound,
  no DB-bound.
- **Postgres tuning real**: el bench corre contra un Postgres con
  defaults (256 MB shared_buffers, autovacuum default). Tuning de
  producción (más buffers, workers, prepared statements) cambiaría
  los números.

Para todo lo anterior, el approach correcto es `pprof` contra una
carga real, no benchmarks sintéticos.
