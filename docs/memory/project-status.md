# Estado del proyecto

> 🎬 **Sesión 2026-05-13 (rama `claude/objective-swartz-6f9f3c`, 1 commit, Sesión E.11 — los 4 medianos cerrados de un golpe)** — continuando dual-dialect tras los 24 ya mergeados (PRs #260, #261, #264, #265, #266). Pendiente de merge: 1 commit (`363d808`) sobre main.
>
> **Lo entregado en esta sesión**:
>
> Cierra el "Bloque 2" del handoff anterior — los 4 repos sub-250 LOC en un único commit. Mismo perfil que E.10: trabajo mecánico aplicando los gotchas ya conocidos. +592/−197 LOC, 8 ficheros tocados (4 repos + repos.go + 3 test files externos).
>
> **Repos cerrados (orden por LOC)**:
>
> | Repo | LOC | Pattern | Notas |
> |---|---|---|---|
> | Collection | 201 | A + 4 raw pre-rewriteadas | BOOLEAN drift `i.is_available = 1` + `img.is_primary = 1` → predicados truthy. EnsureCollection/List/ListItems/SetItemCollection raw por sqlc 1.31.1 trunc + ON CONFLICT CASE |
> | IPTVSchedule | 206 | A puro | IntervalHours/LastDurationMs INTEGER → int64↔int32 |
> | LibraryEPGSources | 235 | A + tx UpdatePriorities + interface{} | Priority/LastProgramCount/LastChannelCount INTEGER cast. nextPriority acepta int64 e int32 (driver-agnostic). **isUniqueConstraintError extendido para Postgres SQLSTATE 23505** — primer repo que detecta UNIQUE drift entre dialectos |
> | Studio | 242 | A + 2 raw pre-rewriteadas | TmdbID NullInt64↔NullInt32. BOOLEAN drift en ListItemsForStudio |
>
> **Verificación al cierre**:
>
> - `go build ./internal/db/...` clean.
> - `GOOS=linux go build ./...` clean (cross-compile completo del binario).
> - `go test -count=1 ./internal/db/` 10.4s — todos verde.
> - `go test ./internal/auth/ ./internal/scanner/ ./internal/iptv/ ./internal/library/ ./internal/federation/` todos verde.
> - `TestSQLC_GeneratedFilesMatchQueries` verde (drift test).
> - `go vet ./internal/db/... ./internal/scanner/...` clean.
> - `internal/api/...` sigue fallando en host Windows por `syscall.Statfs_t` (preexistente, no del refactor; cross-compile linux confirma que esos paquetes compilan bien).
>
> **Estado al cierre — 26 de 28 repos done, 2 pending (los dos grandes)**:
>
> ✅ **Done** (26): Settings, Users, Sessions, SigningKeys, Library, MediaStreams, Items, UserData, Channels, EPGPrograms, Image, UserPreferences, Chapter, ExternalID, DeviceCode, ChannelWatchHistory, ChannelFavorites, ItemValue, ChannelOverride, Provider, EpisodeSegment, Metadata, People, **Collection (E.11)**, **IPTVSchedule (E.11)**, **LibraryEPGSources (E.11)**, **Studio (E.11)**.
>
> ⏳ **Pending** (2):
>
> | Repo | LOC | Tier | Notas esperadas |
> |---|---|---|---|
> | **Home** | **631** | 🟠 Grande | Queries complejas items + user_data agregados. Access-predicate post-040 ya aplicado |
> | **Federation** | **862** | 🔴 Gigante | MUCHOS raw SQL holdouts. Los `is_active = 1` / `can_browse = 1` / `COLLATE NOCASE` ya arreglados en E.6 — falta el repo Go con raw rewrite + dialect branch |
>
> **Próximo arranque sugerido**:
>
> 1. **Home (631 LOC)** sesión propia, ~2-3h. Inspeccionar primero qué queries tiene (CTE con NextUp-style? items + user_data agregados?). Los predicados BOOLEAN/COLLATE ya arreglados en E.6.
> 2. **Federation (862 LOC)** la última sesión grande, ~3-4h. Inspeccionar el repo Go primero porque "MUCHOS raw SQL holdouts" — puede tener tanto raw SQL como Library + Channels juntos.
> 3. **Sesión F** — wiring `pgx/v5/stdlib` + `pgxpool` en `main.go` (~3h). Primera vez que el binario arranca contra Postgres y se valida end-to-end.
>
> Tiempo total restante para Sesión E: **~5-7h** (1-2 sesiones).
>
> **Nota nueva sobre isUniqueConstraintError**: la función está localizada en `library_epg_sources_repository.go` y detecta ambos dialectos por string-matching. Cuando Sesión F wire `pgx/v5`, conviene migrarlo a `errors.As(*pgconn.PgError)` para detección tipada (el TODO está embebido en el doc-comment).
>
> **Cuando Docker daemon esté arriba**: smoke-test E.11 contra Postgres real con `docker run -d -e POSTGRES_PASSWORD=test postgres:16-alpine`, `goose -dir migrations/postgres postgres "..." up`, y un script Go scratch que invoque cada Repository sobre datos seedeados. Patrones nuevos a validar: UpdatePriorities con qtx por rama, isUniqueConstraintError contra error real de pg (testear duplicate URL → ErrEPGSourceAlreadyAttached), TmdbID NullInt32 en Studio. Los Pattern A/B básicos ya tienen smoke contra Postgres real desde E.1-E.4.
>
> ---
>
> 🎬 **Sesión 2026-05-13 (rama `claude/musing-bell-cbe1da`, 1 commit, Sesión E.10 — 13 repos pequeños/mini cerrados de un golpe)** — continuando dual-dialect tras los 11 ya mergeados (PRs #260, #261, #264, #265). Pendiente de merge: 1 commit (`43161da`) sobre main.
>
> **Lo entregado en esta sesión**:
>
> Cierra el "Bloque 1" del handoff anterior — los 13 repos sub-200 LOC en un único commit. Trabajo mecánico: Pattern A / B ya estaban probados contra Postgres real en E.1–E.9, aquí solo aplicar los gotchas conocidos uno por uno. +1169/−342 LOC, 20 ficheros tocados (13 repos + repos.go + 6 test files externos).
>
> **Repos cerrados (orden por LOC)**:
>
> | Repo | LOC | Pattern | Notas |
> |---|---|---|---|
> | UserPreferences | 85 | B puro | Sin sqlc surface, 3 queries pre-rewriteadas. Template Settings |
> | Chapter | 96 | A + tx Replace | `nullableString` reemplaza inlines |
> | ExternalID | 113 | A + 1 raw-SQL | GetItemIDByExternalID — sqlc 1.31.1 LIMIT trunc |
> | DeviceCode | 116 | A + alias→struct | Conversión idéntica al SigningKey de E.2 |
> | ChannelWatchHistory | 117 | A | Number NullInt64↔NullInt32 + Limit int32 cast |
> | ChannelFavorites | 120 | A | Number NullInt64↔NullInt32 |
> | ItemValue | 123 | A + tx-style + 1 raw-SQL | ListGenres (sqlc 1.31.1 ORDER BY trunc) |
> | ChannelOverride | 130 | A + tx ApplyToLibrary | qtx por rama (mismo que Image.SetPrimary) |
> | Provider | 152 | A | Priority int64↔int32 |
> | EpisodeSegment | 154 | A + tx Replace | Sin gotchas (todo BIGINT/text/float64) |
> | Metadata | 177 | A + 2 raw-SQL batch | GetOverviewBatch, GetMetadataBatch — dynamic IN |
> | People | 193 | A + tx-style ReplaceItemPeople | SortOrder + Year NullInt64↔NullInt32 |
>
> **Verificación al cierre**:
>
> - `go build ./internal/db/...` clean.
> - `GOOS=linux go build ./...` clean (full cross-compile binario).
> - `go test -count=1 ./internal/db/` 11.5s — todos verde.
> - `go test ./internal/auth/ ./internal/scanner/ ./internal/iptv/ ./internal/library/ ./internal/federation/` — todos verde.
> - `TestSQLC_GeneratedFilesMatchQueries` verde (drift test).
> - `internal/api/...` sigue fallando en host Windows por `syscall.Statfs_t` (preexistente, no del refactor).
>
> **Estado al cierre — 24 de 28 repos done, 4 pending**:
>
> ✅ **Done** (24): Settings, Users, Sessions, SigningKeys, Library, MediaStreams, Items, UserData, Channels, EPGPrograms, Image, UserPreferences, Chapter, ExternalID, DeviceCode, ChannelWatchHistory, ChannelFavorites, ItemValue, ChannelOverride, Provider, EpisodeSegment, Metadata, People + (también los 6 originales E.1–E.4 que ya estaban verdes antes de E.5).
>
> ⏳ **Pending** (4 + 2 grandes = 6 repos):
>
> | Repo | LOC | Tier | Notas esperadas |
> |---|---|---|---|
> | **Federation** | **862** | 🔴 Gigante | MUCHOS raw SQL holdouts. Sesión propia |
> | **Home** | **631** | 🟠 Grande | Queries complejas items + user_data agregados |
> | Studio | 242 | 🟡 Mediano | |
> | LibraryEPGSources | 235 | 🟡 Mediano | |
> | IPTVSchedule | 206 | 🟡 Mediano | |
> | Collection | 201 | 🟡 Mediano | |
>
> **Próximo arranque sugerido**:
>
> 1. **Los 4 medianos** (Studio + LibraryEPGSources + IPTVSchedule + Collection) en una sesión, ~1h cada uno = ~3-4h. Mismo perfil que E.10 (trabajo mecánico aplicando los gotchas ya conocidos). Cierra los 4 y deja solo Federation + Home.
> 2. **Home (631 LOC)** sesión propia, ~2-3h. Inspeccionar primero qué queries tiene — el access-predicate post-040 ya está aplicado (auditado 2026-05-11).
> 3. **Federation (862 LOC)** la última sesión grande, ~3-4h. Inspeccionar el repo Go primero porque la memoria dice "MUCHOS raw SQL holdouts" — puede tener tanto raw SQL como Library + Channels juntos. Los `is_active = 1` / `can_browse = 1` / `COLLATE NOCASE` ya se arreglaron en E.6.
> 4. **Sesión F** — wiring `pgx/v5/stdlib` + `pgxpool` en `main.go` (~3h). Primera vez que el binario arranca contra Postgres y se valida end-to-end.
>
> Tiempo total restante para Sesión E: **~5-8h** (2-3 sesiones).
>
> **Cuando Docker daemon esté arriba**: smoke-test E.10 contra Postgres real con `docker run -d -e POSTGRES_PASSWORD=test postgres:16-alpine`, `goose -dir migrations/postgres postgres "..." up`, y un script Go scratch que invoque cada Repository.Get/List sobre datos seedeados. Lo nuevo a validar aquí son las firmas Pattern A/B aplicadas (todas variantes ya conocidas — la confianza es alta porque no hay queries nuevas).
>
> ---
>
> 🎬 **Sesión 2026-05-13 (rama `claude/beautiful-margulis-3db022`, 6 commits, Sesiones E.5–E.9 — 11 de 28 repos cerrados)** — continuando dual-dialect tras los 6 ya mergeados (PRs #260 y #261). Pendiente de merge: 6 commits sobre main en PR [#264](https://github.com/Alexzafra13/HubPlay_demo/pull/264).
>
> **Lo nuevo de esta tanda (E.8 + E.9 sobre lo previo)**:
>
> - **[`107f7ca`](https://github.com/Alexzafra13/HubPlay_demo/commit/107f7ca) E.8 — EPGPrograms** (314 LOC): Pattern A para CleanupOld + Insert/Delete dentro del tx ReplaceForChannel; Pattern B para los 3 reads (NowPlaying, Schedule, BulkSchedule chunked en 500 para `SQLITE_LIMIT_VARIABLE_NUMBER`). Sin gotchas BOOLEAN — la tabla `epg_programs` es sólo timestamps + texto. El `coerceSQLiteTime` se queda con su nombre histórico (pgx pasa por el `case time.Time`); doc-comment actualizado.
>
> - **[`434a846`](https://github.com/Alexzafra13/HubPlay_demo/commit/434a846) E.9 — Image** (319 LOC): Pattern A para 8 métodos sqlc + Pattern B para GetPrimaryURLs (dynamic IN + el último `is_primary = 1` reescrito a `is_primary`). Tx `SetPrimary` dialectado. Width/Height (INTEGER) → NullInt64 sqlite, NullInt32 pg en Create (helper `nullableInt32` reusado de MediaStreams). 11 callers actualizados en tests via PowerShell sed.
>
> **Estado al cierre de la tanda — 11 de 28 repos done, 17 pending**:
>
> ✅ **Done** (11):
>
> | Repo | LOC | Pattern | Notas |
> |---|---|---|---|
> | Settings | 149 | B (raw SQL pre-rewrite) | Template Pattern B |
> | Users | — | A + 1 raw SQL holdout | |
> | Sessions | — | A + 2 raw SQL holdouts | |
> | SigningKeys | — | A (alias → struct) | |
> | Library | — | A + 2 raw + 3 tx | |
> | MediaStreams | — | A + 1 tx + nullableInt32 | |
> | Items | 633 | A + 4 raw SQL | FTS dual-mecanismo + toTSQueryPrefix |
> | UserData | 485 | A + 2 raw SQL | + fix masivo BOOLEAN drift en 8 ficheros pg |
> | Channels | 562 | A + 9 raw SQL + tx | FILTER aggregate, ReplaceForLibrary tx |
> | EPGPrograms | 314 | A + 3 raw SQL + tx | Sólo time/text, sin BOOLEAN |
> | Images | 319 | A + 1 raw SQL + tx | Width/Height nullableInt32 |
>
> ⏳ **Pending** (17 repos, 3953 LOC totales):
>
> | Repo | LOC | Tier | Notas esperadas |
> |---|---|---|---|
> | **Federation** | **862** | 🔴 Gigante | MUCHOS raw SQL holdouts. Sesión propia. Ya tiene los `is_active = 1` / `can_browse = 1` / `COLLATE NOCASE` arreglados en E.6, pero aún hay raw SQL en el repo Go que necesita rewrite + dialect branch |
> | **Home** | **631** | 🟠 Grande | Segundo más grande. Probablemente queries complejas sobre items + user_data agregados |
> | Studio | 242 | 🟡 Mediano | |
> | LibraryEPGSources | 235 | 🟡 Mediano | |
> | IPTVSchedule | 206 | 🟡 Mediano | |
> | Collection | 201 | 🟡 Mediano | |
> | People | 193 | 🟢 Pequeño | El query con `is_available = 1` ya arreglado en E.6 |
> | Metadata | 177 | 🟢 Pequeño | Tiene un raw SQL batch (visto en E.5/E.6) |
> | EpisodeSegment | 154 | 🟢 Pequeño | |
> | Provider | 152 | 🟢 Pequeño | |
> | ChannelOverride | 130 | 🟢 Pequeño | |
> | ItemValue | 123 | 🟢 Pequeño | |
> | ChannelFavorites | 120 | 🟢 Pequeño | El `c.is_active = 1` ya arreglado en E.6 |
> | ChannelWatchHistory | 117 | 🟢 Pequeño | El `c.is_active = 1` ya arreglado en E.6 |
> | DeviceCode | 116 | 🟢 Pequeño | |
> | ExternalID | 113 | 🟢 Pequeño | |
> | Chapter | 96 | 🟢 Mini | |
> | UserPreferences | 85 | 🟢 Mini | |
>
> **Próximo arranque sugerido (orden por valor)**:
>
> 1. **Empezar por los 11 pequeños/mini (sub-200 LOC)** — son trabajo mecánico, sin riesgo, suman ~1500 LOC y cierran 11 repos rápido. Una sesión de 2-3h debería cerrarlos todos. Beneficio psicológico + reducir lista a sólo Federation + Home + 4 medianos.
> 2. **Después los 4 medianos** (Studio, LibraryEPGSources, IPTVSchedule, Collection) — ~1h cada uno. Otra sesión.
> 3. **Home (631 LOC)** — sesión propia. Inspeccionar primero qué queries tiene.
> 4. **Federation (862 LOC)** — la última sesión grande. Inspeccionar el repo Go primero porque la memoria dice "MUCHOS raw SQL holdouts" — puede tener tanto raw SQL como Library + Channels juntos.
> 5. **Sesión F** — wiring `pgx/v5/stdlib` + `pgxpool` en main.go (~3h). Es donde el binario por fin puede arrancar contra Postgres y se valida end-to-end.
>
> Tiempo total restante estimado para Sesión E: **~6-9h** (3-4 sesiones de 2-3h).
>
> **Cuando Docker esté arriba**: smoke-test E.5–E.9 contra Postgres real usando `docker run -d -e POSTGRES_PASSWORD=test postgres:16-alpine`, `goose -dir migrations/postgres postgres "..." up`, y un script Go scratch que invoque cada Repository.GetByID/List sobre datos seedeados. Las firmas Pattern A/B + el FTS-dual + el FILTER aggregate son lo nuevo a validar — todos los demás patrones ya tienen smoke contra Postgres real desde E.1-E.4.
>
> ---
>
> 🎬 **Sesión 2026-05-13 (rama `claude/beautiful-margulis-3db022`, 4 commits, Sesiones E.5 + E.6 + E.7 — los tres repos grandes cerrados)** — continuando dual-dialect tras los 6 ya mergeados (PRs #260 y #261). Pendiente de merge: 4 commits sobre main en PR [#264](https://github.com/Alexzafra13/HubPlay_demo/pull/264).
>
> **Lo entregado en esta tanda (resumen)**:
>
> - **[`20e24de`](https://github.com/Alexzafra13/HubPlay_demo/commit/20e24de) E.5 — Items** (633 LOC): Pattern A + Pattern B; FTS dual-mecanismo (FTS5 ↔ tsvector + `to_tsquery`), helper `toTSQueryPrefix`, helper `nullableIntPtrInt32`, BOOLEAN gotcha resuelto via `WHERE is_available` (sin `= 1`). Detalle completo abajo.
>
> - **[`175b2b6`](https://github.com/Alexzafra13/HubPlay_demo/commit/175b2b6) E.6 — UserData** (485 LOC) **+ fix masivo BOOLEAN/COLLATE drift**: octavo repo refactor (Pattern A para 10 métodos sqlc + Pattern B para GetBatch dynamic IN y NextUp CTE). EN EL MISMO COMMIT se arregla un drift sistemático que la Sesión D no había detectado: 14 sitios `column = 1` / `column = 0` sobre columnas BOOLEAN reescritos en 8 ficheros queries-postgres (channels, channel_favorites, channel_watch_history, federation, images, iptv_scheduled_jobs, people, user_data) + 3 `ORDER BY x COLLATE NOCASE` reescritos a `ORDER BY LOWER(x)` en federation.sql. `sqlc generate` regenera 8 ficheros sqlc_pg. Sin estos arreglos el postgres backend habría petado al primer Upsert/MarkPlayed/SetFavorite/etc.
>
> - **[`5fb1110`](https://github.com/Alexzafra13/HubPlay_demo/commit/5fb1110) E.7 — Channel** (562 LOC): noveno repo, último de los tres grandes. Pattern A para 6 métodos sqlc + Pattern B para 9 raw SQL (incluyendo el tx pattern de `ReplaceForLibrary`, `HealthSummaryByLibrary` con `COUNT(*) FILTER` que SQLite soporta desde 3.30 y por tanto es dialect-portable, y los cuatro health writers con UPDATE inline).
>
> **Estado al cierre de la tanda**:
>
> | Repo | Estado |
> |---|---|
> | Settings | ✅ Pattern B |
> | Users | ✅ Pattern A + 1 raw SQL holdout |
> | Sessions | ✅ Pattern A + 2 raw SQL holdouts |
> | SigningKeys | ✅ Pattern A (alias → struct) |
> | Library | ✅ Pattern A + 2 raw SQL holdouts + 3 tx |
> | MediaStreams | ✅ Pattern A + 1 tx + nullableInt32 |
> | Items | ✅ E.5 — Pattern A + 4 raw SQL (FTS dual + toTSQueryPrefix) |
> | **UserData** | ✅ **E.6 — Pattern A + 2 raw SQL + 8 fixes BOOLEAN drift en pg queries** |
> | **Channels** | ✅ **E.7 — Pattern A + 9 raw SQL + tx ReplaceForLibrary** |
> | EPGPrograms | ⏳ 314 LOC — tiene BulkSchedule raw SQL holdout |
> | Image | ⏳ 319 LOC — tiene GetPrimaryURLs raw SQL holdout |
> | Federation | ⏳ enorme con MUCHOS raw SQL holdouts |
> | 16 repos pequeños/medianos | ⏳ trabajo mecánico |
>
> **Drift de Sesión D detectado y arreglado en E.6** (importante para auditoría):
>
> - Las queries-postgres traducidas en Sesión D heredaban literales `WHERE column = 1` / `column = 0` de la fuente SQLite. Postgres tiene BOOLEAN real → rechaza el cast implícito. 14 sitios afectados, 8 ficheros tocados. Detección sólo posible al refactorizar UserData y leer las queries pg generadas con cuidado. Patrón aplicado:
>   - Predicado WHERE `x = 1` → `x` (truthy en ambos dialectos)
>   - Predicado WHERE `x = 0` → `NOT x`
>   - Asignación SET `x = 1` → `x = TRUE`
>   - INSERT VALUES con literal 1/0 sobre columna BOOLEAN → `TRUE` / `FALSE`
>   - `ORDER BY x COLLATE NOCASE` → `ORDER BY LOWER(x)` (NOCASE es SQLite-only)
>
> - **Regla preventiva añadida**: cuando se traduce un nuevo query a queries-postgres/, antes del `sqlc generate`, hacer `grep "= 1\|= 0\|COLLATE NOCASE"` sobre el fichero y resolver cada hit.
>
> **Gotchas acumulados a final de la tanda** (todos vivos para los 19 repos siguientes):
>
> - **Limit type**: sqlc emite `int64` para SQLite y `int32` para Postgres en LIMIT/OFFSET. Cast inline en cada llamada pg. Aplica también a Year/SeasonNumber/EpisodeNumber/PlayCount/Number en columnas declaradas INTEGER.
> - **BIGINT** se mantiene int64 en ambos dialectos (Size, DurationTicks, PositionTicks, *_ticks, bytes_*).
> - **Helpers de nullable INT**: `nullableIntPtr` (NullInt64, sqlite) y `nullableIntPtrInt32` (NullInt32, pg).
> - **BOOLEAN** : usar `WHERE column` (sin `= 1`) como predicado portable. Para SET/INSERT pg requiere `TRUE`/`FALSE` explícitos.
> - **FTS** : SQLite usa FTS5 virtual table (`items_fts MATCH 'foo*'`); Postgres usa `tsvector` + `to_tsquery('simple', ?)` con sanitización vía `toTSQueryPrefix(q)`.
> - **COLLATE NOCASE** : NO existe en Postgres → `LOWER(column)` para case-insensitive sort portable.
> - **CTE con param duplicado**: sqlc 1.31 no maneja `?` repetido en subqueries — los repos lo dejan como raw SQL Pattern B (NextUp en UserData es el ejemplo canónico).
> - **Tx pattern**: cada rama abre su `qtx := r.sq.WithTx(tx)` / `qtx := r.pq.WithTx(tx)` localmente; sin abstracción intermedia.
>
> **Verificación al cierre**:
> - `go build ./internal/db/... ./internal/iptv/... ./internal/library/... ./internal/auth/...` clean.
> - `GOOS=linux go build ./...` clean (cross-compile completo del binario).
> - `go test -count=1 ./internal/db/` 10.5s, todos verde.
> - `go test ./internal/iptv/ ./internal/library/... ./internal/scanner/ ./internal/auth/... ./internal/federation/...` todos verde.
> - `internal/api/...` falla en host Windows por `syscall.Statfs_t` pre-existente (no es del refactor).
> - `sqlc generate` clean.
> - 4 commits sobre `claude/beautiful-margulis-3db022`. PR [#264](https://github.com/Alexzafra13/HubPlay_demo/pull/264) abierto, descripción a actualizar con E.6 + E.7.
>
> **Próximo arranque sugerido**:
>
> 1. **EPGPrograms** o **Image** (los dos restantes con raw SQL holdout, ~314-319 LOC cada uno). Tras esos, **Federation** es el último gigante (raw SQL pesado). Después 16 repos mecánicos.
> 2. Tiempo restante estimado para Sesión E: ~6-8h en bloques de 2-3h cada uno.
> 3. **Sesión F** (wiring `pgx/v5/stdlib` + `pgxpool` en main.go, ~3h) sigue después de E. Ese es el primer momento donde el binario puede arrancar contra Postgres real y SE PUEDE VALIDAR end-to-end.
> 4. Cuando Docker daemon esté arriba en el host: smoke-test E.5/E.6/E.7 contra Postgres real (`docker run postgres:16-alpine` + goose + repo CRUD via Go scratch program). Las firmas Pattern A/B ya están validadas en E.1-E.4 contra Postgres real, lo nuevo aquí es el FTS dual-mecanismo + el FILTER aggregate.
>
> ---
>
> 🎬 **Sesión 2026-05-13 (rama `claude/beautiful-margulis-3db022`, 1 commit, Sesión E.5 — primer repo grande)** — continuando dual-dialect tras los 6 ya mergeados (PRs #260 y #261). Pendiente de merge: 1 commit sobre main.
>
> **Lo entregado**:
>
> - **[`20e24de`](https://github.com/Alexzafra13/HubPlay_demo/commit/20e24de) Sesión E.5 — ItemRepository dual-dialect**: séptimo repo, primero de los tres grandes (633 LOC). Mezcla Pattern A (sqlc-backed: Create/GetByID/GetByPath/Update/Delete/DeleteByLibrary/CountByLibrary/GetChildren) + Pattern B (raw-SQL con `rewritePlaceholders`: List, ChildCountsByParents, LatestItems, LatestSeriesByActivity).
>
> **Gotchas nuevos del Sesión E.5** (válidos para los 21 siguientes):
>
> - **FTS dual-mecanismo**: SQLite usa FTS5 (`items_fts MATCH 'foo*'`); Postgres usa la columna `search_vector` tsvector + GIN poblada por trigger en `migrations/postgres/002_fts_search.sql` (`search_vector @@ to_tsquery('simple', ?)`). Helper nuevo `toTSQueryPrefix(q)` sanitiza la entrada del usuario para `to_tsquery` (strip de `& | ! ( ) : * < > ' " etc`) y agrega `:*` al último token para prefix-search. SIN sanitización el parser de `to_tsquery` lanza syntax error con queries reales como "harry+potter (2001)".
>
> - **`WHERE is_available = 1` solo funciona en SQLite**: las columnas BOOLEAN se almacenan como INTEGER en SQLite (`= 1` es válido), pero Postgres tiene BOOLEAN real y rechaza `integer = boolean`. Solución uniforme: `WHERE is_available` (sin `= 1`) — truthy en ambos. Aplica a cualquier raw SQL futuro que filtre por columnas BOOLEAN.
>
> - **Year/SeasonNumber/EpisodeNumber**: INTEGER en ambos schemas, pero sqlc emite NullInt64 en SQLite y NullInt32 en Postgres. Los métodos sqlc branchéan params; los raw-SQL escanean en el tipo nativo del dialecto y proyectan a `int`. Helper nuevo `nullableIntPtrInt32` paralelo a `nullableIntPtr`. Aplica también a Size/DurationTicks **NO** porque están declarados BIGINT (NullInt64 en ambos).
>
> - **Cast estructural entre Row types**: cuando dos row types generados por sqlc son column-by-column idénticos (e.g. `sqlc_pg.GetItemByPathRow` vs `sqlc_pg.GetItemByIDRow`), Go permite el cast `RowA(rowB)` y evita duplicar el row-mapping helper. Útil cuando varias queries proyectan el mismo SELECT.
>
> **Verificación al cierre**:
>
> - `go build ./internal/db/... ./internal/auth/... ./internal/library/...` clean
> - `GOOS=linux go build ./...` clean (cross-compile completo del binario)
> - `go test -count=1 ./internal/db/` 13.9s — todos verde
> - `go vet ./internal/api/... ./internal/db/... ./internal/library/... ./internal/scanner/... ./internal/stream/...` (con GOOS=linux) clean
> - 40 callers de `NewItemRepository` actualizados (1 prod en `repos.go` + 39 tests en 8 ficheros)
> - Local `golangci-lint` bloqueado por App Control de Windows; CI lo correrá en Linux
> - Postgres real no smoke-testeado esta sesión (Docker daemon no arrancado en el host); Pattern A/B ya validado contra Postgres real en E.1-E.4
>
> **Estado real al cierre**:
>
> | Repo | Estado |
> |---|---|
> | Settings | ✅ Pattern B |
> | Users | ✅ Pattern A + 1 raw SQL holdout |
> | Sessions | ✅ Pattern A + 2 raw SQL holdouts |
> | SigningKeys | ✅ Pattern A (alias → struct) |
> | Library | ✅ Pattern A + 2 raw SQL holdouts + 3 tx |
> | MediaStreams | ✅ Pattern A + 1 tx + nullableInt32 |
> | **Items** | ✅ **E.5 — Pattern A + 4 raw SQL (FTS dual + toTSQueryPrefix)** |
> | UserData | ⏳ 485 LOC — siguiente recomendado (crítico, el progreso de reproducción) |
> | Channel | ⏳ 561 LOC — grande |
> | EPGPrograms | ⏳ 314 LOC — tiene BulkSchedule raw SQL holdout |
> | Image | ⏳ 319 LOC — tiene GetPrimaryURLs raw SQL holdout |
> | Federation | ⏳ enorme con MUCHOS raw SQL holdouts |
> | 16 repos pequeños/medianos | ⏳ trabajo mecánico |
>
> **Próximo arranque** (cuando se retome):
>
> 1. **UserData** es el siguiente recomendado — sin él no se persiste el progreso de reproducción. 485 LOC. Pattern A esperado para la mayoría; verificar si tiene raw SQL holdouts.
> 2. Después Channel (561 LOC), luego en cualquier orden los medianos.
> 3. Tiempo restante estimado para Sesión E: ~10-12h en bloques de 3-4h.
> 4. **Sesión F** (wiring pgx en main.go, ~3h) sigue después de E.
> 5. Si Docker daemon está arriba, hacer smoke-test del repo más reciente contra Postgres antes de cerrar la sesión: `docker run -d --name pg-test -e POSTGRES_PASSWORD=test postgres:16-alpine && goose -dir migrations/postgres postgres "..." up && go run ./scripts/smoketest-X.go`.
>
> ---
>
> 🎬 **Sesión 2026-05-12 noche (rama `claude/gallant-galileo-3c374b`, +5 commits Sesión E parcial)** — continuación de la sesión del día. La rama acumula ahora ~18 commits sobre main, todos pendientes de merge. **Stop point honesto: 6 de 28 repos refactorizados a dual-dialect, patrón A y B probados ambos contra Postgres real.**
>
> **Lo nuevo de esta tanda nocturna**:
>
> - **[`128dbd0`](https://github.com/Alexzafra13/HubPlay_demo/commit/128dbd0) Sesión E.1 — dialect helper + Settings (Pattern B)**: nuevo `internal/db/dialect.go` con `DriverSQLite`/`DriverPostgres` constantes, `IsPostgres()` helper, y `rewritePlaceholders(driver, sql)` que convierte `?` → `$N` en runtime. Maneja string literals + line comments correctamente (la palabra "user's" en un comment activaba el modo string-tracking y se tragaba placeholders posteriores — bug que ya pillé antes en el script de conversión de queries). 11 tests unitarios. SettingsRepository refactorizado como PoC del Pattern B (raw SQL pre-computado al construir). Verificación end-to-end: `docker run postgres:16-alpine` + smoke test Go invocando SettingsRepository via pgx → CRUD completo round-trip ✓.
>
> - **[`d1f36d2`](https://github.com/Alexzafra13/HubPlay_demo/commit/d1f36d2) Sesión E.2 — auth core (3 repos)**: User/Session/SigningKey refactorizados al Pattern A (dual q pointers, branch per method). El más sutil fue SigningKey que era `type SigningKey = sqlc.JwtSigningKey` (alias) → convertido a struct propio. Sessions tiene 2 raw SQL holdouts (RotateRefreshToken por bug UPDATE 4+ placeholders, GetByPreviousRefreshTokenHash por la column-post-038), Users tiene 1 (ListProfilesForOwner por bug ORDER BY+COLLATE).
>
> - **[`911a09a`](https://github.com/Alexzafra13/HubPlay_demo/commit/911a09a) Sesión E.3 — LibraryRepository**: refactor más complejo hasta ahora. Mezcla Pattern A (sqlc) + 3 transacciones con `WithTx` (Create, Update, ReplaceAccess) + 2 raw SQL holdouts (UserHasAccess, ListForUser). Tx pattern: `if useSQLite() { qtx := r.sq.WithTx(tx) ... } else { qtx := r.pq.WithTx(tx) ... }` — sin abstracción intermedia, cada rama completa.
>
> - **[`24b7e2c`](https://github.com/Alexzafra13/HubPlay_demo/commit/24b7e2c) Sesión E.4 — MediaStreamRepository**: pequeño pero introdujo helper nuevo `nullableInt32` para columnas INTEGER (no BIGINT) en el branch Postgres.
>
> **Patrón decidido y probado para los 22 repos restantes**:
>
> 1. **Pattern A** (repos sqlc-based, ~21 de 27): dual q pointers `sq *sqlc.Queries` + `pq *sqlc_pg.Queries`, exactamente uno non-nil tras construct. Helper `useSQLite() bool`. Cada método público es `if r.useSQLite() { ... sqlc branch ... } else { ... sqlc_pg branch ... }`. Verbose por construcción pero LINEAL — cada método autocontiene su lógica de ambos dialectos. Auditable sin saltar entre sitios.
>
> 2. **Pattern B** (repos raw SQL, ~7 holdouts): driver field + queries pre-computadas en el constructor vía `rewritePlaceholders(driver, sql)`. Cero overhead por llamada. Reusable en los otros 6 holdouts existentes (federation, library partial, session partial, user partial, image batch, metadata batch, items LatestItems).
>
> **Gotchas descubiertos haciendo los 6 repos** (válidos para los 22 siguientes):
>
> - **sqlc genera int32 para Postgres INTEGER, int64 para SQLite INTEGER**. Cast inline `int32(...)` en la rama postgres. Aplica a LIMIT/OFFSET (siempre int) y a columnas declaradas INTEGER en el schema.
> - **BIGINT en ambos dialectos = int64**. Las columnas `*_ticks`, `size`, `bytes_*` no sufren este split porque están declaradas BIGINT en migrations/postgres/.
> - **Type aliases (`type X = sqlc.Y`)** se rompen con dual-dialect. Convertir a struct propio + 2 helpers (uno por dialecto) para mappear desde la row generada.
> - **Transacciones funcionan idénticas**: `WithTx(tx)` existe en ambos paquetes generados. Solo cambia el tipo retornado.
> - **`nullableInt32`** se ha añadido al pool de helpers junto a `nullableInt64`, `nullableFloat64`, `nullableString`.
>
> **Estado real al cierre de la noche**:
>
> | Repo | Estado |
> |---|---|
> | Settings | ✅ Pattern B |
> | Users | ✅ Pattern A + 1 raw SQL holdout |
> | Sessions | ✅ Pattern A + 2 raw SQL holdouts |
> | SigningKeys | ✅ Pattern A (alias → struct) |
> | Library | ✅ Pattern A + 2 raw SQL holdouts + 3 tx |
> | MediaStreams | ✅ Pattern A + 1 tx + nullableInt32 |
> | Items | ⏳ 633 LOC — el más grande, tiene raw SQL LatestItems |
> | UserData | ⏳ 485 LOC — grande |
> | Channel | ⏳ 561 LOC — grande |
> | EPGPrograms | ⏳ 314 LOC — tiene BulkSchedule raw SQL holdout |
> | Image | ⏳ 319 LOC — tiene GetPrimaryURLs raw SQL holdout |
> | Federation | ⏳ enorme con MUCHOS raw SQL holdouts |
> | UserData / Channel / Home / Metadata / etc | ⏳ por ver |
> | 14 repos pequeños/medianos | ⏳ trabajo mecánico |
>
> **Verificación a final de noche**:
> - `go build ./...` clean
> - `go test -count=1 ./internal/db/ ./internal/auth/...` todos verde (15s + 5.8s)
> - `golangci-lint v2.5.0` 0 issues
> - Test smoke contra Postgres real: SettingsRepository CRUD round-trip ✓
>
> **Próximo arranque** (cuando se retome):
>
> 1. Continuar Sesión E refactorizando los 22 repos restantes. Orden recomendado: **Items + UserData + Channel** primero (los 3 grandes críticos sin los que no se puede ver contenido). Después en cualquier orden los medianos/pequeños.
> 2. Tiempo estimado restante para Sesión E: ~12-14h en sesiones de 3-4h cada una.
> 3. Después de E vendrá **Sesión F** (wiring pgx + pgxpool en main.go, ~3h) — ese es el punto donde el binario por fin puede arrancar contra Postgres y se puede VALIDAR end-to-end.
> 4. La regla mecánica para cada repo siguiente:
>    - Si usa sqlc: Pattern A (ver UserRepository como template)
>    - Si tiene raw SQL: Pattern B (ver SettingsRepository como template) o mezclar como Library/Session/User
>    - Cast `int32(...)` en la rama postgres cuando la columna sea INTEGER (no BIGINT)
>    - Actualizar `repos.go` para pasar `driver` al constructor
>    - Actualizar todas las llamadas test en grep con sed `s/db\.NewXRepository(database)/db.NewXRepository("sqlite", database)/g`
>    - Verificar `go build ./...` + `go test ./internal/db/` antes de pasar al siguiente
>
> ---
>
> 🎬 **Sesión 2026-05-12 (rama `claude/gallant-galileo-3c374b`, 11 commits acumulados, dual-dialect Postgres + plug-and-play migration)** — sesión larga de varios bloques estratégicos. Pendiente de merge: rama con 11 commits sobre main.
>
> **Bloque 1 — auto-tune transcoding ([72f7409](https://github.com/Alexzafra13/HubPlay_demo/commit/72f7409))**: arreglo del bug del `transcode_preset` dead-config (estaba hardcoded a "veryfast"). Nuevo `AutoTuneStreaming(hwAccel, cores)` que rellena MaxTranscodeSessions / MaxTranscodeSessionsPerUser / TranscodePreset según el hardware al boot. Tabla auto-tune: NVENC=3, QSV/VAAPI=6, VideoToolbox=4, software=cores/2; preset escala con cores (≥12 fast, 6-11 veryfast, 4-5 superfast, <4 ultrafast). 3 settings nuevos en el panel admin (`streaming.*`) con validators int 1-64 / int 1-32 / enum libx264. `force_direct_play` ahora tiene banner rojo + confirm() obligatorio.
>
> **Bloque 2 — host metrics reales ([bea8286](https://github.com/Alexzafra13/HubPlay_demo/commit/bea8286))**: nuevo paquete `internal/sysmetrics/` con `Sampler` que usa `github.com/shirou/gopsutil/v4` (pure Go, no CGO). Muestreo cada 5s en background con `atomic.Value` para read sin bloqueos. Probes one-time al Start: CPU model + cores físicos/lógicos, RAM total, GPU model via `nvidia-smi --query-gpu` (cuando hay NVIDIA). Wire shape `host` añadido a `/admin/system/stats`. Frontend: nueva sección "Host" en SystemStatus con cards CPU% (sparkline + color por threshold) + RAM bar + GPU info. Identity strip arriba muestra "HubPlay X · uptime · Ryzen 5 5600 · 6c/12t · NVENC".
>
> **Bloque 3 — burn-in subs PGS/DVDSUB/ASS ([77e0c2c](https://github.com/Alexzafra13/HubPlay_demo/commit/77e0c2c))**: el gap funcional real de subs de Blu-ray + anime. Nuevo `internal/stream/subburn.go` con `BurnSubtitleSpec` + helpers `IsImageSubtitleCodec` / `IsStyledTextSubtitleCodec` / `IsBurnableSubtitleCodec`. `BuildFFmpegArgs` rutea bitmap codecs a `-filter_complex` con `[0:s:N]overlay`, ASS/SSA a `-vf subtitles=filename='...':si=N`. Path escape correcto para colons/backslashes/brackets. Session key incluye `burnSubIndex` (el índice se hornea en los segmentos, switch requiere fresh session). Frontend: `BURN_SUB_TRACK_ID_BASE = 20000` (paralelo a FEDERATED_TRACK_ID_BASE = 10000), entries con `burnIn: true` flag, sub-label "Integrado · reinicia el stream". `usePlayback` gana `switchBurnSubtitle(idx, resumeAt)` que mantiene audio + remonta master URL con `?subtitle=N`.
>
> **Bloque 4 — fix OpenAPI drift gate ([5b2d90a](https://github.com/Alexzafra13/HubPlay_demo/commit/5b2d90a))**: bonus. Test `TestOpenAPISpec_RouterCoverage` fallaba bajo `-race` porque `/auth/device/events` (SSE pairing añadido en a549332) no estaba en el allowlist. Añadida entrada al `outOfScopeExact` con justificación SSE.
>
> **Bloque 5 — audit plan futuro ([39732b8](https://github.com/Alexzafra13/HubPlay_demo/commit/39732b8))**: doc `docs/memory/audit-plan.md` (430 líneas) con 5 pases secuenciales accionables — funcional / código / performance / seguridad / producción. Para ejecutar en otra sesión con media real.
>
> **Bloque 6 — dual-dialect Postgres foundation ([50c9089](https://github.com/Alexzafra13/HubPlay_demo/commit/50c9089) + [9335f46](https://github.com/Alexzafra13/HubPlay_demo/commit/9335f46) + [db886c1](https://github.com/Alexzafra13/HubPlay_demo/commit/db886c1))**: 3 commits, Sesiones A/B/C del plan. Foundation: `sqlc.yaml` con bloque postgres staged comentado, `migrations/postgres/001_initial_schema.sql` traducido, `pgx v5.9.2` añadido. Sesión B: las 18 migraciones 002-019 traducidas (FTS5 → tsvector + GIN + función pl/pgsql + 2 triggers, hex(randomblob) → encode(gen_random_bytes,'hex') + pgcrypto, partial indexes con TRUE en lugar de 1, INTEGER mantenido para tls_insecure por paridad sqlc cross-dialect). Sesión C: las 22 migraciones 020-041 (BLOB → BYTEA para Ed25519, INTEGER PRIMARY KEY AUTOINCREMENT → BIGSERIAL, bytes_out → BIGINT por overflow, json_each → LATERAL jsonb_array_elements_text + IS JSON ARRAY de Postgres 16+, INSERT OR IGNORE → ON CONFLICT DO NOTHING). **41 migraciones aplicadas en Postgres 16 real via goose en docker, 46 tablas creadas, todos los índices críticos verificados.** SQLite intacto en todo momento.
>
> **Bloque 7 — voz interna en docs ([56fcd68](https://github.com/Alexzafra13/HubPlay_demo/commit/56fcd68))**: a petición del usuario, `postgres-migration.md` reescrito sin tono de onboarding-de-contribuidores. Preferencia guardada en memoria (`feedback_internal_voice_docs.md`).
>
> **Decisiones senior tomadas en esta sesión**:
>
> 1. **Plug-and-play SSE para migración DB (no polling)**: actualizado el plan Sesión G con sub-fases G.1–G.4. Migración 100% desde panel admin usando EventSource auto-reconnect nativo del browser (NO es polling — es comportamiento del protocolo, exactamente lo mismo que `useUserDataSync`). State machine + marker file en `/config/migrations/{id}.json` sobreviven al restart. Auto-detect de `/var/run/docker.sock` decide modo `in_process` vs `advisory`. Read-only window 503 + Retry-After durante pgloader copy. Rollback automático si falla.
>
> 2. **Disciplina de mantenibilidad dual-dialect**: cuatro capas documentadas en `postgres-migration.md` — convención de gemelas obligatorias, schema-fingerprint test (Sesión J), query-parity test (post-Sesión D), CI matrix con `[sqlite, postgres]`. Imposible mergear PRs que rompan un backend sin verlo.
>
> 3. **App TV (Android) no afecta arquitectura dual-dialect**: es un cliente, le da igual el backend. Lo único requerido: handler de 503 + Retry-After (común a apps con conectividad flaky), device-code flow (ya existe vía `/auth/device` desde la sesión 2026-05-11). Postgres ES beneficioso para hogares con muchas TVs activas (writes de progreso paralelizan).
>
> 4. **Squash de migraciones diferido a v1.0 cutover**: NO hacer mientras estamos en pleno trabajo. Plan documentado: archivar `migrations/sqlite/_archive/`, baseline consolidado en `001_baseline.sql`, script `migrate-to-baseline` que detecta DBs existentes y reescribe `goose_db_version`. Riesgo en el test server actual: bajo con procedimiento documentado.
>
> 5. **sqlc.yaml bloque postgres queda comentado** hasta Sesión D (primer query traducido). Sin queries la generación falla en dir vacío — comentado mantiene el commit limpio.
>
> **Métricas al cierre**:
> - 11 commits sobre `claude/gallant-galileo-3c374b`, pendientes de merge.
> - 41 migraciones Postgres validadas contra `postgres:16-alpine` real (`successfully migrated database to version: 41`).
> - `go test -race ./...` todos verdes (22 paquetes, incluyendo handlers, db, stream, sysmetrics).
> - `golangci-lint v2.5.0` 0 issues.
> - `pnpm test --run` 532/532, `tsc --noEmit` clean.
> - Smoke test del binario boot en docker: identity strip muestra "AMD Ryzen 7 7730U · 16c · ..." con métricas live, auto-tune `max_sessions=8 max_sessions_per_user=4 libx264_preset=fast`.
> - Binary build CGO_ENABLED=0 19.5MB.
>
> **Backlog activo al cierre**:
>
> - **Sesión D (siguiente)**: traducir 26 query files a `internal/db/queries-postgres/`. Mecánico salvo donde haya `INSERT OR IGNORE` / `json_each` / fecha-math. Activa el bloque sqlc postgres + permite regen del paquete `sqlc_pg`.
> - **Sesión E**: refactor de 14 repos a interface dual-dialect. La grande (~14h).
> - **Sesión F**: wiring `pgx/v5/stdlib` + `pgxpool` en main.go. Una vez aquí el binario puede arrancar contra Postgres.
> - **Sesión G** (4 sub-fases): plug-and-play SSE migration. Ver `docs/architecture/postgres-migration.md` para el desglose.
> - **Sesión H–J**: worker pool, extensiones, CI dual. Pueden esperar a v1.1.
> - **Audit del proyecto**: planificada en `docs/memory/audit-plan.md`. Hay que ejecutarla con media real, no en esta rama.
>
> **Reglas duras que esta sesión añade / refuerza**:
>
> - **Migraciones SIEMPRE en pareja**: añadir `migrations/sqlite/N_x.sql` exige `migrations/postgres/N_x.sql` en el mismo commit. Sin excepciones.
> - **No assumed types entre dialectos**: cualquier columna `*_ticks` / `size` / `bytes_*` es `BIGINT` en Postgres (regla de escaneo documentada).
> - **SQLite es la fuente de verdad de schema** hasta que termine Sesión F. El generador no es bidireccional.
> - **No polling para feedback de operaciones lentas**: usar SSE con reconnect nativo. Es el patrón del proyecto.
> - **El test server del usuario tiene goose_db_version=41**: cualquier squash futuro debe respetar eso, no asumir DB virgen.
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/fix-admin-permissions-fpFHG`, admin principal + pairing UI con QR/SSE)** — el usuario reportó dos cosas: la matriz de bibliotecas mostraba vacío para el admin principal (Phase B cerró el modelo pero olvidó al admin) y el flujo de vincular dispositivo era poco profesional (todos los accesos mezclados en Settings, sin QR para TVs). Tres commits pusheados a `origin/claude/fix-admin-permissions-fpFHG`, pendientes de merge.
>
> **Commit 1 — `a965743` `feat(access): primary admin sees every library by default`**.
>
> - Migración `041_primary_admin_library_access.sql`: backfill `INSERT OR IGNORE` para el admin principal (oldest top-level `role='admin'`, misma definición que `GetPrimaryAdminID`) × todas las libraries existentes. Migración 040 lo había excluido (`AND u.role != 'admin'`) confiando en el bypass de código de LIST, pero la matriz de Phase B consulta `library_access` directo. Persistir cierra la inconsistencia.
> - Nueva sqlc query `GrantPrimaryAdminLibraryAccess` invocada desde `LibraryRepository.Create` dentro de la misma tx: mantiene la invariante para libraries creadas después de la migración. No-op cuando no existe admin (cold-start pre-setup).
> - **Decisión consensuada con el usuario**: solo el admin principal recibe grants automáticos. Admins secundarios siguen apoyados en el bypass de LIST; su matriz vacía refleja honestamente que no tienen grants explícitos. Demote conserva los grants.
> - 4 tests Go nuevos en `library_repository_test.go`: happy path, no-admin no-op, only-primary-admin (no secundarios), backfill-migration simulation (wipe + replay del SQL de 041).
>
> **Commit 2 — `a549332` `feat(auth/device): SSE pairing channel + RFC 8628 §3.3.1 + cookies`**. El usuario preguntó "de forma correcta todo nada de polling no?", así que se entregó SSE en vez de polling para la UI in-app. El proyecto ya tenía el patrón (`MeEventsHandler`).
>
> - `verification_uri_complete` en `/auth/device/start` (RFC 8628 §3.3.1) = `verification_url + ?code=ABCD-EFGH`. Helper `buildVerificationURIComplete` honra URLs con query ya presente.
> - Nuevo tipo de evento `event.DeviceCodeApproved` (data: `device_code`, `user_id`). `DeviceCodeService.ApproveDevice` publica vía `s.auth.publish` tras `codes.Approve`. `Service.publish` ya era nil-safe.
> - Nuevo método `DeviceCodeService.InspectDevice(ctx, deviceCode) (*DeviceCodeStatus, error)` — read-only snapshot (status: pending/approved/consumed/expired + expires_at) que el SSE handler consulta on-connect sin tocar `LastPolledAt`.
> - Nuevo handler SSE `GET /api/v1/auth/device/events?device_code=X`: keyed por device_code (mismo threat model que `/poll`), 404 BEFORE flushear headers si el code es typo, emite `pending` inicial + síntesis de `approved/consumed/expired` cuando ya está terminal (cubre reconexión + race), subscribe al bus, timer local con `time.Until(expiresAt)` para emitir `expired` y cerrar. SSE limiter keyed `"device:" + deviceCode` (no user_id porque endpoint es unauthenticated).
> - `/auth/device/poll` setea cookies (`hubplay_access`, `hubplay_refresh`) en éxito vía `setAuthCookies(w, r, tok, accessTTL, refreshTTL)` ANTES del `respondJSON`. Native clients (TVs, CLI) ignoran las cookies y leen los tokens del JSON; navegador queda logueado.
> - `MySession` wire gana `auth_method` derivado del prefijo `device-code-` en `device_id` (string check en handler, sin migración). Función `sessionAuthMethod` en `internal/api/handlers/auth.go`.
> - `NewDeviceAuthHandler` ahora pide `authCfg config.AuthConfig` + `bus EventBusSubscriber` + `limiter *SSELimiter`. Método `HasEventBus()` permite al router skipear el SSE endpoint si no hay bus inyectado (tests, headless setups).
> - 2 tests nuevos en `internal/auth/device_test.go`: `InspectDevice` walk de estados, `ApprovePublishesEvent` (sync.Mutex + channel, ventana 2s). 4 tests nuevos en `internal/api/handlers/auth_device_test.go` (archivo nuevo): start emite URI complete, poll setea cookies, SSE happy path (con goroutine que dispara Approve mid-stream y `bufio.Scanner` parseando event-stream wire format), SSE unknown code → 404.
>
> **Commit 3 — `dcb23cd` `feat(web): in-app device pairing UI with QR + SSE + linked-device list`**. Tres surfaces frontend en una sola pasada:
>
> - **`/pair`** (público, fuera de `ProtectedRoute`): página nueva `PairThisDevice.tsx`. Detecta el dispositivo (`Tizen → "Samsung TV"`, `webOS → "LG TV"`, etc.) para que la session row en Settings sea legible. Llama `useStartDeviceCode` on-mount, renderiza el user_code grande + QR vía `qrcode` (`pnpm add qrcode @types/qrcode`, ~28KB gz, modo SVG para evitar canvas en TVs viejos), abre `EventSource` contra `/auth/device/events`. Escucha `approved` → llama `usePollDeviceCode` UNA SOLA VEZ → navega a `/` (App.tsx ya rutea según `password_change_required` etc.). `expired/consumed` → notice + botón "Generar nuevo código". `EventSource.onerror` no flipea a error (auto-reconnect transitorio); solo los terminal events cierran el stream.
> - **`/link`** gana sección "Dispositivos vinculados" (`LinkedDevicesList.tsx`) debajo del formulario. Filtra `MySession[]` por `auth_method === "device_link"`. Hidden completamente si vacío. Revoke sin confirm (operator ya está en otro dispositivo). También prefill desde `?code=ABCD-EFGH` via `useSearchParams`: el QR del TV apunta a `/link?code=…`, móvil escanea, aterriza con el campo lleno, tap "Aprobar".
> - **Settings → Tus dispositivos**: `DevicesPanel` ordena device-link primero, password después, dentro de cada grupo por `last_active_at` desc. Cada row tiene badge `"Vínculo dispositivo"` (azul) vs `"Sesión web"` (gris). El badge se oculta en `/link` (toda la lista ya es device-link) vía `showAuthMethodBadge` prop.
> - **`DeviceRow.tsx`** extraído de `DevicesPanel` para reuso. Icono: `Tv` si `device_link`, `Smartphone` si `device_name` matchea `/iphone|android|mobi|ipad|tablet/i`, sino `Laptop`.
> - **Login** gana link discreto al final del card: "¿Estás en una TV o consola? Vincula este dispositivo" → `/pair`.
> - **i18n**: nuevas keys en `link.linked.*`, `pair.*`, `login.pairPrompt/pairCta`, `settings.devices.badge.*` (en es + en). Convención del proyecto seguida: `defaultValue` por todos lados, en.json mezcla inglés y español según traducción parcial existente.
> - **TS strict**: `MySession.auth_method` es required (no opcional) — rompe fixtures de tests, actualicé `DevicesPanel.test.tsx` con `auth_method: "password"`.
> - 3 tests nuevos en `LinkedDevicesList.test.tsx` (filter, empty render, revoke sin confirm), 2 en `LinkDevice.test.tsx` (prefill, canonicalización antes de POST). El test de LinkDevice IMPORTA `"@/i18n"` para que `t()` resuelva keys reales — el resto del frontend usa `defaultValue` consistentemente, este componente no.
>
> **Decisiones senior tomadas en esta sesión**:
>
> 1. **SSE en vez de polling para la UI in-app**. El proyecto migró a SSE en sesiones anteriores (`drive useMySessions + useAdminStreamSessions from SSE`). RFC 8628 prescribe polling pero solo es vinculante para native clients; la web in-app puede push y `/poll` queda como source-of-truth de token issuance + fallback. El SSE handler nunca emite tokens — solo notifica "ahora llama /poll".
> 2. **Síntesis de terminal events on-connect** en vez de "esperar a que pase algo". Si el row ya está approved/consumed/expired cuando el client se conecta, emitimos el evento y cerramos. Hace los reconnects idempotentes sin código cliente complejo.
> 3. **Cookies en `/poll`** (no en `/approve`). El operator apruena DESDE otro device — el cookie ahí no sirve. La cookie tiene que estar en la respuesta que el dispositivo paired recibe, que es `/poll`. Native clients ignoran cookies y leen JSON. Wire shape sin cambios.
> 4. **Reusar el prefijo `device-code-` en `device_id`** para derivar `auth_method`. Cero migración. La invariante ya existía (`auth/device.go:255` lo stampea), solo había que exponerla.
> 5. **`qrcode` en modo SVG** en vez de canvas. Tizen/webOS antiguos tienen problemas con WebGL/canvas grandes; SVG inline es seguro y la diff de tamaño es despreciable (~5KB).
> 6. **Solo admin principal recibe auto-grants**, no todos los admins. Consistente con la noción de "primary admin" que ya está en el código (`GetPrimaryAdminID`, `PRIMARY_ADMIN_LOCKED` en deactivate). Admins secundarios son raros y su matriz vacía es honesta.
>
> **Métricas al cierre**:
> - Backend Go: `go test -race ./internal/auth/... ./internal/api/handlers/... ./internal/db/... ./internal/library/... ./internal/event/...` → todos verdes. `golangci-lint` 0 issues.
> - Frontend: `pnpm test --run` → **524/524** (+8 nuevos: 4 LibraryRepository + Migration + 2 device service + 4 device handler + 3 LinkedDevicesList + 2 LinkDevice). `tsc --noEmit` clean.
> - Nuevas dependencias frontend: `qrcode@1.5.4`, `@types/qrcode@1.5.6` (dev). `pnpm@10.32.1`.
> - Migración 041 idempotente, no rompe el sweep `testutil.NewTestDB`.
>
> **Reglas duras que esta sesión añade / refuerza**:
>
> - El admin principal SIEMPRE tiene grants explícitos en `library_access` post-041. Cualquier código que itere libraries para el admin principal puede asumirlo. Admins secundarios NO (siguen con el bypass de código).
> - SSE `/auth/device/events` está autenticado por `device_code` (no por bearer/cookie). El threat model es idéntico a `/poll`: quien tenga el opaque token puede escuchar. No filtrar por user — el row no tiene user_id hasta después del approve.
> - `MySession.auth_method` es required en TS. Todo fixture nuevo lo necesita.
> - `qrcode` queda como única dep para QR rendering. Si en el futuro hace falta QR en otro lado (federation invites? share links?) reutilizar la misma lib + modo SVG.
> - El sweeper de `device_codes` no publica eventos al borrar. El SSE handler usa un timer local para `expired`. Si en el futuro se quiere precisión strict, hookear el sweeper al bus.
>
> **Pendiente / próximo bloque**:
>
> El backlog activo del cierre anterior sigue válido:
> - **Subs burn-in PGS/DVDSUB/ASS** — feature visible (Blu-ray subs hoy no se ven).
> - **Audio multichannel passthrough** (5.1/7.1 → estéreo hoy).
> - **VAAPI fully-on-GPU**.
>
> Cosas que asomaron en esta sesión y NO se hicieron (no scope):
> - Backfill script para grants de admins secundarios si el operador los promueve y quiere matrices completas.
> - Sweeper hookeado al bus para emitir `device_code.expired` exacto (cosmético).
> - `frontend-design` polish del `/pair` (la página funciona pero usa el styling estándar; un TV-mode con tipografía más grande, contraste alto, focus visible para mando D-pad sería P3).
> - QR generation reuse en federation invites (hoy `generateInvite` devuelve un código texto). Misma lib, ~15 LOC de UI.
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/infallible-robinson-97926d`, Phase B completa + seek-coalesce audit)** — cierre del modelo de acceso por hogar con dos PRs sucesivos sobre la misma rama, más auditoría a fondo de seek-coalesce que terminó en una recomendación de no tocar.
>
> **Commits pusheados** (ambos a `origin/claude/infallible-robinson-97926d`, pendientes de merge):
>
> 1. `d0017be` `feat(access): admin endpoints for the household library matrix` — Phase B backend.
>    - `POST /users` ampliado con `grant_library_ids` opcional. Valida la existencia de cada library ANTES de crear el user (half-applied imposible) y rechaza outright si el body es de profile creation (ADR-014).
>    - `GET /users/{id}/library-access` — normaliza profile→parent server-side, devuelve `{user_id, owner_id, library_ids, is_inherited}` para que el frontend pinte la matriz heredada en un round-trip.
>    - `PUT /users/{id}/library-access` — diff transaccional (insert faltantes, revoke sobrantes). 400 si target es profile.
>    - Nueva query sqlc `ListLibraryAccessByUser`; nuevo método `LibraryRepository.ReplaceAccess` con tx.
>    - **sqlc 1.31.1 trip evitado**: la primera versión del comment del query tenía un em-dash que truncó `ORDER BY library_id` a `ORDER BY library_`. Fix a ASCII y regen. Patrón ya documentado en `architecture-decisions.md`.
>    - **Decisión senior**: cambio de path `/users/{id}/access` → `/users/{id}/library-access` porque el primero YA estaba ocupado para account-expires-at. El status doc viejo no lo había detectado.
>    - 4 tests Go nuevos en repo + handler. `go test -race ./...` verde (vía docker linux porque health.go usa `syscall.Statfs` no-portable a Windows). `golangci-lint v2.5.0` 0 issues.
>    - OpenAPI drift documentado bajo `/users/{id}/library-access`; `POST /users` ya estaba en allowlist.
>
> 2. `0886dfa` `feat(web): admin matrix UI for the household library access surface` — Phase B frontend.
>    - Add User modal gana sección "Bibliotecas accesibles" con todas las libraries pre-tickeadas (default "dale acceso a todo el hogar"; el admin DESTICKEA para restringir). Ships dirty set como `grant_library_ids` en el mismo POST.
>    - Nueva acción "Bibliotecas" en el kebab por usuario → abre modal Edit que carga GET (normaliza profile→parent server-side) y persiste con PUT contra `owner_id`. Profile targets muestran notice "editas el titular del hogar" + heredan al instante.
>    - Componente reutilizable `LibraryAccessCheckboxes.tsx` (controlado, language-aware, lib-content-type tag). Usado por ambos modales.
>    - 3 vitest nuevos en `UsersAdmin.test.tsx` cubren: payload del create-with-grants, round-trip GET/PUT del kebab, routing profile→parent. **519/519** vitest verdes (era 516). `tsc --noEmit` clean. Vite arranca limpio.
>    - i18n: añadidas 9 keys en `es.json` + `en.json`. Las tests usan regex `|` para matchear ambos idiomas porque el fallbackLng es `en` y muchos valores de en.json siguen en español (traducción parcial preexistente del proyecto).
>    - **Detalle no obvio**: el `useEffect` que pre-checkea libraries en el Add User modal seedea SOLO una vez (cuando modal opens AND newGrantLibraryIds.length === 0). Si el admin destickea todo y luego cierra+reabre, el modal SÍ vuelve a tickear todo. Es UX intencionada — "abrir el modal" siempre arranca desde el estado por defecto.
>    - **Profile flow**: el handler frontend SIEMPRE manda el PUT contra `accessData.owner_id` (no contra el `accessTarget.id` original). El server YA rechazaría con 400 si mandases el id del profile; resolver en frontend es belt-and-braces.
>
> **Audit de seek-coalesce** (recomendación: **no tocar**):
>
> El status doc viejo listaba "refactor seek-coalesce" en P1. Audit a fondo (`internal/stream/manager.go:506+`, 4 tests dedicados en `manager_test.go:421-700`) reveló que el código YA está maduro:
> - AND-gate (segmento ≤2 + tiempo ≤300ms) tuneado contra el fanout de hls.js (~100ms, 3 segments) sin tragarse re-clicks humanos legítimos. Bug 2026-05-10 cubierto.
> - Sliding-window rate limit (20 restarts/60s) defendiendo contra el bug 2026-05-07 (cliente disparó 366 seeks por un click humano).
> - Per-session mutex (`ms.restartMu`) sin bloquear el resto del manager.
> - Constants HARDCODED a propósito — los comentarios dicen explícitamente que no son user-tunable porque son fixes empíricos a bugs concretos. Exponerlas en config invita a romper la coalesce policy.
>
> Opciones de "refactor" evaluadas y descartadas:
> 1. Config-tunable → anti-recomendado, invita regresiones.
> 2. Extraer `canCoalesce()` + `bumpRateLimit()` helpers → la función pasa de 100→50 líneas pero el flujo top-down actual lee como receta con why-comment por gate; fragmentar dispersa la comprensión. Win marginal.
> 3. AND-gate predictivo / ML → sobreingeniería.
> 4. Zombie-ffmpeg al expirar el 2s shutdown timeout → comentado como trade-off conocido; `-start_number` evita corrupción de archivos. Solo cosmético.
>
> **Senior call**: el código causó 2 incidentes en 1 semana y desde entonces convergió. Cualquier cambio aquí es churn sin valor. Item retirado del backlog activo.
>
> **Pendiente / próximo bloque sugerido**:
>
> El usuario está informado de las opciones del backlog en lenguaje llano (no jerga). Se le presentaron 3 candidates de mayor valor visible:
> - **Subs burn-in PGS/DVDSUB/ASS** (Blu-ray y subs animados que hoy no se ven porque son imágenes; hay que quemarlos sobre el vídeo en transcoding) — feature visible inmediato.
> - **Audio multichannel passthrough** (5.1/7.1 hoy baja a estéreo; relevante para home cinema/soundbar).
> - **VAAPI fully-on-GPU** (hoy mezcla CPU+GPU; pasarlo todo a GPU = más sesiones simultáneas con el mismo hardware).
>
> A la espera de que el usuario elija cuál atacar después del merge de los dos PRs activos.
>
> **Reglas duras que siguen vigentes**:
> - `library_access.user_id` ES SIEMPRE un top-level user (ADR-014). El frontend resuelve `owner_id` antes del PUT; el backend rechaza con 400 si llega un profile id.
> - El predicate strict (post-migración 040) bypassea para admin queries (admin ve todo).
> - sqlc 1.31.1 sigue siendo la última versión upstream. Comentarios en `.sql` files: ASCII estricto, sin em-dashes.
> - Tests del backend en Windows: usar docker (`golang:1.25-alpine` + `apk add build-base`) porque health.go usa `syscall.Statfs` no-portable a Windows. Issue preexistente, no tocado.
>
> **Métricas al cierre de la sesión**:
> - Backend: `go test -race ./...` → todos los paquetes verdes. golangci-lint v2.5.0 → 0 issues.
> - Frontend: `pnpm test --run` → **519/519** (+3 nuevos). `tsc --noEmit` clean. Vite dev arranca sin compile errors.
> - OpenAPI drift gate satisfecho con los 2 endpoints nuevos documentados.
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/review-project-1y28j`, modelo de acceso por hogar)** — el usuario pidió diseño de control de acceso per-user/group + M3Us personales. Audit reveló que el sistema YA existía parcialmente: tabla `library_access` per-user (opt-in: público por defecto) Y profile-system tipo Netflix vía `parent_user_id` (migración 034) que NO estaba integrado en el predicado de acceso.
>
> **Modelo final acordado con el usuario (re-formulación tras 2 rondas)**: el "hogar" se materializa REUSANDO los profiles existentes. Top-level user (parent_user_id NULL) = dueño del hogar. Profiles (parent_user_id != NULL) = miembros que heredan acceso. Los grants en `library_access` apuntan SIEMPRE al top-level user. No hay tabla `households` ni `groups`. El admin gestiona todo (v1 no expone self-service para invitar miembros).
>
> **Commit en esta sesión** (pusheado a `origin/claude/review-project-1y28j`):
>
> - `748d640` `feat(access): household model via profiles + strict library_access`. Migración 040 + reescritura del predicado en 4 surfaces (home_repository ×4 EXISTS, library_repository UserHasAccess, library_repository ListForUser).
>
> **Migración 040** (`040_households.sql`, idempotente, ~25 líneas):
> 1. Promueve grants per-profile al parent (INSERT OR IGNORE → si ya había grant del parent no duplica).
> 2. Borra grants huérfanos contra profiles.
> 3. Backfill: bibliotecas SIN grants (eran "públicas" en el modelo viejo) reciben grant explícito hacia cada top-level user no-admin existente para preservar visibilidad. Pre-v1.0 squash limpia esto al consolidar schema.
>
> **Cambio de predicado** (modelo strict: sin fallback público):
> ```sql
> EXISTS (
>   SELECT 1 FROM library_access la
>   JOIN users u ON u.id = ?
>   WHERE la.library_id = X
>     AND la.user_id = COALESCE(u.parent_user_id, u.id)
> )
> ```
> Reemplaza al viejo `EXISTS(...) OR NOT EXISTS(...)`. Mismo número de `?` placeholders (1), drop-in en cada call site.
>
> **Holdouts raw SQL nuevos** (sqlc 1.31.1 trips):
> - `LibraryRepository.ListForUser` ya era sqlc, pasa a raw SQL en `library_repository.go` porque el JOIN-con-COALESCE trunca el ORDER BY (mismo patrón ya conocido). La query sqlc original se elimina; la nota cruza-referencia el bug en su sitio.
> - `UserHasAccess` ya era raw SQL, se reescribe in-place.
>
> **Decisiones senior tomadas**:
> 1. **No tabla `households`**: el usuario describió "hogares con miembros" pero EL CÓDIGO YA TENÍA PROFILES. Reusar es 25 LOC de migración vs 200+ LOC de tablas/repos/handlers nuevos para el mismo comportamiento funcional.
> 2. **Strict mode (sin fallback público)** porque el usuario dijo "el admin tiene control de todo". Backfill preserva data existente; en deploys fresh-install no hay nada que preservar.
> 3. **No exponer "invitar miembro" auto-servicio en v1**: el admin crea top-level users Y profiles. Self-service se añade después sin tocar el modelo de datos.
> 4. **`GrantAccess` espera SIEMPRE el top-level user id**. El handler admin tiene que resolver `COALESCE(parent_user_id, id)` antes de llamar al repo, o el grant queda huérfano (el predicate nunca lo consulta). Documentado en el doc-comment del método.
>
> **Tests añadidos**:
> - `TestLibraryRepository_Access` reescrito para el modelo strict (sin grants → 0 visible, con grant explícito → sólo esa biblioteca).
> - `TestLibraryRepository_Access_ProfileInherits` nuevo: cubre el contrato "profile hereda acceso de parent" y "revoke al parent quita acceso al profile en el mismo instante".
> - `home_repository_test.go` setups (`setupHomeTrendingTest`, Recommended × 2) actualizados para incluir `GrantAccess` (sin él, los rails quedan vacíos por el predicate strict).
>
> **M3U personal** queda implícito en el modelo: una biblioteca `content_type=livetv` con `m3u_url` y un solo grant en `library_access(user_id, library_id)` → solo ese top-level user y sus profiles ven los canales. No requiere modelo nuevo.
>
> **Pendiente para próxima sesión — Phase B (kick-off rápido)**:
>
> Phase A entregó el modelo de datos + predicado. Phase B es UX para que el admin opere el modelo sin tocar SQL. Orden sugerido y entry points:
>
> 1. **Endpoint POST `/api/v1/admin/users` con `grant_library_ids` opcional**.
>    - Hoy `internal/api/handlers/admin_users.go` tiene `CreateUser` que crea el row de users y nada más. Ampliar la request shape para aceptar un array de library_ids; el handler hace el INSERT + N×`libRepo.GrantAccess` en una sola tx.
>    - Validar que el caller resuelve top-level: si el body pide crear un profile (parent_user_id set), los grants deben ir al parent, no al profile recién creado (gotcha documentado en ADR-014).
>    - Test: nuevo en `admin_users_test.go` cubriendo "create user con grants en 1 POST".
>
> 2. **Endpoint POST `/api/v1/admin/users/{id}/profiles`** (crear miembro/profile bajo un top-level user). Probablemente ya exista parcialmente (mirar `user_repository.go:CreateProfile` y handler asociado). Solo verificar que esté expuesto en el router y testeado.
>
> 3. **Endpoint PUT `/api/v1/admin/users/{id}/access`** — reemplazar el set de grants de un usuario.
>    - Útil para la UI: el admin tickea/destickea bibliotecas y el frontend hace un PUT con el array completo.
>    - El handler hace diff (current grants vs new) y aplica grants/revokes para llegar al target. Idempotente.
>    - Bonus: si el `id` es un profile, error 400 "grants must target the top-level user".
>
> 4. **Frontend admin: panel "Usuarios"** con matriz de bibliotecas.
>    - `web/src/pages/admin/Users.tsx` ya existe. Añadir columna o expandible con checkboxes de bibliotecas.
>    - Endpoint nuevo a añadir: GET `/api/v1/admin/users/{id}/access` que devuelve `[library_id]` con los grants actuales.
>    - i18n strings en `web/src/i18n/locales/es.json` + `en.json`.
>
> 5. **Live TV agregando múltiples bibliotecas livetv del hogar**.
>    - `web/src/pages/LiveTV.tsx` hoy itera UNA biblioteca seleccionada. Audit: probablemente ya funcione con `ListForUser` post-Phase-A si la página solo pide "libraries content_type=livetv que veo"; verificar.
>    - Si necesita cambio: agregar canales de todas las livetv accesibles en la grid principal, con un badge "Origen: Mis canales" / "Origen: General" según library.
>
> **Reglas duras de Phase B** (a recordarle al asistente al inicio):
> - `library_access.user_id` ES SIEMPRE un top-level user. El handler resuelve antes de llamar al repo. ADR-014 + doc-comment de `GrantAccess`.
> - El predicate strict ya está en producción; cualquier endpoint admin que liste para el admin **debe saltarse el predicate** (rol=admin bypasea). No copiar el JOIN+COALESCE en queries que el admin consume.
> - sqlc 1.31.1 sigue siendo la última versión upstream. Si necesitas añadir queries nuevas, revisar primero ADR-013 + convenciones (ASCII en comments, evitar `?` en `NOT(...)`, evitar ORDER BY ... COLLATE con JOINs complejos).
>
> **Métricas al cierre**: 21 paquetes Go verdes con `-race`. 2 tests de access nuevos. Backfill testeado en CI vía migraciones aplicadas en cada `testutil.NewTestDB`.
>
> ---
>
> ⚠️ **sqlc 1.31.1 sigue siendo la última versión** (verificado 2026-05-11 contra `github.com/sqlc-dev/sqlc/releases`). El bug del parser que documentamos en `architecture-decisions.md` NO está arreglado upstream. Las 5+ queries en raw-SQL holdout (`ListProfilesForOwner`, refresh-token UPDATE, `SearchSharedItems`, y las 3 nuevas de federation poster colours) seguirán así hasta que upstream publique 1.32+. Cuando salga, abrir tarea de "migrar holdouts de vuelta a sqlc" — son ~15 min cada una. Por ahora **NO especular** sobre roadmap upstream cuando hablemos con el usuario.
>
> 🎬 **Sesión 2026-05-11 (rama `claude/review-project-1y28j`, federation poster colours)** — el usuario pidió cerrar el item "node-vibrant server-side". Audit en frío reveló que el grueso del trabajo YA estaba hecho (migración 014, `imaging/colors.go`, API expone `backdrop_colors` para items locales, `ItemDetail.tsx:167-177` ya hace `hasServerPalette` gate). El item real pendiente era SOLO federation: `PeerItemDetail` ejecutaba node-vibrant siempre porque `FederationRemoteItem` no transportaba colores.
>
> **Senior call**: cerrar el gap real (federation) en lugar de tocar el path local que ya funciona. Documentar que el item del backlog estaba parcialmente vencido.
>
> **Commits en esta sesión** (todos pendientes de push):
>
> - `feat(federation): plumb pre-extracted poster colours through SharedItem` — `federation.SharedItem` gana `PosterColor` + `PosterColorMuted` (mismas CSS rgb() strings que la tabla `images`). Los 3 decoders de `client.go` los reenvían. El emitter (peer-side) los pinta a partir de un batch SELECT raw-SQL (`attachPrimaryImageColors`) llamado tras `ListSharedItems` / `ListRecentSharedItems` / `SearchSharedItems` — un solo query extra por página, sin N+1.
>
> - `feat(api): expose backdrop_colors on /me/peers item wire` — `peerItemWire` + `peerSearchHitWire` ganan `backdrop_colors: { vibrant, muted }`, mismo wire shape que el handler de items locales emite. `paletteFromShared` lifter devuelve `nil` cuando ambos swatches están vacíos para que `omitempty` los drope (peers antiguos que no emiten estos campos quedan exactos como antes).
>
> - `feat(web): PeerItemDetail consumes server backdrop_colors first` — `FederationRemoteItem` + `federationItemToMediaItem` forwardean los colores. `PeerItemDetail` aplica el mismo patrón que `ItemDetail`: `hasServerPalette` gate desactiva el fallback URL así `useVibrantColors` no carga `node-vibrant/browser` (~400KB) cuando el peer ya mandó la paleta. Items pre-014 o peers viejos siguen yendo al fallback runtime sin regresión visual.
>
> - `feat(db): migration 039 + cache colour round-trip` — `federation_item_cache` gana `poster_color` + `poster_color_muted` (default `''`). `UpsertCachedItems` / `ListCachedItems` quedaron en raw SQL holdouts para que la cache offline también arrastre colores. Sin esto, ir offline silenciosamente dropeaba la paleta y el frontend volvía a node-vibrant.
>
> - `test`: backend `TestFederationRepository_SharedItem_ColorsForwarded` cubre los 3 emitters + cache roundtrip. Frontend `federationAdapter.test.ts` con 4 tests (full palette / half palette / absent / preserves-other-fields).
>
> **Decisiones senior tomadas (no obvias del código)**:
>
> 1. **Raw SQL batch en `attachPrimaryImageColors`, NO subquery inline en sqlc**: probé pintar las dos `dominant_color*` columns vía `COALESCE((SELECT … LIMIT 1), '')` correlated subqueries dentro de `ListSharedItems` / `ListRecentSharedItems`. sqlc 1.31.1 trunca el output al combinar eso con `ORDER BY ... COLLATE NOCASE` — bug estructural ya documentado en `architecture-decisions.md`. Pivot a un batch `SELECT … WHERE item_id IN (?…)` post-query: 1 query extra por página, sin tocar sqlc, sin N+1.
>
> 2. **Cache table → raw SQL holdouts**: el segundo intento (añadir las 2 columnas al `INSERT INTO federation_item_cache (…)` con 11 placeholders) también tropezó el parser. Movimos `UpsertCachedItems` y la query de lectura a raw SQL en el repo y dejamos los queries sqlc en su shape pre-cambio. Precedente: `ListProfilesForOwner`, `SearchSharedItems`. Comentarios cruzados explican el por qué.
>
> 3. **Federation peers viejos = backwards compat**: el wire shape ahora trae `poster_color` con `omitempty`. Un peer que pre-data este cambio simplemente no lo envía, `paletteFromShared` devuelve `nil`, omitempty drope el campo, el frontend ve `backdrop_colors: undefined`, `hasServerPalette` falla, runtime fallback se enchufa. Cero rupturas.
>
> 4. **No expandir el emitter a 5 swatches (vibrant/muted/darkVibrant/lightVibrant/lightMuted)**: el backend (migración 014, `imaging/colors.go`) solo computa 2. Replicar el algoritmo de node-vibrant en Go server-side daría aurora con más variación pero es trabajo nuevo, y `aurora.ts` ya tiene fallback chains que llenan las 4 esquinas con solo `vibrant`+`muted`. Si en el futuro queremos paridad exacta, el sitio para iterar es `internal/imaging/colors.go`.
>
> 5. **No backfill automático**: items ya escaneados antes de migración 014 quedan sin colores; cuando el operador re-escanea o el `ImageRefresher` los toca, los colores aparecen. Backfill explícito sería un script separado fuera de scope.
>
> **Estado del wire shape al cierre**:
>
> ```text
> /api/v1/me/peers/{peerID}/libraries/{libID}/items
>   items[].backdrop_colors?: { vibrant?, muted? }  ← NEW
> /api/v1/me/peers/search?q=…
>   hits[].backdrop_colors?: { vibrant?, muted? }   ← NEW
> /api/v1/me/peers/recent
>   hits[].backdrop_colors?: { vibrant?, muted? }   ← NEW
> /api/v1/peer/libraries/{libID}/items   (peer→peer)
>   items[].poster_color?, poster_color_muted?     ← NEW
> /api/v1/peer/search?q=… , /api/v1/peer/recent    (peer→peer)
>   items[].poster_color?, poster_color_muted?     ← NEW
> ```
>
> **Métricas al cierre**:
> - Backend Go: `go test -race ./...` → 21 paquetes verdes. Federation repo tests +1 test nuevo (4 subtests).
> - Frontend: `pnpm test --run` → **516/516** (era 512, +4 nuevos).
> - `go build ./...` + `tsc --noEmit` clean. sqlc regen idempotente sobre el shape final.
>
> **Backlog actualizado**:
> - **Frontend P1**: tests grandes para `pages/` (Home, LiveTV, Search, Movies, Series, Collections). ~~node-vibrant server-side~~ ✅ cerrado (local YA estaba; federation cerrado hoy).
> - **Streaming P1**: subs burn-in PGS/DVDSUB/ASS, audio multichannel passthrough, refactor seek-coalesce, `-force_key_frames` GOP-aligned, VAAPI fully-on-GPU.
> - **Infra P1**: vacío.
> - **Features grandes P3**: Multi-version, Watch-together, Privacy stack.
>
> **Posible follow-up identificado** (no scope hoy):
> - Backfill script para items pre-014 sin colores extraídos.
> - Expandir el extractor server-side a 4-5 swatches para paridad con node-vibrant. Hoy `aurora.ts` rellena con fallback chains cuando solo hay 2.
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/review-project-1y28j`, bloque polling → SSE)** — tras cerrar el bloque infra/seguridad, el usuario pidió "Revisa lo del polling" y luego "Las dos a SSE" entre las migraciones que recomendé.
>
> **Audit del polling restante** (verificado contra el código):
>
> | Hook | Antes | Verdict |
> |---|---|---|
> | `useAdminStreamSessions` (admin Now Playing) | **5s** | ✅ migrado a SSE |
> | `useMySessions` ("Tus dispositivos") | 30s | ✅ migrado a SSE |
> | `useSystemStats` (Dashboard + SystemStatus) | 30s | Stay — snapshot agregado, polling apropiado |
> | `useHomeLiveNow` | 60s | Stay — EPG slots cambian cada ~30 min, polling marginal |
> | `useBulkSchedule` | 5 min | Stay — schedule futuro, baja frecuencia |
> | (setInterval para timers/relojes/progress writer) | — | Stay — no son polling de datos |
>
> **Hallazgo no obvio del audit**: el evento `transcode.started/completed` que parecía solo cubrir transcodes, en realidad fire para TODAS las sesiones que el manager mantiene en `m.sessions` (DirectStream + Transcode). DirectPlay (`manager.go:379-392`) NO entra al map — pero tampoco aparece en `ListAllSessions()`, así que el evento cubre exactamente lo que la API admin lista. Nombre legacy engañoso, semántica correcta para el caso de uso. Documentado en el comentario del hook nuevo.
>
> **Commits en esta sesión** (todos pendientes de push):
>
> - `feat(auth): publish UserLoggedOut on RevokeSession` — el endpoint era silencioso, ahora emite el mismo payload shape que `Logout` (`user_id` + `session_id`). El SSE consumer del frontend invalida la query y la fila de la sesión revocada desaparece dentro de ~50ms en vez de esperar el siguiente poll. +2 tests Go (PublishesUserLoggedOut + ForeignSession_NoPublish — el segundo blinda el anti-enumeration carve-out de no publicar cuando la sesión pertenece a otro usuario).
>
> - `feat(api): forward user.logged_in/out through /me/events` — añadido a `userScopedEventTypes` en `me_events.go`. El per-user filter del handler ya hace el matching por `Data["user_id"]`; los dos eventos ya emiten ese campo desde antes (`auth.Login`, `auth.Logout`), así que el split es zero-extra-code en el filtro.
>
> - `feat(web): drive useMySessions + useAdminStreamSessions from SSE` — dropea `refetchInterval` en ambos hooks, suscribe a los eventos correspondientes vía `useUserEventStream` / `useEventStream`, invalida la query en cada evento. Para `NowPlayingPanel`, añadido `useNowTick(1000)` para que la columna "elapsed" siga subiendo entre eventos (antes el re-render lo daba el 5s refetch).
>
> - `test(web): cover the two new hook subscriptions` — `auth.test.tsx` + `system.test.tsx` con FakeEventSource (mismo patrón de `useUserDataSync.test.tsx`). 8 tests nuevos: no-polling-after-mount, subscribes-to-correct-types, invalidates-on-each-event (×2 per hook).
>
> - `test(setup): default no-op EventSource stub in vitest setup` — sin esto, cualquier componente que consuma un hook con SSE crashea en tests no relacionados (DevicesPanel.test.tsx fue el canario). Tests que necesiten driving events siguen pudiendo override con `vi.stubGlobal`.
>
> **Decisiones senior tomadas**:
>
> 1. **Suscripción dentro del hook, no en cada panel**: `useAdminStreamSessions` tiene 2 consumers (`NowPlayingPanel`, `SystemStatus`). Si el SSE wiring vive en cada panel, dos sitios para mantener y dos lugares para olvidar. Dentro del hook se hace una vez y cada panel hereda sin saberlo. Trade-off conocido: tests que mockean `api` ahora también necesitan EventSource — resuelto con el setup global.
>
> 2. **Reusar `UserLoggedOut` para `RevokeSession`, no nuevo type**: el shape del payload (`user_id` + `session_id`) es idéntico al de `Logout`, y el consumer (la query invalidation) trata ambos identical. Añadir `SessionRevoked` event sería plumbing extra sin información nueva.
>
> 3. **`transcode.started/completed` legacy name kept**: renombrar a `stream.session.started/stopped` sería más claro pero requiere coordinar listeners (subscriptions en `events.go`, frontend metrics, observability) — beneficio cosmético, coste real. Comentario en el hook nuevo explica la semántica.
>
> 4. **`useNowTick(1000)` solo en NowPlayingPanel**: `SystemStatus` también consume `useAdminStreamSessions` pero no muestra elapsed; añadir el tick allí sería waste. El panel sabe qué necesita re-render.
>
> 5. **NoopEventSource en setup.ts, no en cada test**: el stub global previene crashes cross-test sin imponer una API a los tests que sí drivean events. Vi.stubGlobal en tests específicos sigue funcionando y reemplaza el noop por la subclase con `.emit()`.
>
> **Métricas al cierre**:
>
> - Backend Go: `go test -race ./internal/auth/ ./internal/api/...` → ok 106s + 78s + 68s, todos verdes (incluído los 2 RevokeSession tests nuevos).
> - Frontend: `pnpm test --run` → **512/512** verde (era 504, +8 nuevos).
> - `go build ./...` + `tsc --noEmit` clean.
>
> **Backlog actualizado**:
>
> - **Streaming P1** (del audit del 2026-05-10, sin tocar): subs burn-in PGS/DVDSUB/ASS, audio multichannel passthrough, refactor seek-coalesce, `-force_key_frames` GOP-aligned, VAAPI fully-on-GPU.
> - **Frontend P1**: `node-vibrant` sigue 100% client-side, tests grandes para `pages/` (Home, LiveTV, Search, Movies, Series, Collections). ~~Polling residual~~ ✅ migrado (los dos targets reales del backlog).
> - **Infra P1**: vacío.
> - **Features grandes P3**: Multi-version, Watch-together, Privacy stack.
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/review-project-1y28j`, bloque de infra/seguridad)** — el usuario pidió "revisa mi proyecto para seguir haciendo cosas" + eligió el bloque "Trivy pin + CSRF + SSE cap" + "que me recomiendas? quiero que sea robusto y bien hecho".
>
> **Recomendación senior tomada**: implementar SSE cap (gap real) + pinear Trivy a SHA (mejor práctica industria, el comentario que justificaba `@master` pasaba por alto que la imagen se publica a GHCR pública y la consumen terceros) + cerrar CSRF como "intentional by design, no action" (la auditoría flagueaba "fail-open cuando no hay session cookie" pero el código + tests demuestran que es la elección correcta — sin sesión autenticada no hay nada que proteger).
>
> **Commits en esta sesión**:
>
> - **Trivy pin a SHA**: `aquasecurity/trivy-action@master` → `@ed142fd0673e97e23eac54620cfb913e5ce36c25 # v0.36.0` en `.github/workflows/docker.yml`. Comentario refactorizado: la justificación previa ("self-hosted single-tenant, latency-of-fix outweighs supply-chain risk") era débil porque la imagen va a GHCR pública. Bump path documentado (releases de aquasecurity/trivy-action). Renovate/dependabot puede levantarlo automáticamente.
>
> - **SSELimiter compartido**: nuevo `internal/api/handlers/sse_limiter.go` con caps `Default=100 global`, `Default=5 per-user`. Tres handlers (`events.go` global, `me_events.go` user-scoped, `admin_logs.go` admin) ahora consumen UNA instancia inyectada vía `api.Dependencies.SSELimiter`. Cada handler llama `limiter.Acquire(userID)` ANTES de flush de headers — una vez que la SSE response arranca el status code está locked-in. Sobre el cap: 503 + `Retry-After: 30`. Release es idempotente (sync.Once interno) por si el handler hace defer doble.
>
> - **Tests**: `sse_limiter_test.go` con 6 tests (under-caps, idempotent release, anonymous-only global, map cleanup, concurrent acquire/release race-detector smoke, defaults-when-zero) + `TestMeEvents_RejectsWhenPerUserCapReached` integration test (verifica 503 + Retry-After + release-on-disconnect cycle).
>
> **Decisiones senior tomadas (no obvias del código)**:
>
> 1. **Per-user cap = 5, global cap = 100**: dimensionado para self-hosted household-scale. Usuario legit típico = 2-3 tabs; >5 concurrentes per-user es runaway reconnect loop. Si una deployment crece, los caps se lift via config (no constants tweak).
>
> 2. **Acquire ANTES de WriteHeader**: una vez que el flusher escribe `Content-Type: text/event-stream` el status code está fijo a 200. Si quisiéramos rechazar a la mitad tendríamos que cerrar la conexión sin status — feedback ambiguo para el cliente.
>
> 3. **Anonymous userID="" cuenta solo para global**: carve-out futuro-proof. Hoy las 3 SSE surfaces requieren auth (todas dentro de `r.Use(deps.Auth.Middleware)`), pero si llega una pública (status SSE, p.ej.), no queremos que la nada-userID exhausta una sola entrada del mapa.
>
> 4. **Sync.Once interno en release**: defer + double-call defensivo. El cost del Once es trivial (~ns) y la idempotencia simplifica el código de los handlers — no necesitan trackear "ya releazé".
>
> 5. **`SSELimiter` opcional (nil = no cap)**: tests pasan nil y los handlers skipean la enforcement. Producción wira un SSELimiter shared en main.go. Mismo pattern que `LogBuffer` — nil-safe en construction, requerido en runtime para producción.
>
> 6. **Trivy bump path documentado**: en lugar de "actualiza cuando puedas" el comentario apunta explícitamente a `https://github.com/aquasecurity/trivy-action/releases` y obliga a mantener SHA + version comment juntos. Renovate path explícito.
>
> 7. **CSRF "fail-open" cerrado sin código nuevo**: relectura cuidadosa de `csrf.go:74-79` + `csrf_test.go` (5 tests, incluído `TestCSRF_MutatingWithoutSessionCookieAllowed`) confirma que el behavior está bien diseñado y testeado. Atacante sin la cookie del víctima no puede setearla cross-origin (SameSite=Lax), y sin sesión autenticada no hay nada que CSRF pueda explotar. Cerrado como "intentional, no action".
>
> **Métricas**:
>
> - Backend Go: `go build ./...` clean. `go test -race -count=1 ./internal/api/...` → ok 67s + 55s, todos verdes incluyendo los 7 SSE tests nuevos.
> - Frontend: sin cambios (sesión 100% backend/infra).
>
> **Backlog actualizado al cerrar sesión** (filtrado de los items que ya están hechos):
>
> **Streaming P1** (del audit del 2026-05-10):
> - Burn-in PGS/DVDSUB/ASS (sin soporte nativo del browser).
> - Audio multichannel passthrough (siempre baja a `aac stereo`).
> - Refactor seek-coalesce (3 capas defensivas tras `7f6a053`).
> - `-force_key_frames` GOP-aligned en transcode args.
> - Pipeline VAAPI fully-on-GPU.
>
> **Frontend P1**:
> - Polling 5s/30s residual donde debería ser SSE.
> - Tests grandes para `pages/`: Home, LiveTV, Search, Movies, Series, Collections.
> - **node-vibrant** sigue 100% client-side (`useVibrantColors.ts`). Plex/Jellyfin lo hacen server-side + cache.
>
> **Infra/seguridad P1** (estado real al cierre):
> - ~~`RateLimitConfig.GlobalRPM` dead code~~ ✅ done (commit `dc4c741`).
> - ~~YAML 0644 → 0600~~ ✅ done (commit `2a4a3b7`).
> - ~~`govulncheck` en CI~~ ✅ done (commit `9cee31e`).
> - ~~hls.js sin chunk separado~~ ✅ done (commit `0c870ae`).
> - ~~HDR/10-bit decision + tone-mapping~~ ✅ done (commit `392f1da`).
> - ~~Trivy pinned a SHA~~ ✅ done en esta sesión.
> - ~~CSRF middleware "fail-open"~~ ✅ cerrado como intentional, sin código.
> - ~~No connection cap en SSE~~ ✅ done en esta sesión.
> - (no quedan items P1 de infra/seguridad activos)
>
> **Features grandes pendientes (P3, sin tocar)**:
> - Multi-version del mismo título (4K + 1080p agrupados con picker).
> - Watch-together (sync WebSocket sessions).
> - Privacy stack (modo offline NFO, egress allowlist, CSP estricto).
>
> ---
>
> 🎬 **Sesión 2026-05-11 (rama `claude/review-project-status-SBKN4`, 2 commits, todo pusheado)** — el usuario pidió "revisa mi proyecto para ver por dónde seguir o si está la memoria desactualizada" y "haz como senior lo que creas mejor".
>
> **Hallazgo principal — el backlog estaba sustancialmente desactualizado**. Audit honesto contra el código actual descubrió que **5 items que estaban listados como "pendientes" en el handoff del 2026-05-10 ya estaban implementados** (algunos hace semanas), y otros tenían sutilezas que cambiaban su prioridad:
>
> | Item del backlog "pendiente" | Estado real |
> |---|---|
> | Hero del home auto-play | ✅ Ya hecho — `HeroBanner.tsx:266-273` usa `playHrefFor()` que devuelve `?play=1`. |
> | Next-up overlay automático | ✅ Ya hecho — `VideoPlayer.tsx:501-502`, `setUpNextActive(true)` en `ended`. |
> | Skip-intro/credits visible en player | ✅ Ya hecho — `SkipSegmentButton` shipped en Phase 1 (`3da1a01`). |
> | **Skip-intro Phase 2 (chromaprint)** | ✅ Ya hecho — `internal/library/fingerprint.go` + `segment_matcher.go` + `segment_fingerprinter.go` + `libchromaprint-tools` en Dockerfile. La memoria decía "ready to plug in" pero está completamente enchufado. |
> | Aurora colors aplicado al detail | ✅ Ya hecho — `web/src/pages/itemDetail/aurora.ts` con `buildAuroraStyle()` + test. |
> | `manualChunks` en vite | ⚠️ Parcial — `react`, `router`, `query` separados; `hls.js` sigue sin split. |
>
> **2 commits pusheados a `origin/claude/review-project-status-SBKN4`**:
>
> - `39f01c7` *docs(memory): sync audit handoff with PR #239 follow-up* — corrige el handoff del 2026-05-10 que decía `git push origin main` pendiente (ya estaba mergeado vía PR #238) y omitía el follow-up `59dbad0` *fix(livetv): make ?channel=<id> deep-link idempotent against re-render churn* (PR #239, freeze de 45s+ al pinchar canales desde el rail "En directo ahora" del Home).
>
> - `d7ea9ee` *feat(home): row actions on Continue Watching cards — mark watched + remove* — cierra los dos items reales del backlog UX:
>   - Backend: `DELETE /me/continue-watching/{itemId}` con nuevo `UserDataRepository.ClearProgress`. Distinto semánticamente de `MarkPlayed` (miente sobre completion) y `MarkUnplayed` (nukea todo el row): zeroes `position_ticks` preservando `play_count`, `is_favorite`, `last_played_at`. Emite `event.ProgressUpdated` con `position_ticks=0` para que `useUserDataSync` invalide el CW rail en otros devices vía SSE existente — cero event types nuevos. Idempotente.
>   - Frontend: `LandscapeCard` gana props opcionales `onMarkWatched` + `onRemove`. Overlay hover top-left con Check + X (rating badge sigue top-right, sin colisión). Buttons hacen `preventDefault + stopPropagation` para no disparar el `<Link>` envolvente. `useRemoveFromContinueWatching` con optimistic update + rollback on error. `useMarkPlayed` reusa el endpoint existente.
>   - i18n: 2 strings nuevas (`home.markWatched`, `home.removeFromContinueWatching`) en ES + EN.
>   - OpenAPI: el endpoint nuevo documentado bajo `paths:` (es user-facing, no operator-only).
>   - Tests: 2 Go nuevos (zeroes-preserves-rest + idempotent-no-row) + 4 vitest nuevos (no-buttons-without-handlers + mark-watched-fires + remove-fires + clicks-do-NOT-navigate). **504/504 vitest verdes** (era 500).
>
> **Decisiones senior tomadas**:
> 1. **`ClearProgress`, no `MarkUnplayed` reuse**: el endpoint existente nukea `play_count` + `is_favorite`. Para "quitar de Seguir viendo" eso es colateral damage — el usuario no está diciendo "no he visto esto", está diciendo "no me lo muestres aquí". Zeroes `position_ticks` y nada más.
> 2. **sqlc safe pasa el query nuevo**: 3 placeholders, generación limpia sin tocar los bugs documentados (ORDER BY+COLLATE, UPDATE con 4+ placeholders). Si futuros queries vuelven a topar con los bugs, raw SQL holdout sigue siendo el patrón.
> 3. **Reuse `ProgressUpdated` event, no nuevo type**: `useUserDataSync` ya invalida CW en este evento. Añadir `RemovedFromCW` event sería duplicar plumbing por nada — el shape del payload (`position_ticks: 0`) ya transmite la información.
> 4. **Overlay top-LEFT, no top-right**: el rating badge ya vive top-right. Splitear left/right evita colisión en lugar de stack.
> 5. **OpenAPI yaml, no allowlist**: es endpoint user-facing del SPA. Las paths operator-only (admin keys, federation pairing) sí quedan en allowlist; las user-facing migran.
>
> **Métricas reales al cierre (la guía CLAUDE.md estaba desactualizada desde 2026-04-17)**:
> - .go production: 224 (era 97) · _test.go: 137 (era 53) · ratio ~61%.
> - frontend tests: 67 files / 504 assertions (era 12 / "~15%").
> - HTTP routes: 217 (era 74).
> - Handlers: 29 con test, 21 sin (mayoría thin wrappers / admin-only / federation passthroughs).
> - **CLAUDE.md actualizada con estas métricas en esta sesión**.
>
> **Backlog real que queda al cerrar la sesión** (filtrado de los items que no estaban realmente pendientes):
>
> **Streaming P1** (del audit del 2026-05-10, sin tocar):
> - HDR/10-bit decision + tone-mapping (`stream/decision.go` ignora `BitDepth`/`ColorTransfer`/`Profile`).
> - Burn-in PGS/DVDSUB/ASS (sin soporte nativo del browser).
> - Audio multichannel passthrough (siempre baja a `aac stereo`).
> - Refactor seek-coalesce (3 capas defensivas tras 3 commits seguidos).
> - `-force_key_frames` GOP-aligned en transcode args.
> - Pipeline VAAPI fully-on-GPU.
>
> **Frontend P1**:
> - **hls.js** sigue sin chunk separado (~400KB en el bundle principal). React/router/query sí están en manualChunks.
> - Polling 5s/30s residual donde debería ser SSE.
> - Tests grandes para `pages/`: Home, LiveTV, Search, Movies, Series, Collections.
> - **node-vibrant** sigue 100% client-side (`useVibrantColors.ts`). Plex/Jellyfin lo hacen server-side + cache.
>
> **Infra/seguridad P1**:
> - `RateLimitConfig.GlobalRPM` confirmado dead code (declarado en `config.go:158`, 0 lectores).
> - YAML config probablemente persistido a `0644` (sin evidencia de fix; chmod 0600 no aparece en `internal/config/`).
> - CSRF middleware exists (`internal/api/csrf.go`); el "fail-open cuando no hay session cookie" del audit no he verificado en esta sesión.
> - **`govulncheck` sigue ausente en CI** (`.github/workflows/`). Trivy sigue pinned a `@master` (supply-chain risk).
> - No connection cap en SSE.
>
> **UX adicionales que el usuario mencionó pero NO atacamos** (filtrado del original):
> - ~~Quitar item de CW~~ ✅ hecho hoy.
> - ~~Marcar como visto desde la rail~~ ✅ hecho hoy.
> - ~~Hero del home auto-play~~ ✅ estaba ya.
> - ~~Skip-intro/credits visible~~ ✅ Phase 1 + Phase 2 shipped.
> - ~~Next-up overlay automático~~ ✅ estaba ya.
> - ~~Aurora colors al detail~~ ✅ estaba ya.
> - **Todo cerrado**. No quedan UX explicitos del bloque del audit. Si el usuario pide nuevos, abrir entrada propia.
>
> **Features grandes pendientes (P3, sin tocar)**:
> - Multi-version del mismo título (4K + 1080p agrupados con picker).
> - Watch-together (sync WebSocket sessions).
> - Privacy stack (modo offline NFO, egress allowlist, CSP estricto).
>
> ---
>
> 🎬 **Sesión 2026-05-10 (auditoría senior + 11 commits, rama `claude/relaxed-lederberg-48e836`)** — el usuario pidió "revisa mi proyecto, quiero comprobar cosas que todo lo que tenemos funciona de verdad… piensa como senior". Auditoría completa con 4 sub-agentes en paralelo (backend / frontend / streaming-IPTV / infra-seguridad) + verificación visual end-to-end via Chrome MCP. Encontrados 5 P0 reales, los 5 arreglados + 5 mejoras UX adicionales. **Todo pusheado a `origin/claude/relaxed-lederberg-48e836` y mergeado a `main` local vía fast-forward**. Detalle completo en [`session_2026-05-10_audit_p0_fixes.md`](session_2026-05-10_audit_p0_fixes.md).
>
> **5 P0 fixes**:
> - `5b47630` *i18n(es): restore accents and ñ across 147 strings* — "Contrasena", "Iniciar sesion", "Administracion", "Anadir a favoritos"… en cada pantalla. Audit exhaustivo identificó 147 entradas en `es.json` (no 78 como decía el primer grep). Script Python one-shot, idempotente, validación JSON-OK al final.
> - `251cd7a` *fix(db): bypass sqlc 1.31.x parser bug for ListProfilesForOwner* — `/api/v1/me/profiles` daba 500 SQL syntax error en cada llamada (selector "Who's watching?" roto). Causa: bug del codegen de sqlc 1.31.1 que añade `?;` espurio + trunca el último token cuando ORDER BY combina expresión booleana + COLLATE NOCASE. Fix: raw SQL en `user_repository.go` siguiendo el patrón de los otros 5 holdouts.
> - `7c9149d` *fix(api): require active setup wizard for browse/libraries/settings/complete* — `/setup/browse` listaba filesystem (`/home`, `/srv`, `/var/lib`, `/config`, librerías) sin auth para siempre. Helper `requireSetupActive` con 403 SETUP_COMPLETE; `Status` queda público para el on-boot del SPA.
> - `73418a9` *feat(auth): rotate refresh token on every successful refresh* — antes el RT se reusaba durante todo el RefreshTokenTTL (30 días) sin detección. Rotación atómica + UPDATE hand-rolled (sqlc trunca UPDATEs con 4+ placeholders, mismo bug que profiles).
> - `97c6698` *feat(auth): refresh-token reuse detection with chain revocation* — migración 038 añade `previous_refresh_token_hash` + índice. Auth0-style: si llega un RT que matchea el previous (no el current) = reuse → revoca toda la sesión + WARN log. One-step memory (no full chain) suficiente para el threat "atacante leak + race vs dueño".
>
> **6 mejoras UX adicionales pedidas por el usuario al iterar**:
> - `aa223a8` *test: regression coverage for the four P0 fixes* — 268 LOC de tests nuevos (4 `internal/db/...`, `internal/auth/...`, `internal/api/handlers/...`). `go test ./...` verde via `golang:1.25` docker (host sin go en PATH).
> - `3be7dce` *feat(home): launch player directly from Continue Watching cards* — antes click en CW paraba en detail page. `LandscapeCard` gana prop `autoPlay` que tagea href con `?play=1`. ItemDetail ya tenía el deep-link consumer wireado pero las rails no lo usaban.
> - `ee6836c` *fix(player): resume from saved progress + smarter back-out target* — handlePlay ahora lee `user_data.progress.position_ticks` del item y lo pasa como `startPosition`. Cuando se cierra un player auto-played, episodios → season list (`/items/${parent_id}`), movies → `navigate(-1)`. Manual play sin tocar.
> - `f488ea0` *feat(layout): universal back arrow in the top bar* — ArrowLeft visible en cada `pathname !== "/"`. Click → `navigate(-1)`. Edge case URL-directa: si `window.history.state.idx === 0`, fallback a `navigate("/")`.
> - `d78f17d` *feat(home): movies in Continue Watching show their poster, not the backdrop* — intermedio: PosterCard vertical 2:3 para movies, mezclado con LandscapeCard 16:9 para episodios. **Reverted en `6c046ce`** porque rompía el rhythm de la rail y el usuario aclaró que existe un tipo "Miniatura" específico.
> - `6c046ce` *feat(images): expose thumb (16:9 miniatura) and use it in Continue Watching* — backend ahora expone `thumb_url` en `/me/continue-watching` y `/items/{id}` (campo de DB que ya existía pero no estaba on the wire). `ImageRepository.GetPrimaryURLs` añade `type='thumb'` al SELECT. `LandscapeCard` para movies prefiere `thumb_url > backdrop_url > poster_url`. Rail vuelve a uniform 16:9.
>
> **Decisiones senior tomadas (no obvias del código)**:
> 1. **sqlc 1.31.1 bug es estructural, no edge case**: visto 3 veces (ORDER BY + COLLATE, UPDATE con 4+ placeholders, comentario pre-existente). Las 2 queries afectadas pasaron a raw SQL con comment cruzado. Recomendación: bumpar sqlc cuando salga el fix, migrar de vuelta.
> 2. **Player VOD bug = imagen Docker obsoleta, no código** — el commit `44566b9` ya tenía el fix; el contenedor del usuario corría `hubplay:fingerprint` de 17h atrás. Rebuild solucionó el "bug crítico". **Lección**: en cada audit, verificar la fecha del binary corriendo vs último commit relevante en main.
> 3. **Reuse-detection one-step (no full chain)** suficiente para el threat model self-hosted: el escenario "atacante con RT N rotaciones viejo" coincide con "dueño activo y rotando", la ventana de ataque desaparece. Auth0 keeps full chain pero el coste (insertar fila por refresh + cleanup) no se justifica aquí.
> 4. **Setup `Status` queda público intencional** — el SPA lo polea on-boot para decidir si redirigir al wizard. Carve-out pinned por `TestSetupHandler_PostCompletion_Status_StillOpen` para que un futuro "lock everything" refactor no rompa el flow.
> 5. **Resume position desde `getItem`, no `getProgress`** — el endpoint `/me/progress/{id}` devuelve un shape flat (`position_ticks` directo) que no matchea el tipo declarado `UserData` (nested `progress.position_ticks`). El client TypeScript habría leído undefined silenciosamente. `getItem` devuelve el shape canónico que el resto del codebase ya consume.
> 6. **Back-out tras auto-play va a season list para episodios**, no `navigate(-1)` directo — el detail page del episodio individual existe sólo como redirect target intermedio; mostrarla post-playback es jarring. Movies no tienen ese problema (su detail page es la canonical).
> 7. **Thumb backend: añadirlo a la API, no construir URL en cliente** — el campo existe en DB (`type='thumb'`), no había razón para no exponerlo. Una llamada al `/items/{id}/images` por movie sería N+1 + duplicaría conocimiento de qué tipos hay. La opción D (extender el wire shape) deja el frontend con un access pattern consistente.
>
> **Métricas**:
> - Backend Go: `go test -count=1 ./internal/db/... ./internal/auth/... ./internal/api/handlers/...` verde via `golang:1.25` docker (~55s total). El flake `TestFSWatcher_NewSubdirGetsWatched` sigue presente, no tocado.
> - Frontend: `pnpm exec tsc --noEmit` clean. Vitest no corrido en esta sesión (cambios de data-only en es.json + UI components sin lógica nueva).
> - Container: `hubplay` (`ghcr.io/alexzafra13/hubplay_demo:latest`) recién built, healthy en :8097, mount apunta al config padre con la DB de 130MB del usuario.
>
> **Estado del worktree** (actualizado tras follow-up):
> - 11 commits en `claude/relaxed-lederberg-48e836`, PR #238 mergeada a `main` (`afae026`).
> - Post-merge follow-up: `59dbad0` *fix(livetv): make ?channel=<id> deep-link idempotent against re-render churn* (PR #239, merge `aa01f14`) — el useEffect que auto-reproducía un canal al venir del rail "En directo ahora" del Home spiraleaba por re-render churn (`openPlayer` dep rebuilt en cada render porque `channels` array ref cambia) y con React 19 concurrent mode podía colgar la página >45s. Fix con `handledChannelRef` idempotente per-channel-id + eliminado el strip-on-not-found que perdía deep-links silenciosamente cuando la lista no había hidratado aún.
> - `origin/main` y local main ya sincronizados.
>
> **Backlog que queda explícito (próxima sesión)**:
> - **Streaming P1** (del audit inicial): HDR/tone-mapping, subtítulos burn-in (PGS/ASS), audio multichannel passthrough, GOP-aligned `-force_key_frames`, refactor del seek-coalesce (3 capas defensivas), VAAPI fully-on-GPU.
> - **Frontend P1**: polling 5s/30s residual → SSE, vite `manualChunks` (hls.js no se separa), tests para Home/LiveTV/Search/Movies/Series, `node-vibrant` server-side.
> - **Infra P1**: `RateLimitConfig.GlobalRPM` dead code, YAML config a `0600`, CSRF fail-open + localhost en AllowedOrigins, `govulncheck` en CI, Trivy pinned a SHA, cap SSE per-user.
> - **UX que el usuario mencionó pero no se atacaron**: quitar item de CW (botón X), marcar como visto desde la rail, hero del home auto-play (sigue yendo a detail), skip-intro/credits visible, next-up overlay automático al fin episode, aurora colors al detail.

---

> 🎬 **Sesión 2026-05-10 (continuación post-megablock, rama `claude/review-and-continue-Xi3KF`)** — quick-wins block del backlog: cerrados los 4 conflictos i18n del megablock + subtítulos federados end-to-end. **2 commits limpios, todo verde, pusheado**.
>
> **Commits**:
> - `921649f` *i18n(round-3): resolve 4 conflicting keys + backfill missing playback defaults*. Los 4 conflictos del megablock handoff (`liveTV.miniExpand`, `liveTV.live`, `admin.users.primaryHint`, `admin.users.addProfile`) eran "una key reusada en dos contextos con copy distinto" — la fix limpia es split per-contexto, no elegir uno y romper la otra UI. **Bug bonito cazado**: la chip flotante de `MiniPlayer` decía "Expandir reproductor" en producción aunque el dev escribió `defaultValue: "Expandir"` en el chip — i18n prefería el JSON value que lleva la versión larga porque la aria-label sibling también usa la misma key. El split arregla ambos sitios. Drive-by del extractor: 6 keys de audio language (audioLangTitle/Description, languageAuto, subtitleLangTitle/Description, subtitlesOff) shipped en PR #229 sin backfill — el extractor las pilló automáticamente.
> - `c781c11` *feat(federation): proxy embedded subtitle tracks across the peer boundary*. Cierra la deuda "Subtítulos federados pendiente (~2h)". El master.m3u8 federado es variant-only (no EXT-X-MEDIA SUBTITLES) y HubPlay sirve subs out-of-band también localmente, así que el problema NO era reescribir el master — era que el camino federado no tenía el equivalente del par `/stream/{itemId}/subtitles{,/{trackIndex}}` local. Implementado los 4 endpoints (2 origin + 2 proxy) con la misma shape JSON / WebVTT que el camino local. Frontend: `VideoPlayer` acepta nuevo prop `peerStreamSessionId`, fetch al mount, merge en el dropdown con namespace de IDs ≥ 10000 para distinguir federadas de HLS-native (0..N-1), `<track>` element para la pickada usando el mismo rAF effect que la externa.
>
> **Decisiones senior tomadas (no obvias del código)**:
> 1. **Split de keys, no elegir un copy**: `addProfile` se usaba como label de botón (`+ Perfil`) Y como título de modal (`Añadir perfil`). Elegir uno rompe la otra UI. Split en `addProfile` + `addProfileTitle` deja cada contexto con su copy correcto.
> 2. **Subs federados scoped a session UUID, no a itemID**: el endpoint origin recibe `{sessionId}` no `{itemId}`. El session UUID ya prueba acceso (StartSession aplicó share.CanPlay), así que repetir el ACL gate por itemID sería redundante. Mismo patrón que master.m3u8 + variants.
> 3. **Subs federadas riden el mismo `<track>` mechanism que las externas**: ambas son "subtitle source que no viene de hls.js". Reusar el mismo rAF + label-prefix discriminator (`Federated:` vs `External:`) evita un canal paralelo de cues.
> 4. **ID namespace `>= 10000` para subs federadas en el dropdown**: `setSubtitleTrack` de hls.js solo conoce IDs 0..N-1. El handler del player rutea por rango: id ≥ 10000 → estado local + `<track>`, id < 10000 → hls.js. Lo más limpio es añadir un range-check sin cambiar la API de PlayerControls.
> 5. **`subtitles` literal antes que `{quality}` wildcard en chi**: `subtitles/{trackIndex}` y `{quality}/{segment}` ambos tienen 2 segments — chi prefiere literal sobre param a igual depth, pero registrar primero clarifica intent y blinda futuros refactors.
> 6. **Origin-side endpoints NO van al openapi.yaml**: server-to-server, no hay SDK consumer humano. Se añaden a `outOfScopeExact` con justificación. Los proxied (`/me/peers/...`) sí — esos los consume el browser del operador.
> 7. **Fail-soft en `listFederatedSubtitles`**: si el peer está caído o devuelve error, el dropdown solo muestra HLS tracks (típicamente vacío en federación) + el operador conserva la opción de OpenSubtitles. No bloquear playback por subs.
>
> **Métricas**:
> - Backend Go: `go test -race ./...` verde (un flake conocido en `TestFSWatcher_NewSubdirGetsWatched` por timing de inotify, pasa al reintentar).
> - Frontend: `tsc -b` clean, `pnpm lint` 0 errores (2 warnings pre-existentes unchanged), **vitest 468/468**.
> - Tests backend nuevos: 4 (`TestFederationStream_Subtitles_*` — happy path, unknown session 404, foreign-peer enum guard 404, empty list 200).
>
> **Backlog que queda**:
> - **Multi-version del mismo título** (~1 sesión, P3): 4K + 1080p agrupados con quality picker.
> - **Watch-together** (~1-2 sesiones, P3): sync sessions WebSocket. Plex tiene la web rota, diferenciador real.
> - **Privacy stack** (P3): modo offline NFO, egress allowlist, CSP estricto.
> - **Tests round 4**: SystemStatus, LibraryNewPage, AuthKeysPanel, UnhealthyChannelsPanel, ChannelsWithoutEPGPanel.
> - **`MaxConcurrentStreams` por peer**: hoy compiten contra el cap global.
> - **Skip-intro Phase 3**: manual segment editor (UI). Phase 1+2 (chapter + chromaprint) ya shipped en PRs #229/#230.
> - **Schema fix `is_default`/`is_forced`**: el `SubtitleTrack` schema en openapi.yaml usa `is_default`/`is_forced` pero la respuesta JSON real usa `default`/`forced` (sin prefix). Pre-existente, no en scope de esta sesión.
>
> ---
>
> 🎬 **Sesión 2026-05-10 (Megablock — 8 commits cerrando todo el backlog operacional pendiente)** — todo lo que quedaba en la cola priorizada del Bloque C handoff (i18n round 2, tests round 2, PIN brute-force, getUserActions desktop, skip-intro Phase 1, watcher fsnotify) más una optimización de coste descubierta cuando el user preguntó por consumo de recursos. Vitest 425 → 468.
>
> **Branch**: `claude/hungry-liskov-6a853c` (worktree). Todo pusheado. PR pendiente de abrir.
>
> **Commits (en orden cronológico)**:
> - `136d5dd` *admin(desktop): UsersAdmin row actions render via getUserActions()*. Cierra el follow-up del Bloque C — la strip de botones desktop ahora consume el mismo `KebabMenuItem[]` que el kebab móvil. Orden visible cambia (Personalizar → +Perfil → PIN → Reiniciar → Eliminar) porque ese es el orden canónico en `getUserActions()`. Iconos del KebabMenuItem ignorados en desktop a propósito (filas compactas). Pickup adicional: tooltips `pinHint` y `resetPasswordHint` que faltaban en el helper, ahora también en el kebab móvil. Net −40 líneas.
> - `15c2e84` *i18n(round-2): extract 173 more defaultValue strings + ship the extractor*. Sustituye el script Python de la ronda 1 por uno mecánico Node `web/scripts/extract-i18n-defaults.mjs` checkeado en repo. Conservador: solo string-literal keys + string-literal defaultValues, no sobrescribe operadores curados, surfacea conflictos. 4 conflictos pre-existentes detectados (no resueltos: `liveTV.miniExpand`, `liveTV.live`, `admin.users.primaryHint`, `admin.users.addProfile`) — cada uno necesita decisión de copy unificado. en.json sigue placeholder ES por ahora.
> - `ca51e42` *tests(round-2): cover ChangePassword + BackupPanel + ScanProgressBanner*. +17 tests vitest (425 → 442). Drive-by fix: `admin.scan.scanning` llevaba meses como `"Escaneando "` en es.json/en.json — la regex de la ronda 1 había truncado en el apóstrofo de `"Escaneando '{{name}}'"`. Restaurado el placeholder, traducido en.json a `"Scanning '{{name}}'"`. Bilingual matchers en los tests porque jsdom defaultea a en-US.
> - `ea4443b` *auth(profiles): rate-limit PIN attempts on SwitchProfile*. Cierra el gap de brute-force en PINs de 4 dígitos (10k combos, ~17 min con bcrypt cost 10). Reusa el `loginRateLimiter` existente con namespace `pin:` (per-profile + per-IP). Empty PIN cuenta como fallo para que `pin = ""` no sea bypass. Correcta = `recordSuccess` reseta contadores. Wrong-PIN devuelve `ErrInvalidPassword` (401), throttled devuelve `ErrForbidden` (403) — distinguibles por el cliente. +4 tests Go.
> - `3da1a01` *feat(player): skip-intro / skip-credits / skip-recap (Phase 1, chapter-based)*. Vertical slice end-to-end: migración 037 con tabla `episode_segments(item_id, kind, source, start_ticks, end_ticks, confidence, detected_at)` PK `(item_id, kind, source)` para que detectores futuros coexistan; sqlc + repo `Replace()`-style scoped por source; detector chapter-based (`internal/library/segment_detector.go`) con regex (intro/opening/theme/prelude/cold open/teaser → intro, outro/credits/ending/closing/tag/stinger/coda → outro, recap/previously → recap) + position guards 50/50; subscribe a `library.scan.completed` async; `SegmentDetect{Started,Progress,Completed}` events nuevos al SSE; `attachSegments` en item detail handler colapsa al de mayor confianza por kind; frontend `SkipSegmentButton` floating bottom-right z-30 + `pickActiveSegment` puro extraído a `segmentLogic.ts` con confidence floor 0.7 + tail-trim 0.5s; suprimido durante up-next. **Phase 2 (chromaprint) lista para enchufar — el enum `source` ya acepta `'fingerprint'`, todo el resto del pipeline (storage, API, UI) reusable sin cambios.**
> - **Tooling fix bundled**: la rama tenía sqlc local en v1.28.0 hasta que detecté que el repo pin'a v1.31.1 (Makefile L14). v1.28.0 tiene dos bugs: parser roto en `INSERT … ON CONFLICT` multi-línea (puntea `excluded.detected_at` y desplaza chars al siguiente query), e infiere `int64` en lugar de `bool` para columnas EXISTS-derivadas en federation. Re-generé con v1.31.1 (`GOTOOLCHAIN=auto` por requisito Go 1.26).
> - `66bea4c` *tests(round-3): cover the IPTV admin panel trio*. +16 tests vitest (442 → 468) en ScheduledJobsPanel, EPGSourcesPanel, LivetvAdminPanel. Sub-panels mockeados a sentinel divs para testear routing/tabs sin arrastrar todos los hooks anidados. useEventStream stubbed (jsdom no implementa EventSource). **Truco aprendido**: para tabs de LivetvAdminPanel los visible labels varían por locale ("Programación" / "Schedule") y a veces el count badge se integra al accessible name y a veces no — `[aria-controls$="-schedule"]` selector es estable por kind.
> - `4df3a59` *feat(library): real-time fsnotify watcher complementing the periodic scheduler*. fsnotify v1.10.1 añadido a go.mod. `internal/library/watcher.go` con dispatcher single-goroutine que owns el handle + debouncer map + watched-roots; per-library debounce 2s (ráfaga de 10 GiB → 1 scan); recursive subscriptions (walk inicial + inline subscribe-on-CREATE para subdirs nuevos a runtime); reconcile loop 5min poll de `service.List`. **Fail-soft crítico**: en Docker on Windows con bind-mounts inotify no propaga; `NewWatcher` o el primer `Add()` falla, log warning y exit cleanly — el scheduler periódico sigue cubriendo. Reusa fingerprint cache del scanner: re-llamar `Scan(libID)` cuando un solo archivo cambió es esencialmente gratis. +4 tests usando `scanCounter` que se suscribe al event bus (más fiable que polling `IsScanning` cuando el mock scanner termina en <1ms).
> - `fb95ad6` *perf(library): make fsnotify reconcile lazy on unchanged libraries*. Cuando el user preguntó por consumo, descubrí que mi reconcile original recorría el árbol de cada biblioteca cada 5 min "por si acaso" — coste O(carpetas) por tick. Refactor: snapshot `lastSeen map[libID][]paths`, comparar contra current; solo walk cuando library nueva, eliminada o cambia paths. Coste ahora O(bibliotecas) por tick — independiente del tamaño del árbol. Test añadido `TestFSWatcher_ReconcileLazyOnUnchangedLibraries` con `walksDone atomic.Int64` test-only counter: arranque a 5 ticks rápidos = walksDone debe quedar en 1.
>
> **Métricas finales**:
> - Backend Go: `go build ./...` clean, `go vet ./...` clean, suite completa verde en cada paquete.
> - Frontend: `tsc -b` clean, `pnpm lint` 0 errores (2 warnings pre-existentes unchanged), **vitest 468/468** (era 425).
>
> **Decisiones senior tomadas (no obvias del código)**:
> 1. **Skip-intro Phase 1 = chapter-based first, no fingerprinting yet**. La infra (schema, scheduler, eventos, API, UI) es idéntica para ambos algoritmos — fingerprinting será solo cambiar el detector. Phase 1 cubre rips de Blu-ray / ediciones profesionales (subset significativo) con cero deps externas nuevas. Senior approach: vertical slice end-to-end primero, profundidad después. El enum `source` reserva `'fingerprint'` y `'manual'` para que Phase 2/3 no requieran migración.
> 2. **PIN throttle reusa loginRateLimiter, no nuevo módulo**. Es el mismo patrón conceptual (límite de attempts por (subject, IP), reset on success); duplicar daría mantenimiento doble. Per-profile key + per-IP key cubren ambos vectores. ErrForbidden (403) para distinguir throttle de wrong PIN (401) sin enumerar — un atacante no puede saber si está locked o solo equivocado.
> 3. **fsnotify watcher fail-soft, no retry**. Una vez que el host filesystem no soporta inotify (Docker on Windows con bind-mounts es el caso real del user), retry cada minuto es ruido. Log warning, scheduler periódico cubre, el operador puede deployar en una máquina Linux real cuando quiera.
> 4. **Reconcile lazy, walks solo cuando cambia algo**. Las nuevas subcarpetas a runtime las pilla el inline CREATE handler — el reconcile NO las necesita. Solo justifica caminar el árbol cuando una biblioteca aparece, desaparece, o cambia `Paths`.
> 5. **`scanCounter` test helper en watcher_test.go**: poll `IsScanning` race-condicionaba con el mock scanner que termina en <1ms. Subscribirse al evento `LibraryScanCompleted` es la señal correcta — agnóstico al timing.
>
> **Backlog que queda al cerrar la sesión**:
> - **Skip-intro Phase 2** (~2-3 sesiones): chromaprint/`fpcalc` añadido a Dockerfile, fingerprint extraction wrapper en Go, cross-episode matching con sliding window + Hamming distance, scheduler integration. **Toda la infra ya enchufada**, es solo añadir el nuevo detector.
> - **4 conflictos i18n** (~30 min): `liveTV.miniExpand`, `liveTV.live`, `admin.users.primaryHint`, `admin.users.addProfile`. Cada uno necesita decisión de copy unificado.
> - **Multi-version del mismo título** (~1 sesión, P3): 4K + 1080p agrupados como una entrada con picker de calidad. Schema lo soporta (`fingerprint` + `parent_id`); falta scanner agrupar y UI mostrar.
> - **Watch-together** (~1-2 sesiones, P3): sync sessions WebSocket. Plex tiene la web rota, diferenciador real.
> - **Privacy stack** (P3): modo offline NFO, egress allowlist, CSP estricto.
> - **Tests round 4**: SystemStatus, LibraryNewPage, AuthKeysPanel, UnhealthyChannelsPanel, ChannelsWithoutEPGPanel.
>
> ---
>
> 🎬 **Sesión 2026-05-10 (Bloque C — mobile responsive admin)** — **UsersAdmin renderiza como cards apiladas con kebab menu en <768px**. La única superficie de admin que se rompía de verdad en móvil ya no lo hace.
>
> **Commit**: `8f14cc4` *admin(mobile): UsersAdmin renders as stacked cards + kebab menu under <md*.
>
> **Decisiones tomadas en sesión**:
> 1. **Kebab menu (Opción B), no horizontal scroll strip**: el operador del móvil prefiere 2 clicks (kebab → opción) a deslizar para encontrar el botón. Implementado como nuevo componente `web/src/components/common/KebabMenu.tsx` — trigger + dropdown sin Radix dep, click-outside + Escape cierran, items con flags `danger`/`disabled`/`hidden`/`hint`.
> 2. **Single source of truth para acciones**: helper `getUserActions(user)` dentro de UsersAdmin retorna un `KebabMenuItem[]` que el kebab consume directo. La desktop sigue rendizando botones inline pero podría leer del mismo array en una pasada futura para deduplicar.
> 3. **Card de mobile reusa el state machine de collapse/expand**: la lógica `expandedParents` + chevron + member-count pill se replica en el card; los children renderan debajo con el mismo accent rail izquierdo cuando expanded.
> 4. **`useIsMobile` lifted a hook compartido** (`web/src/hooks/useIsMobile.ts`) en vez de quedarse inline en AppLayout. Mismo comportamiento (768px breakpoint, `useSyncExternalStore` sin effect).
> 5. **LibraryCard NO se tocó**: ya usaba `flex-col sm:flex-row` con `flex-wrap` en las acciones; mobile-friendly sin esfuerzo. Validado por inspección.
>
> **Tests añadidos** (+5, 420 → 425):
> - mobile path renderiza cards (no `<table>`)
> - chevron expande el kid row
> - kebab abre con items correctos para un user regular
> - profiles ocultan `+ Perfil` y `Reiniciar contraseña` del kebab
> - click en `Personalizar` abre el rename modal
>
> **Verificación**: backend `go test ./...` verde · vitest 425/425 · tsc -b clean · vite build clean · pnpm lint 0 errores.
>
> **Pendientes en backlog (cola priorizada)**:
> - **i18n migration round 2** (~2-3h): los ~30 archivos restantes con ~440 defaultValues. Mismo script Python del commit `a00bd9d` (la primera pasada solo cubrió 20 de los 50 archivos).
> - **Tests round 2** (~3-4h): ChangePassword form validation, BackupPanel restore validation, ScanProgressBanner mount, BecauseYouWatchedRail empty state.
> - **Skip intro/outro detection** (~5-8h, feature gorda): chromaprint o Plex's intro-marker tool.
> - **PIN brute-force lockout** (~1h): bcrypt-cost da rate-limit natural pero sin lockout explícito por user.
> - **getUserActions usado también por desktop** (~30 min): la helper está pero el desktop sigue inlineando los botones; reescribir el `<td>` de actions para mapear `getUserActions(user).filter(it => !it.hidden && !it.disabled)` y rendir buttons. Pequeña limpieza.
>
> ---
>
> 🎯 **Próxima sesión planeada — Bloque C: mobile responsive admin** (~3-4h estimado).
>
> **El problema concreto**:
> - `/admin/users` (`web/src/pages/admin/UsersAdmin.tsx`) es una `<table>` de **8 columnas** (username, displayName, role, edad máxima, acceso, estado, created, actions) envuelta en `overflow-x-auto`. En móvil (<768px) se convierte en un scroll horizontal: el operador tiene que arrastrar a la derecha para ver "actions" y volver a la izquierda para identificar la fila. Peor aún, la columna de acciones tiene 5 botones (`+ Perfil`, `Personalizar`, `Cambiar PIN`, `Reiniciar contraseña`, `Eliminar`) que envuelven a 2-3 líneas, dejando cada fila a ~200px de alto.
> - `/admin/libraries` (`LibrariesAdmin.tsx`) — ya está agrupado por content_type pero las filas siguen siendo card-like sin tabla; se reflujan razonablemente. Verificar.
> - `/admin/system` — editorial sections, ya OK en móvil (Health rows, Connection grid `sm:grid-cols-2`, Storage bars).
> - `/admin` (Resumen) — paneles ya stackean (`lg:grid-cols-2`).
>
> **El plan**:
> 1. Lift `useIsMobile` (vive inline en `AppLayout.tsx:19`, breakpoint 768) a `web/src/hooks/useIsMobile.ts` para reuso.
> 2. En `UsersAdmin.tsx`: en `isMobile` swap el `<table>` a una pila de `<div>` cards. Cada card: avatar + nombre arriba; rol / cap / estado / created como `<dl>` etiquetada; acciones en un row horizontal scrolleable o detrás de un kebab (`MoreVertical` icon → menú).
> 3. Validar `LibrariesAdmin.tsx` en mobile real (probable que solo necesite ajustes de padding).
> 4. Tests: re-renderizar UsersAdmin con `useIsMobile = true` y verificar que las acciones siguen disparando los mismos handlers.
>
> **Patrón de diseño**: matchear lo que ya hace TopBar / LiveTV en móvil — cards apiladas con divider, sin tabla. Conservar la lógica de collapse/expand de profile members (chevron sigue al lado del nombre, no en una columna aparte).
>
> **Sub-items que también caben aquí**:
> - i18n migration round 2: ~30 archivos restantes, ~440 defaultValues. Mismo script Python del commit `a00bd9d`. ~2-3h adicionales.
> - Tests round 2: ChangePassword form validation, UsersAdmin rename modal, BackupPanel restore validation. ~3-4h.
>
> **Decisiones a tomar al arrancar**:
> - ¿Las acciones de cada row caben en un row horizontal scrolleable, o mejor un kebab menu? El kebab es más limpio pero añade un click extra; el scroll mantiene parity con desktop.
> - ¿Siempre stackear en mobile, o gating por feature flag? Recomiendo siempre.
>
> ---
>
> 🎬 **Sesión 2026-05-10 (Bloque B — calidad de código, post-PR-220 re-sync)** — **Tres ataques al fondo de armario que no daban features pero sí dejan la base sustancialmente más sana**: lint clean (13 errores → 0), tests frontend admin/auth (de 0 a 22 tests cubriendo Login/DevicesPanel/WhoIsWatching/LogsPanel), i18n migration (161 keys extraídas a es.json/en.json).
>
> **Commits**:
> - `25a98dc` *lint: clear all 13 pre-existing errors*. Tres bugs reales (Date.now en render de HeroSpotlight, ref-during-render en UpNextOverlay y useHls), seis sync-prop-to-state con eslint-disable + comment justificando por qué `key={prop}` sería peor (VideoPlayer, useHls, SystemSettingsSection, MediaBrowseFilters, LibraryEditModal, useMetricsHistory en SystemStatus), tres react-refresh/only-export-components con file-level disable (PageHeader, MediaBrowseFilters, WatchTonightTile), ItemDetail useMemo dep mismatch arreglado pulling local. 3 unused eslint-disable directives borrados. Quedan 2 warnings pre-existentes no fixables: EPGGrid (TanStack Virtual incompat) + PeerLibraryItemsPage (exhaustive-deps suggestion).
> - `1efe6fd` *tests: cover Login error mapping, DevicesPanel revoke flow, WhoIsWatching bounce, LogsPanel pause/drain*. Cuatro test files nuevos, +22 tests (398 → 420). Login: la matriz de loginErrorMessage que mapea wire codes (ACCESS_EXPIRED / ACCOUNT_DISABLED / INVALID_CREDENTIALS / RATE_LIMITED) a copy ES + redirect logic post-success (change-password vs select-profile vs solo). DevicesPanel: pill "Este dispositivo" en current=true, confirm prompt antes de revocar la propia, empty-state copy. WhoIsWatching: bounce-when-solo, error fallback explícito, multi-profile rendering, presencia del botón Volver. LogsPanel: state machine pause/drain (entries buffereadas mientras paused → drained on unpause), filter ERROR-only, EventSource onerror → "Reconectando" pill. EventSource mockeado via test class.
> - `a00bd9d` *i18n: extract 161 defaultValue strings into es.json + en.json placeholders*. Script Python regex sobre los 20 archivos admin/auth de mayor tráfico extrayó cada `t("key", { defaultValue: "..." })` y las pobló en es.json bajo el dotted path correcto. Keys ya curadas (20) NO se sobreescribieron. en.json recibió placeholders ES (es-default codebase, mejor mostrar texto en español que warning de missing-key). Los `defaultValue` siguen en código como belt-and-braces. Otros ~30 archivos con ~440 defaultValues quedan para follow-up.
>
> **Decisiones senior tomadas**:
> 1. **eslint-disable con comentario explicativo > refactor agresivo**: para los 6 sync-prop-to-state, la "fix correcta" del lint rule es `key={prop}` para forzar remount. Pero esa estrategia destruiría hls.js, animaciones de Sheet, focus mid-typing en filtros. Trade-off documentado inline en cada disable.
> 2. **No file-split por react-refresh/only-export-components**: HMR-only concern. Splitear PageHeader/MediaBrowseFilters/WatchTonightTile solo para Fast Refresh fragmentaría la API por nada. File-level disable + comment.
> 3. **Tests focused en regresión, no coverage por coverage**: las 4 superficies cubiertas (Login error map, DevicesPanel revoke, WhoIsWatching bounce, LogsPanel state machine) son las que han dado bugs reales o son state-machines complejas. ChangePassword / UsersAdmin / BackupPanel quedan para otra sesión — son CRUD más sencillos.
> 4. **i18n: en.json placeholder = ES value**: alternativa era dejar las keys faltantes y que cayera al defaultValue (status quo). Pero entonces un translator no puede ver qué keys faltan en EN. Placeholder ES + nota en commit doc dice "translator: replace these" sin requerir ningún tooling.
>
> **Quedan en backlog**:
> - **Bloque C — Mobile + features grandes** (~5-7h): mobile responsive admin tables (UsersAdmin/LibrariesAdmin overflow horizontal en móvil); skip intro/outro detection (chromaprint, feature gorda).
> - **i18n migration round 2** (~2-3h): los ~30 archivos restantes con ~440 defaultValues. Mecánico, mismo script.
> - **Tests frontend round 2** (~3-4h): ChangePassword form validation, UsersAdmin rename/delete confirms, BackupPanel restore validation, ScanProgressBanner mount, BecauseYouWatchedRail empty state.
> - **Singletons sueltos**: PIN brute-force lockout (~1h, bcrypt-cost da rate-limit natural pero sin lockout explícito); 2FA (descartado por user en sesión previa); empty states de LiveTV / setup wizard (intencionalmente no migrados).

> 🎬 **Sesión 2026-05-10 (continuación post-merge, rama `claude/review-project-updates-GN0JB` re-sincronizada con main)** — **Bloque A operacional: 4 items del backlog ejecutados de un tirón**. Todos pequeños individualmente; el conjunto cierra los huecos restantes que dejé documentados en el commit anterior de esta entrada.
>
> **Commits**:
> - `712787b` *auth: device-code path checks AccessExpiresAt + setup wizard auto-generates password*. Cierra el último flanco de access-expired (DeviceCodeService.ApproveDevice + PollDevice no chequeaban) usando el mismo `ErrAccessExpired` que login/refresh/validate. Setup wizard estrena flujo opt-in de auto-pwd: AccountStep gana checkbox "Generar contraseña automáticamente" (default OFF), backend devuelve `generated_password` una vez en el response, CompleteStep lo surface con copy-to-clipboard. Sin forced-rotation porque el operador IS el admin nuevo y vería el password en el mismo flow.
> - `c9b691a` *docs(openapi): migrate user-facing /users mutations + because-you-watched into the spec*. 5 paths salieron del allowlist al yaml propiamente: PUT /users/{id}/{display-name,avatar-color,pin,content-rating} (admin OR parent OR self) + GET /me/home/because-you-watched. Schemas con validación (length caps, palette restriction, regex en pin) + 403 doc del auth matrix explicit. Las ~98 entradas restantes del allowlist son operator-only (admin auth keys, federation pairing, IPTV admin, channel health) — sin SDK consumer, no migran.
> - `3bcf510` *settings(providers): fanart link in the Provider catalogue + es copy on the API-key hint*. Auditoría descubrió que provider keys editables desde Settings YA estaba implementado (page tiene ProviderSettings con useProviders + useUpdateProvider, backend tiene PUT /providers/{name}). Solo dos completeness fixes: añadir URL de Fanart al providerMeta y traducir "Get your API key at" al ES via i18n. El claim original "hoy solo yaml" del backlog era erróneo.
> - `7e18a61` *common(empty-state): bordered + compact variants + migrate the obvious ad-hoc usages*. EmptyState era single-purpose (py-16 full-page) → ad-hoc divs en PeersPage (×2) + PeerLibrariesPage que reproducían a mano `rounded-lg border-dashed bg-elevated`. Nuevas props `bordered` + `compact` capturan el patrón. Migrados los 3 call-sites obvios. NO migrado: Live-TV (usa `tv-*` design language), SetupWizard library drop zone (es CTA, no empty), SystemStatus inline session strip (más tight que compact).
>
> **Decisiones senior tomadas**:
> 1. **Setup wizard auto-pwd default OFF**: el bootstrap admin es el operador del setup, lo más probable es que tenga su password elegida. Surprise-with-random-password sería hostil. Toggle opt-in.
> 2. **Setup wizard forced-rotation NO se aplica al auto-pwd**: el operador acaba de ver el password en el wizard, mandarlo a /change-password en el primer login sería circular. Trade-off vs el flow normal de /admin/users (donde sí se fuerza rotation porque el admin crea cuentas para OTROS).
> 3. **OpenAPI cleanup selectivo**: 103 paths en allowlist → 98. Solo migré los 5 user-facing genuinos. Operator-only stays en allowlist (admin keys, federation, IPTV admin, etc) — no SDK consumer, sin valor en doc-as-public-spec.
> 4. **EmptyState `bordered` + `compact` props, no `variant="card"|"page"`**: dos booleans dan los 4 quadrants reales (bare/page, bare/inline, card/page, card/inline) sin acoplar layout a variant name. Misma elección de Vercel/shadcn para componentes con multiple display modes.
>
> **Quedan en backlog (orden propuesto para próximas sesiones)**:
> - **Bloque B — Calidad de código** (~6-8h): tests frontend admin/auth (cero cobertura hoy), lint cleanup pre-existente (13 errors en archivos no tocados por este branch), i18n migration (~600 `defaultValue` → es.json/en.json).
> - **Bloque C — Mobile + features grandes** (~5-7h): mobile responsive admin tables + Skip intro/outro detection.
> - **Singletons sueltos**: PIN brute-force lockout (~1h, bcrypt-cost da rate-limit natural pero no lockout explícito); 2FA (descartado por user en sesión previa).

> 🎬 **Sesión 2026-05-10 (rama `claude/review-project-updates-GN0JB`, **46 commits acumulados**, todo empujado, listo para PR)** — **Sesión maratón cerrando todas las ráfagas de "qué falta": Tanda 1 (security/ops), Tanda 2 (UX features), rediseño cinematográfico de Login + WhoIsWatching, fixes de regresiones del propio session, y 4 features grandes de cierre (cap filter coverage, avatar custom, "Porque viste X", lint clean)**. Continuación natural de la sesión 2026-05-09.
>
> **Cambios totales en la rama**: 112 ficheros, +12881 / −1516 líneas. Todos los tests verdes (backend + 398 vitest), tsc + build limpios.
>
> **Bloque 1 — Tanda 1 (security/ops)**:
> - `e6b2dc7` *auth: split ACCESS_EXPIRED from ACCOUNT_DISABLED + friendly login errors*. Backend ahora distingue "tu acceso temporal caducó" vs "tu cuenta está desactivada"; frontend traduce ambos códigos + INVALID_CREDENTIALS + RATE_LIMITED a copy ES amigable en lugar del raw del servidor.
> - `65ce819` *auth+ops: refresh checks AccessExpiresAt + /health/ready gates on ffmpeg + disk*. Cierra el flanco que dejaba a un usuario con acceso caducado seguir extendiéndose vía /auth/refresh. /health/ready ahora también valida ffmpeg-en-PATH + espacio libre > 1 GiB en el volumen de la BD (`syscall.Statfs.Bavail`); apto para `docker healthcheck` real, no solo k8s liveness.
> - `0a05eb0` *auth: "Tus dispositivos" — list + revoke active auth sessions per user*. Plex/Jellyfin staple. Backend: Service.ListSessions / RevokeSession / CurrentSessionID + GET /me/sessions + DELETE /me/sessions/{id} (foreign IDs → 404 anti-enumeration; revocar la propia limpia cookies server-side). Frontend: DevicesPanel en Settings con icono Laptop/Smartphone, IP, last-active relativo, "Este dispositivo" pill en la sesión actual. Confirm prompt al revocar la propia.
> - `4795a62` *admin: backup / restore the SQLite DB from the admin pane*. **Self-hosted operator must-have**. Download: GET /admin/system/backup ejecuta `VACUUM INTO` (canonical SQLite live-backup) y stream el `.db` con Content-Disposition. Restore: POST multipart, valida los 16 bytes magic header SQLite, stage en `<dbdir>/.pending-restore.db`. El swap real ocurre en el siguiente boot — `db.ApplyPendingRestoreIfAny()` en main.go antes de sql.Open mueve la live DB (+ WAL/SHM) a `hubplay.db.bak-<stamp>` y rename atómico. Hacer el swap en vivo dejaría SQLite en estado indefinido (open WAL + reader pool + fresh file underneath). UI: BackupPanel en /admin/system → Avanzado.
> - `0e42911` *fix(profiles): SQL truncation in ListProfilesForOwner + skip picker on solo accounts*. **Bug bonito**: sqlc 1.31.x trunca `COLLATE NOCASE;` a `COLLATE NOCA` cuando va pegado al `;` final. Las otras queries del proyecto que usan COLLATE llevan ASC/DESC después y por eso renderizan bien. Workaround: añadir explícito `ASC` (cosmético para SQLite, load-bearing para sqlc) + comentario en el .sql para que nadie lo "limpie". Plus: Login revertido a `data.profiles?.length > 1` antes de redirigir a /select-profile (cuentas solas van directas a /).
> - `6f4aa70` *ci: tolerate Trivy skip on failed builds + retry go mod download*. Trivy ahora gated en `success()` y upload con `hashFiles('trivy-results.sarif') != ''` para no fallar dos veces cuando el build cae. CodeQL action v3 → v4 (warning de deprecación dic 2026). Dockerfile: `GOPROXY=https://proxy.golang.org,https://goproxy.cn,direct` + retry 3x con backoff lineal porque arm64 vía QEMU sufre con módulos pesados (modernc.org/libc ~50 MB).
>
> **Bloque 2 — Tanda 2 (UX features)**:
> - `01e886d` *player: PiP shortcut + extended keyboard shortcuts + help overlay*. Atajos YouTube-style: K (play), J/L (-10s/+10s), 0..9 (saltar al N×10% de duración), P (Picture-in-Picture), ? (help overlay). El `<video>` perdió `disablePictureInPicture` (estaba bloqueando nuestro propio JS-initiated PiP). KeyboardHelpOverlay nuevo con Esc/backdrop dismiss.
> - `2e8be64` *users: rename profile / user display name (admin OR parent OR self)*. PUT /users/{id}/display-name con la misma matriz de SetPIN. UI: botón "Renombrar" en /admin/users con modal. Username + parent_user_id quedan intocables → la color del avatar (FNV(username)) no cambia.
> - `20639be` *admin: live log viewer — in-memory ring + SSE + tail panel*. **slog.Handler que envuelve el de stdout**: cada record va a stdout AND a un ring de 500 entries + fanout a subscribers. Drop-on-slow para no bloquear el logger path en un reader laggy (igual que dmesg). HTTP: GET /admin/system/logs (snapshot) + /logs/stream (SSE con replay del ring + heartbeat 20s). Frontend LogsPanel con pause/clear/level filter (All/Warn+Error/Error), buffer de pause + drain on unpause, EventSource con reconnect automático.
> - `c9dc24f` *admin: live scan progress banner — emit + subscribe over existing SSE*. Nueva `event.LibraryScanProgress` emitida cada 50 archivos por el scanner (cantidad balanceada: hot SSD no flooda, slow disk se siente vivo). Reusa el SSE existente /events. Frontend `useScanProgress` Map<libraryId, ScanProgress> + `ScanProgressBanner` arriba de /admin/libraries con spinner + count + path actual.
>
> **Bloque 3 — Rediseño cinematográfico de auth**:
> - `fa4186a` *auth(profile-picker): polish "Who is watching?" — aurora + 4-box PIN + auto-submit + bottom rail*. Aurora backdrop matching Login + framer-motion entrance staggered + PIN como 4 cajas-dot Netflix-style con auto-submit al 4º dígito + shake on wrong-PIN. Bottom rail con "Cerrar sesión" y "Gestionar perfiles" (admin-only).
> - `7870d5e` *admin: shared SectionHeader, polish across Resumen/Federación/Users/Settings*. SectionHeader extracted a `components/admin/SectionHeader.tsx`. Federación con `lg:sticky lg:top-4` para que la huella se quede en pantalla durante el handshake. Users: rail accent border en padre+hijos cuando expanded. Settings refactor a lista vertical (estilo macOS Settings, ya no grid 2-col que orphanaba la última fila), bloqueo de Save con valor vacío, dropdown único renderizado como readonly text + caption "(única opción detectada)".
> - `036194c` *auth(profile-picker): cinematic upgrade — backdrop mosaic + halo glow + ambient tint + premium type*. 4-layer canvas: aurora + backdrop blur-mosaic (12 items recientes a 7% opacity) + hover ambient tint en el color del avatar + viñeta. Cards 160→180px con gradient "lit-from-above". Halo radial al hover/focus en el color de paleta. Type extralight + tracking apretado.
> - `58c45e2` *auth(profile-picker): split layout — round avatars + visible poster wall + larger logo*. Avatares redondos (rounded-full matching TopBar). Logo 28→56px arriba del viewport. Cuando hay ≥4 pósters: layout split izquierda (picker) / derecha (poster wall 3x2 con tilts alternos hand-arranged feel). Sin contenido: layout centrado de fallback.
> - `4197506` *auth(login + picker): back button + red sign-out + cinematic Login canvas*. Picker: pill "Volver" arriba a la izquierda (siempre navega a "/" no `back()` para no atrapar al user); "Cerrar sesión" en rojo sutil (red-500/20 border + red-400/85 text). Login: GhostPosters drift detrás del card (6 rectángulos rounded con paleta avatar, framer-motion infinito drift), big logo arriba + hero "Bienvenido". md+ only para no crowd móvil.
> - `2b0e7d7` *auth: vertical profile list in split layout + revert Login logo placement*. ProfileCard gana `compact` prop. En split layout: avatares como filas de lista (h-16 round + nombre + hint), hover slide x +4px. En centred: hero tiles. Login revertido a logo dentro del card sin frases (per user request).
>
> **Bloque 4 — Cierre de gaps + features grandes**:
> - `338b217` *fix: lint warnings I introduced — refs during render, setState in effect, hook order*. LogsPanel: pausedRef.current = paused movido a useEffect; bufferRef.current.length leído via state paralelo `bufferedCount`; un-pause drain pasado de useEffect a click handler (`onTogglePause`). WhoIsWatching: `hoveredHex = useMemo()` hoisted antes de los early returns (era rules-of-hooks violation real).
> - `21eae3e` *auth(rating): apply per-profile cap on /items/search + /me/home/{trending,recommended}*. Cierra el agujero que dejaba a un perfil kid con cap PG-13 escribir "fight club" en el SearchBar y ver el resultado R. HomeTrendingItem y HomeRecommendation ganan ContentRating field via `COALESCE(i.content_rating, '')`. Filtro post-fetch usando `library.AllowedRating`. Inner LIMIT bumps a 2x cuando cap activo (headroom para no dejar rail vacío). HomeHandler gana `users UserService` dep.
> - `fb4395d` *auth: per-profile avatar colour override + Personalizar modal*. **Migración 036**: `avatar_color TEXT` en users. Backend: SetAvatarColor service valida contra paleta de 14 hex, PUT /users/{id}/avatar-color con matriz admin/parent/self. Frontend: `avatarColorForUser(user)` prefiere override, fallback al deterministic FNV. Hex desconocido fall-through (DB legacy / hand-edit no rompe). Renombrar modal pasa a "Personalizar perfil" con grid 7-col de swatches + tile "A" que clear el override. Submit dispara display_name + avatar_color en paralelo cuando ambos dirty.
> - `594adc6` *home: "Porque viste X" recommendation rail — endpoint + hook + Home rail*. **El Home cierra el discovery loop**. Backend: HomeRepository.BecauseYouWatched(userID, limit) con 3 SQL passes — (1) latest completed user_data row, fold a parent series si es episode; (2) seed metadata + genres; (3) score unwatched movies/series por overlap de género. GET /me/home/because-you-watched devuelve `{seed, items}` con shape compatible con Recommended (`recommended_because: { genres }`). Cap honoured con headroom 2x. Frontend: BecauseYouWatchedRail mounted en Home entre los layout sections y los federated rails (mismo patrón "out-of-layout-for-v1"). Self-hides en cold-start (seed: null) o cuando seed sin genres.
>
> **Decisiones senior tomadas esta sesión** (anotadas para futuras):
> 1. **Backup/restore swap-on-restart, no live**: hacer el swap mientras SQLite tiene WAL abierto + reader pool dejaría la DB en estado indefinido. Stage + reinicio + rename atómico es la única manera segura. UI dice "se aplicará al reiniciar".
> 2. **slog.Buffer fanout drop-on-slow**: NUNCA bloquear el logger path en un subscriber laggy. Misma regla que dmesg. Slow consumers pierden entries past su channel buffer (16) en vez de stallar todo.
> 3. **Scan progress beat cada 50 archivos**: hot SSD harían cientos por segundo y floodarían el bus; slow disk con ~5fps sigue sintiéndose vivo. Sweet spot validado.
> 4. **Cap filter con headroom 2x**: cuando hay cap activo, fetch 2x el limit + filter post-fetch. Alternativa (push cap al SQL como `IN (?, ?, ...)` variadic) es más bajo overhead pero feo de mantener; 2x pad es chea sobre todo en queries que ya tienen LIMIT 12.
> 5. **Avatar override fall-through silencioso para hex desconocido**: una DB row con hex que ya no está en la paleta (por update de palette o hand-edit) cae al determinism helper, no se renderiza. Razón: sino el user no podría "un-pick" el color desde la UI list.
> 6. **"Porque viste X" siempre fold a series**: un episodio "completed" lights up the whole series como seed. El header reads "Porque viste Breaking Bad", no "Porque viste S05E14". Mismo rollup que Trending para consistencia.
> 7. **Always navigate to "/" en el "Volver" del picker** (no `navigate(-1)`): history-back desde un fresh login lands on /login again, lo que loops al user out. "/" es la respuesta correcta en ambos flujos.
>
> **Quedan pendientes (cola priorizada para sesiones futuras)**:
> - **Skip intro/outro** detection (chromaprint o Plex's intro-marker tool) — feature gorda, ~5-8h.
> - **Setup wizard auto-generated password** para el primer admin (~1h).
> - **OpenAPI yaml** para los ~10 endpoints en outOfScopeExact (~30 min).
> - **i18n cleanup**: ~600 `defaultValue` en código vs 286 keys reales en es.json/en.json. Migración mecánica.
> - **Mobile responsive**: tablas /admin/users + /admin/libraries no se reflujan en móvil.
> - **Empty states consistency**: mezcla de `<EmptyState>` + `<p className="text-muted">…</p>`.
> - **Provider keys** (TMDb / Fanart / OpenSubtitles) editables desde Settings (hoy solo yaml).
> - **PIN brute-force**: bcrypt-cost 10 da rate-limit natural pero no hay lockout explícito por user.
> - **Refresh-token check en device-code path** (`auth_device.go`): solo cubrí Login + Refresh regulares.
> - **2FA** (descartado por user esta sesión).
> - **0 tests frontend para `pages/admin/*`, Login, WhoIsWatching, ChangePassword**.
> - **13 errores lint pre-existentes** en archivos no tocados (EPGGrid refs-during-render, MediaBrowseFilters setState-in-effect, VideoPlayer:446, etc).
>
> **Esta sesión NO ha tocado**: federation streaming, IPTV transmux/scheduler, scanner walker (solo añadí emit de progress), provider tmdb/opensubtitles. Todo el trabajo en `internal/auth/`, `internal/db/`, `internal/api/`, `internal/library/`, `internal/logging/`, `internal/event/`, `cmd/hubplay/main.go`, `web/src/pages/{admin,Login,WhoIsWatching,Home,Settings}.tsx`, `web/src/components/{admin,player,settings,home,layout}/`.

> 🎬 **Sesión 2026-05-09 (rama `claude/review-project-updates-GN0JB`, todos los commits ya pusheados, NO mergeados a main aún)** — **Sesión muy larga: refactor masivo de admin (3 commits) + sistema completo de perfiles Netflix-style + reset password + content rating cap + rediseño Hero + Resumen vivo + branding/avatar/series-rail/series-recientes**. La rama acumula **18 commits** desde el branch point.
>
> **Bloque 1 — Hero + Resumen + Branding (commits 31792b6 → 064aed5 → 662ffc5 → 004c29e)**
> - `31792b6` *home(hero): season-aware episode slides + curated tier rotation*. Cuando un episodio aparece en el Hero del Home, ahora muestra **póster de la temporada** a la izquierda + **backdrop de la temporada** detrás (en vez del still del episodio que se cortaba a tamaño hero). Backend `/me/continue-watching` enriquece episode rows con `season_poster_url`, `season_backdrop_url`, `series_*` (vía batch image fetch). Frontend HeroBanner refactorizado a tiers con dedupe por series_id. Deep-link directo al episodio (no al detail genérico de la serie).
> - `064aed5` *ui(shell): bump brand mark, anchor hero crop higher, deterministic avatar colours*. TopBar logo 26→32px (~+23%); Hero backdrop con `object-position: 50% 28%` para no cortar cabezas (Transformers ensemble caso real); avatar default ahora **color sólido determinista** (paleta de 14 tonos) por hash FNV-1a del username — cero migración, cero DB, mismo color en todos los dispositivos. 4 tests vitest pinean.
> - `662ffc5` *home(rail): filter latest-in-shows by series; topbar: more breathing room*. "Reciente en series" mostraba solo Wandavision aunque había más; causa: `/items/latest` ordena por `added_at DESC` y librerías TV están dominadas por episode rows. Frontend pasa `?type=series` para shows libraries → SQL filtra antes. TopBar `px-3 md:px-4` → `px-4 md:px-8` para que el logo no esté pegado al borde.
> - `004c29e` *home(rail): order latest-shows by episode activity + surface "+N nuevos" badge*. Mejora del anterior: las series con episodios recientes suben al top del rail (orden por `MAX(added_at)` sobre serie+episodios), y se renderiza un pill verde "+3 NUEVOS" en el póster cuando hay episodios añadidos en últimos 14 días. Backend `LatestSeriesByActivity` con CTE; frontend PosterCard render condicional. 1 test backend con 3 casos.
>
> **Bloque 2 — Admin redesign en 3 fases (commits c0b3287 → bcb5988 → d1a7914)**
> - `c0b3287` *admin(ia): collapse 6 tabs into 4 — Resumen / Biblioteca / Usuarios / Sistema*. **IA brutal del admin**: 6 top-level tabs → 4. Providers folded inside Library page; Federation folded inside Users page; System sub-tabs (Status/Activity/Advanced) fusionadas en una sola página tipo macOS Settings; Activity (3 EmptyStates "coming soon") deleted. SystemLayout/Activity/Advanced.tsx borrados como dead code. Routes legacy redirigen.
> - `bcb5988` *admin(metrics): /admin/system/stream-activity + /admin/system/top-items*. Backend nuevo: 2 endpoints admin para el Resumen rediseñado. Stream-activity zero-pads días vacíos (sparkline necesita serie contigua). Top-items rollup episodios→series igual que Trending pero admin-scope (sin library_access guard). **Gotcha cazado y documentado**: `modernc.org/sqlite` formatea `time.Time` con sufijo " +0000 UTC" que `strftime` no parsea — uso `SUBSTR(.., 1, 10)`.
> - `d1a7914` *admin(summary): redesign Dashboard as editorial Resumen with sparkline + leaderboard*. **Lo que cura el "grita IA"**. Strip de salud una línea sin chrome ("Servidor sano · v1.0.3 · 4d 12h sin reiniciar"); panel "Esta semana" con sparkline SVG custom (~80 LOC, cero deps) + leaderboard top-5; "Catálogo" en prosa una sola frase. Patrón visual asimétrico vs el bento anterior.
>
> **Bloque 3 — Auth grande: profiles Netflix-style + reset password + content rating (commits 46e3b91 → 8d155be → 92097b8 → 9e9f048)**
> - `46e3b91` *users(schema): migration 034 — profile tree + must-change-password + PIN + content cap*. **Migración 034**: `users` table gana 4 columnas: `parent_user_id` (FK a users.id, CASCADE), `pin_hash`, `max_content_rating`, `password_change_required`. Diseño elegante: un perfil ES un row en users con parent_user_id set; cero cambios necesarios en `user_data`/`user_preferences`/`favorites`/`federation_progress` (todos siguen keyed por users.id). Repo + sqlc regenerado.
> - `8d155be` *auth(passwords): admin reset, auto-generated temp passwords, forced first-login rotation*. **Reset password feature completo**. Backend: `auth.GeneratePassword()` (12 chars de alfabeto legible 56-char, ~70 bits), `ResetPassword(userID)` invalida sesiones + setea must-change=true, `ChangePassword(current, next)` con bypass de current-check cuando must-change está activo. HTTP: `POST /me/password` (self), `POST /users/{id}/reset-password` (admin), POST /users con password vacío auto-genera y devuelve en `generated_password` una sola vez. Frontend: ChangePassword screen forzada (gated en ProtectedRoute), modal con password en clipboard tras crear/reset, checkbox "auto-generar" por defecto en el form de admin. `auth.refreshMe()` hace flip inmediato del flag.
> - `92097b8` *auth(profiles): Netflix-style profile tree + Who's-Watching picker + per-profile PIN*. **Profiles end-to-end**. Backend: `ListProfiles(userID)` resuelve owner-id, `SwitchProfile(currentID, targetID, pin)` verifica mismo owner + bcrypt-PIN, `SetPIN(userID, pin)` (4-digit). Login response carry `profiles[]`. POST /auth/switch-profile, GET /me/profiles, PUT /users/{id}/pin. Profile creation via Register con `parent_user_id` set (synthesized username `<parent>/<base>`). Frontend: `/select-profile` WhoIsWatching screen con avatar cards (deterministic colour helper), 4-digit PIN pad, candado overlay en profiles con PIN. Login redirige a /select-profile cuando profiles.length>1. TopBar dropdown gana "Cambiar perfil". Admin Users: botón "+ Perfil" en parents, "Poner PIN"/"Cambiar PIN" en cualquier row, profile rows con flecha (↳) y tag "perfil" + lock icon cuando hay PIN. switchProfile mutation hace `queryClient.clear()` para que el profile nuevo no vea Continue Watching del anterior.
> - `9e9f048` *auth(rating): per-profile content cap filter for browse / latest / detail*. **Content rating cap**. `internal/library/contentrating.go`: tabla de ranking que cubre MPAA (G/PG/PG-13/R/NC-17) + US TV (TV-Y/Y7/G/PG/14/MA). `AllowedRating(item, cap)` y `AllowedRatingsAtMost(cap)`. `db.ItemFilter.AllowedContentRatings` aplica `IN (...)` en raw SQL. LibraryHandler.Items + AllItems + LatestItems honran el cap; ItemHandler.Get devuelve 404 (NO 403, evita leak de existencia) cuando supera. Frontend admin: dropdown inline de rating en cada row de Users (11 opciones + "Sin límite"). 9 subtests pinean: empty-passes, PG-13-blocks-R, unrated-denied, unknown-cap-fail-open, TV-rating coherence, allow-list contents.
>
> **Verificación al cierre de la sesión**: `go test -count=1 ./...` verde (flakes preexistentes en `iptv.TestTransmuxManager_Touch_KeepsSessionAlive` + `handlers.TestItemHandler_TrickplayManifest_PendingDoesNotBlock` reproducen-and-pass aislados, no son introducidos esta sesión) · vitest 398/398 · tsc -b clean · production build clean · OpenAPI drift verde · sqlc-verify clean.
>
> **Decisiones senior tomadas** (anotadas para futuras sesiones):
> 1. **Profiles ARE users con parent_user_id**, no nueva entidad. Esto es lo que hace el feature realista de implementar — todo `user_data`/`favorites`/etc keyed por users.id sigue funcionando sin tocar. JWT contiene profile.id como user_id; frontend "switch profile" = nuevo JWT. Cero cambios en federation/streaming/middleware.
> 2. **Profile usernames synthetisados como `<parent.username>/<base>`**. Mantiene el UNIQUE constraint sin que el admin tenga que inventar usernames únicos para cada hijo. El frontend renderiza solo el sufijo (split("/").pop()).
> 3. **Wrong PIN devuelve `domain.ErrInvalidPassword`** (mismo sentinel que wrong password) para que un atacante no pueda enumerar profiles vs PINs.
> 4. **Item detail bloqueado por rating devuelve 404, no 403** — un kid profile no debe poder probar la existencia de contenido bloqueado.
> 5. **Auto-generated password modal devuelve plaintext UNA vez**. Sin retry, sin re-show. Si el admin lo cierra sin copiar, debe hacer reset-password. Documentado en el modal copy.
> 6. **`refreshMe()` en el auth store** post-mutation — ChangePassword + SwitchProfile lo llaman para que el cached user flag se actualice instantáneamente sin esperar al siguiente polling cycle de useMe (que rebotaría a /change-password de nuevo en el caso forzado).
> 7. **`AllowedRating("", cap)` deniega cuando cap no está vacío**. Items "unrated" en TMDb son típicamente contenido viejo / internacional que TMDb no categorizó — fail-closed para kid profiles es más seguro que asumir family-friendly.
> 8. **Cap filter NO aplica a /items/search ni /me/home/{trending,recommended}** todavía — son los siguientes en línea pero requieren plumbing similar (UserService en HomeHandler, en ItemHandler.Search). Documentado abajo como follow-up.
>
> **Quedan pendientes (cola priorizada para sesiones futuras)**:
> - **Cap filter en search + trending + recommended**: 3 endpoints más (HomeHandler.Trending, HomeHandler.Recommended, ItemHandler.Search). Mismo patrón: añadir `users UserService` al handler, `AllowedRatingsAtMost` en la query/post-fetch. ~1.5h.
> - **OpenAPI documentar** los 6 endpoints nuevos (paths están en allowlist out-of-scope, debería migrarlos a yaml propiamente). ~30 min.
> - **Admin Users page redesign** propiamente (la sesión metió el feature funcional pero el layout sigue siendo tabla; podría pasar al estilo editorial del Resumen). ~2-3h.
> - **Setup wizard** debería usar el auto-generated password flow para crear el primer admin (hoy pide password manual). ~1h.
> - **Trending del Home (user-facing)**: hoy filtra por `library_access` solo. Si un perfil tiene cap, su trending también debería honrarlo. ~30 min cuando se haga el bullet anterior.
> - **PIN brute-force protection**: hoy bcrypt-cost ~10 ya da rate-limit natural pero no hay lockout explícito por user. Si se vuelve un problema, añadir el mismo `loginRateLimiter` per-user con bucket separado. ~1h.
> - **"Editar perfil" modal** (display name + avatar color picker). Hoy color es determinista por username; permitir override sería el siguiente paso natural si user lo pide. ~1h.
>
> **Esta sesión NO ha tocado**: federation streaming, IPTV transmux, scanner, provider tmdb, recomendaciones provider. Todo el trabajo concentrado en `internal/auth/`, `internal/db/`, `internal/api/handlers/`, `internal/library/`, `web/src/pages/admin/`, `web/src/pages/{ChangePassword,WhoIsWatching}.tsx`, `web/src/components/home/HeroBanner.tsx` + admin Sparkline.

> 🎬 **Sesión 2026-05-08 (rama `claude/adoring-dhawan-7ff545`, PRs #196→#200 todas mergeadas a `main`)** — **Branding real + LiveTV redesign Plex-style en 6 commits**. Cierra el ítem #2 + #3 de la cola priorizada anterior (TV en vivo guide-style + polish vista canal). Cuenta corta porque la sesión iteró rápido contra el deploy del user.
>
> **Commits (de más reciente a más antiguo, ya en main)**:
> - `6f605f1` ⭐ *livetv: redesign hero with 2-column Plex layout + explicit CTAs*. **`HeroSpotlight` reescrito** a 2-col tipo Plex/watch.plex.tv: backdrop full-bleed + degradado izquierdo, columna izquierda con thumbnail/logo del canal y CTAs explícitas (Ver ahora · Más info · Añadir a favoritos), columna derecha con metadata extendida del programa actual + "A continuación". El patrón nuevo se vuelve referencia: cualquier hero del proyecto (Home, Detail) debería alinearse a este 2-col cuando el contenido lo justifique. **El bot recomienda usar este como canon estético** en futuras superficies.
> - `1ee33b5` *livetv: rework click model + restore global TopBar chrome*. La iteración anterior había roto el TopBar global en `/live-tv` (slim TopBar custom). Restaurado el chrome global (mismo TopBar que el resto del shell) + click model unificado: tap en celda EPG abre modal con detalles, doble-tap o tap en CTA dispara play. Antes el modelo era ambiguo y mezclaba navegación con play.
> - `7bf741d` *livetv: align section bg with global app shell (--color-bg-base)*. El fondo de sección difería de `--color-bg-base` por un override pre-redesign; resultaba en una franja visible al hacer scroll entre secciones. Trivial, solo CSS variable.
> - `4caf0a5` *livetv: fix i18n title bug, slim TopBar on /live-tv, polish EPG cells*. Bug i18n: el título de la página caía a la key cruda en es. Polish EPG: celdas con border-radius consistente, hover state + estado "en directo" más legible.
> - `62b44c3` ⭐ *livetv: collapse 4 tabs into Inicio + Explorar (Plex-style)*. La página tenía 4 tabs (Inicio / Guía / Canales / Grabaciones) — exceso para el MVP. **Plex usa 2** (Watch / Browse) y la decisión era replicar eso. Las funciones se reagruparon: "Inicio" trae spotlight + EPG horizontal, "Explorar" lista canales + filtros. Las grabaciones se posponen hasta tener feature real que mostrar.
> - `bda9217` *brand: replace placeholder mark with real hubplay logotype + favicon*. **`BrandMark` deprecado, sustituido por `BrandWordmark`** en TopBar/Sidebar; `web/public/hubplay_icon.svg` + `hubplay_icon_mark.svg` reemplazan el placeholder; favicon nuevo. Cierre cosmético antes de empezar a empujar a usuarios externos.
>
> **Verificación al cierre**: `go test ./... -count=1` verde · vitest verde · tsc clean · build clean. Producción `dev-3cd54f9` corriendo en hubplay.duckdns.org. El user verifica live: TV en vivo se ve coherente con Plex (esperado), branding nuevo aparece en TopBar y favicon.
>
> **Pendientes / cola priorizada al cierre** (ninguno crítico):
> - **#1 Hero del Home más interesante** (planteado por el user en la sesión 2026-05-09 como next-up). Hoy el Hero coge `[continueWatching, ...latestItems]` con filtro `backdrop_url`, slice 0..5, rotate 8s. **Limitaciones**: (a) cuando un slide es un EPISODIO, muestra el "still" del episodio como backdrop y no tiene póster lateral — el user quiere que muestre el **póster + backdrop de la TEMPORADA** como hace el detail page de la season; (b) selección naïf, mezcla `continue` y `latest` sin saber intenciones — el user quiere **tiers de slot por intención** (Reanudar / Próximo / Nuevo / Trending). El plan acordado: backend enriquece `/me/continue-watching` con `season_poster_url`, `season_backdrop_url`, `series_*` ya plumb-eados; frontend re-arquitectura `HeroBanner` con tiers + dedupe + label per-slide + pause-on-hover + deep-link al episodio (no al detail genérico).
> - **#2 360p auto-fanout** — sin cambio desde la sesión anterior.
> - **#3 Setup wizard avanzado** — pedir max sessions + cache path.
> - **#4 Intro animado tipo Netflix** — el backdrop loading overlay actual (commit 2d8514d) sigue siendo el canvas.
> - **#5 (UX pequeño) toast scan diferenciar 409 vs error real**.
>
> **Esta sesión NO ha tocado**: backend (Go), federation, IPTV transmux, auth keystore. Todo en frontend (livetv + topbar + branding) + assets públicos.



> 🎬 **Sesión 2026-05-07 noche (rama `claude/vibrant-ishizaka-57f510`, PRs #191 + #192 mergeadas a main, #193 con 4 commits abierto/construyendo)** — **8 commits cerrando producción + un feature grande (Now Playing admin panel) + bug raíz de las colecciones encontrado en directo contra prod**.
>
> **Commits (de más reciente a más antiguo, todos en la rama)**:
> - `1169806` *stream: drop redundant Session selector in ListAllSessions snapshot*. Lint fix CI: 3 findings staticcheck QF1008 — `ms.Session.X` se puede simplificar a `ms.X` por field promotion del `*Session` embebido. Cosmético, sin cambio de comportamiento.
> - `8afe094` *admin: same percent-encoded id fix for /admin/system/sessions/{id} kill*. Self-found mientras validaba el fix de colecciones — el botón Kill del Now Playing tenía el mismo bug: session keys son `userID:itemID:profileName`, frontend `encodeURIComponent` los rompe. Manager.StopSession es idempotente → respondía 204 sin matar la sesión real. Bug silencioso de los peores. Mismo fix `url.PathUnescape` + test de regresión.
> - `4fac3df` *api: decode percent-encoded id in /collections/{id}*. **Bug del usuario**: "Colección no encontrada" en cada saga. Reproducido contra prod en directo: list 200 ✓, detail 404 con id idéntico. Causa raíz: chi v5 NO decodifica path params automáticamente, IDs `collection:<tmdb_id>` con `:` se convierten en `%3A` por encodeURIComponent del frontend, handler buscaba en DB la cadena literal con `%3A`. Fix: `url.PathUnescape` antes del lookup. 2 tests nuevos pinning encoded + unencoded.
> - `4b769e2` ⭐ *admin: ship "Now Playing" panel with active sessions list + kill switch*. **Cierra la última gap grande del admin vs Plex/Jellyfin**. Backend: `stream.SessionSnapshot` + `Manager.ListAllSessions()`, `internal/api/handlers/admin_streams.go` con ListSessions (GET sorted by StartedAt desc, enriched con username + item title) + KillSession (DELETE idempotente). Routes en `/admin/system/sessions{,/{id}}` bajo `auth.RequireAdmin`. Frontend: `useAdminStreamSessions` poll 5s, `useKillAdminStreamSession` con optimistic remove, `NowPlayingPanel.tsx` con table User/Item/Method/Profile/Elapsed/Kill. Test seam público en `internal/stream/testseam.go` (`NewManagerForTest` / `SetSessionForTest` / `NewClosedSessionForTest`) para cross-package tests. 6 tests nuevos backend.
> - `2630897` *home: unify "En directo ahora" channel placeholder with LiveTV browser*. Home rail mostraba `channel.charAt(0)` en gris sobre negro para canales sin logo; LiveTV browser ya tenía colored-tile-with-initials. Backend `/me/home/live-now` ahora devuelve `logo_initials/bg/fg` vía `iptv.DeriveLogoFallback(name)` (mismo recipe que channelDTO). Frontend swap por `<ChannelLogo>` widget. Pixel-identical entre superficies.
> - `81153da` *HeroTrailer: keep backdrop visible if the iframe never loads*. Reveal usaba timer wall-clock 3.7s independiente de si el iframe cargaba. Con CSP block / network drop / extensión rompiendo YouTube (caso real del user con Audio Transposer), backdrop fade → iframe roto visible. Nuevo `iframeLoaded` state on `onLoad`, gate de reveal. 6s watchdog → handleDismiss si onLoad nunca dispara. `onError` belt-and-suspenders.
> - `a07324b` *player: route session-cleanup DELETE through ApiClient so CSRF doesn't 403 it*. Pagehide unload + close-button cleanup usaban raw `fetch(..., { method: 'DELETE' })` solo con Authorization. CSRF middleware bloqueaba con 403, idle reaper limpiaba a los 90s. Nuevo `ApiClient.stopStreamSession(itemID)` con `request<void>(..., { keepalive: true })` que pilla X-CSRF-Token automático.
> - `843616f` *api: allow YouTube + Vimeo oEmbed in connect-src so trailers actually load*. HeroTrailer hace pre-flight oEmbed `fetch()` antes de montar iframe; CSP `connect-src 'self'` lo bloqueaba en prod, todo trailer caía en silencio a "unavailable". Añadido `https://www.youtube.com https://vimeo.com` a `connect-src`. Test pinea con parsed-CSP helper (no substring match).
> - `79f8004` ⭐ *home: fix /me/home/trending 500 caused by non-UTC time.Time round-trip*. **Causa raíz confirmada contra DB de prod**: rows de `user_data.last_played_at` con forma `"2026-04-28 02:10:59 +0200 CEST m=+13.166591023"`. modernc.org/sqlite serializa `time.Time` no-UTC vía `String()` con monotonic clock suffix. `coerceSQLiteTime` no parseaba el `m=...` → 500 en cada carga de detail page. Fix dos lados: write side normaliza UTC en `UpdateProgress`/`MarkPlayed`/`SetFavorite`/`Upsert`/`nullableTimePtr`; read side strip ` m=±d.d` en `parseSQLiteTimeString` para recuperar legacy data sin migración.
>
> **Verificación al cierre**: `go test -race ./...` verde · golangci-lint v2.5.0 limpio · `tsc -b && vite build` clean · `vitest run` 394/394. Producción `dev-ade9ffc` (PR #192) corriendo en hubplay.duckdns.org con migraciones 33 aplicadas; el rail Trending pobla, "En directo ahora" muestra tiles coloreados consistentes, /collections lista 68 sagas (a partir de 201 pelis), Now Playing aparecerá tras siguiente deploy.
>
> **Lecciones senior anotadas para futuras sesiones**:
> 1. **chi v5 NO decodifica path params**. Siempre que un handler reciba un `chi.URLParam(r, "id")` cuyo formato pueda contener `:` o `/`, hacer `url.PathUnescape` explícito. Combinado con backends idempotentes (manager.StopSession), el bug se enmascara como 204 silencioso — peor que un error visible. Test de regresión debe verificar EFECTO LATERAL (count drops, row removed), no solo status code. Documentado en `~/.claude/.../memory/feedback_chi_path_param_decode.md`.
> 2. **Deployment lag es real**. Mergear PR != binario corriendo. Verificar `docker inspect ... --format '{{.State.StartedAt}}'` Y `/api/v1/health.version` antes de asumir que un bug está en código. Diagnosis local equivocada por una hora antes de descubrir que el contenedor era pre-merge.
> 3. **CSP `connect-src` es invisible hasta prod**. Dev environments rara vez tienen mismatches origin/proxy que disparen oEmbed cross-origin. Documentar terceros en el comentario del CSP cuando se añade cualquier feature que llame fuera del origin.
> 4. **Verificar contra prod live, no asumir**. La diagnosis del agent inicial sobre colecciones decía "row missing in DB" — falsa. La realidad solo se vio reproduciendo el flow encoded vs unencoded contra hubplay.duckdns.org. Memoria explícita ya tenía la regla pero merece reforzarla.
>
> **Cosas que el user verifica en su entorno tras CI rebuild + deploy de PR #193**:
> - Click en saga del rail home → renderiza la colección con miembros (no más "Colección no encontrada")
> - Admin Dashboard → Now Playing panel muestra sesiones activas en directo, Kill button funciona
> - Botón Kill realmente termina ffmpeg (no solo aparenta)
>
> **Pendientes / cola priorizada al final** (ninguno crítico, todos en orden de mi recomendación senior):
> - **#1 360p auto-fanout** — hls.js prefetcha 360p en cada seek, duplicando ffmpeg work. Necesita decisión: (a) master 1-variante, (b) `startLevel` fijo + `capLevelToPlayerSize`, (c) lazy-spawn por bandwidth real. Mi voto: (b).
> - **#2 TV en vivo guide-style estilo Plex** — User pasó referencia https://watch.plex.tv/es/live-tv, quiere featured player + EPG horizontal por categoría. NO sustituir browser actual; añadir como pestaña "Guía".
> - **#3 Polish vista canal en directo** — Plex/Jellyfin tienen tratamiento más rico (info programa actual sobre video, EPG horizontal, descripción extendida).
> - **#4 Setup wizard avanzado** — pedir max sessions + cache path durante install.
> - **#5 Intro animado tipo Netflix** — backdrop loading overlay actual (commit 2d8514d sesión anterior) es el canvas para slot la animación.
> - **#6 (UX pequeño) toast scan diferenciar 409 vs error real** — frontend muestra "No se pudo iniciar el escaneo" en cualquier no-2xx, incluyendo 409 conflict (ya hay scan en marcha). Confunde al user que ve "error" cuando es success.
>
> **Esta sesión NO ha tocado**: federation, IPTV transmux, auth keystore, live-TV player. Todo en home repo + admin + collections + stream manager + frontend admin pages.



> 🎬 **Sesión 2026-05-08 (rama `claude/review-player-tasks-4guc9`, PRs #185 + #186 mergeadas, #187 abierta con 3 commits adicionales)** — **Bug raíz del seek cascade cerrado (`-copyts` en ffmpeg), trickplay regenerado adaptativo, async generation cierra el 504, backdrop loading overlay Jellyfin-style, 6 commits**. Cierra los 4 bugs declarados en `docs/memory/player-seek-bugs-2026-05-07.md` end-to-end con verificación del usuario en producción.
>
> **Commits (de más reciente a más antiguo, todos en la rama)**:
> - `2d8514d` *player: backdrop loading overlay (Jellyfin/Plex-style) until first frame paints*. Cierra el gap "video negro 2-5 s mientras ffmpeg arranca". `VideoPlayer` recibe `backdropUrl?` opcional y mantiene `firstFrameReady` state que flippea en el evento `playing` (no `play` ni `loadeddata`). Overlay full-bleed con backdrop + degradado vignette + título/logo bottom-left + barra fina indeterminada arriba (animación `loading-slide` GPU-only). Fade de 500 ms al frame uno. Reset on `itemId` change para next-up. Callers (`ItemDetail`, `PeerItemDetail`) pluman el backdrop que ya tenían.
> - `3f0ee55` ⭐ *stream: add -copyts to ffmpeg so seek-restart segments align with manifest (root cause fix)*. **LA CURA REAL**. ffmpeg sin `-copyts` resetea PTS a 0 en cada restart → manifest dice "seg 296 está en [1776, 1782]" pero el archivo `.ts` tiene PTS [0, 6] → MSE construye timeline Frankenstein → hls.js fan-out a múltiplos del seek (+297 segs cadence) → MediaSource colapsa duration → player termina en EOF → Play arranca de 0. `-copyts` aplicado unconditionally después de `-i` (no-op cuando startTime=0 anyway). Plex/Jellyfin lo usan por la misma razón. Test `TestBuildFFmpegArgs_AlwaysIncludesCopyts` con 3 casos. **Verificado por user en producción**: "el ffmpeg funciona perfecto".
> - `b2374de` *player: defensive currentTime restore on play + opt-in hls.js debug*. `lastGoodTimeRef` en VideoPlayer actualiza en cada timeupdate con `currentTime > 0.5`; `onPlay` lo restaura si play() landea en 0 con un valor recordado. Defensa contra "Play tras pause empieza de 0" si por edge case currentTime se resetea (probablemente innecesario tras `-copyts` pero cheap belt-and-suspenders). `useHls` añade `debug: window.__hp_debug_hls === true` para opt-in verbose logging.
> - `ac601bc` *trickplay: async generation + smaller sprite to fix 504 timeouts*. Mi fix anterior (adaptive grid) generaba 360-720 thumbs por sprite que tomaba 60-120 s en CPU stock; el reverse proxy 504-eaba antes. Refactor: `ensureTrickplay` no bloquea HTTP, spawn ffmpeg en goroutine con `context.Background()`, devuelve `errTrickplayPending` → 503 + `Retry-After: 10` con código `TRICKPLAY_PENDING`. Per-item `sync.Mutex` con `TryLock`. `maxThumbsPerSprite` 400→200 (sprite ~3 MB PNG, ffmpeg <30 s). 3 tests handler nuevos (fresh / pending-non-blocking / stale).
> - `b96983f` *player: fix "second seek feels blocked" + harden seek-state tracking*. AND-coalesce: `RestartSessionAt` colapsa solo si `time<2s AND segment≤6` (antes solo segmento ±10, atrapaba seeks humanos vecinos). Nueva field `LastRestartTime` en `ManagedSession`. Reduced `restartCoalesceWindow` 10→6 segs. Frontend: reemplaza `isSeekingRef` con lectura directa de `video.seeking` (autorrecupera si un evento `seeked` se cae). Test `_StaleCoalesceDoesNotBlock`.
> - `2ce4a5c` *player: end-to-end fix for the 2026-05-07 seek + trick-play bugs*. Primera tanda. SeekBar pointerup-commit pattern (Plex/YouTube), `isSeeking` gate (luego sustituido por `video.seeking` directo en `b96983f`), progress reporter skip mientras seeking, defensive `lastGoodTimeRef` en useHls + MEDIA_ATTACHED restore, **trickplay generator adaptive grid** (`maxThumbsPerSprite=400` luego bajado a 200, `IntervalSec` adaptativo a duración, `Total = ceil(duration/interval)` real, `Version` stamp para invalidar v1 caches), **per-session restart rate limit** (sliding window 20/60s, 429 + Retry-After).
>
> **Verificación al cierre**: `go test ./... -count=1` verde · `vitest run` 394/394 (era 392, +2 progressReporter seeking-skip) · `tsc -b` clean · production build clean · `pnpm build` clean. Tests nuevos backend: `TestBuildFFmpegArgs_AlwaysIncludesCopyts` (3 casos), `TestTrickplayParams_Adapt` (4), `_NoDuration`, `_GridAlwaysFits`, `TrickplayManifestVersion_NonZero`, `_RestartSessionAt_RateLimited`, `_RateLimitWindowResets`, `_StaleCoalesceDoesNotBlock`, `TestItemHandler_TrickplayManifest_FreshCacheServesImmediately`, `_PendingDoesNotBlock`, `_StaleCacheReturnsPending`. Tests nuevos frontend: 2 en `useProgressReporter.test.ts` (skip while seeking).
>
> **Lecciones senior** (anotadas para futuras sesiones):
> 1. **No declarar "closed" antes de verificación en prod**. La primera resolución decía "fixed end-to-end" sin que el user hubiera testado en su entorno con una peli larga real; el cascade volvió a aparecer y resultó tener una causa diferente a la que asumimos.
> 2. **Server logs algorítmicos NO son suficientes**. Las cadencias `+366 / +231 / +297` en logs lucían algorítmicas pero no apuntaban a la causa. El snippet de debug del navegador (XHR + video events + duration changes) fue lo que surfaceó el timeline collapse — y desde ahí `-copyts` fue 5 minutos.
> 3. **Integridad del timeline MSE es frágil**. Si el manifest y los PTS reales del segmento discrepan, MSE construye un timeline Frankenstein silenciosamente; los síntomas downstream (fetches en cascada, duration colapsando, playhead saltando) parecen bugs de manejo de seek pero no lo son.
>
> **Defensive layers que se quedan** (todas <50 LOC, protegen contra regresiones futuras):
> - SeekBar pointerup-commit (Plex/YouTube pattern, correcto al margen de `-copyts`)
> - `video.seeking` direct read en timeupdate (self-recovery si `seeked` se cae)
> - `useProgressReporter` skip while seeking (evita corromper resume state)
> - `lastGoodTimeRef` recovery × 2 (en useHls para MEDIA_ATTACHED, en VideoPlayer para `play` event) — duplicación admitida porque cada ref vive en scope local sin coupling
> - AND-coalesce + restartCoalesceTimeWindow + restartRateLimit (defensa server contra regresiones de cliente)
> - Trickplay version stamp (invalidación cuando cambie el contrato)
> - hls.js debug opt-in via `window.__hp_debug_hls = true` (puramente diagnóstico, off por default)
>
> **Cosas que el user verifica en su entorno tras CI rebuild**:
> - Click en barra a cualquier minuto → seek instantáneo, no se cuelga (commit `3f0ee55`)
> - Pause + Play → resume desde donde estaba, nunca desde 0 (`3f0ee55` + `b2374de` defensa)
> - Hover en timeline → thumbnail correcto en cualquier punto (commit `2ce4a5c` adaptive trickplay)
> - Trickplay primera vez → 503 + retry tras 10 s, no 504 colgado (commit `ac601bc`)
> - Click Play en peli → backdrop fullscreen con título/logo + barra de carga, fade al video (commit `2d8514d`)
> - Logo de la peli en el overlay sale del `item.logo_url` actual; cuando se reemplace por el definitivo lo coge automático sin tocar código.
>
> **Pendientes / cola al final** (todo out-of-scope para esta sesión, ninguno crítico):
> - **DELETE `/stream/.../session` 403** al cerrar player → CSRF middleware bloquea el cleanup; hoy idle reaper de 90 s lo limpia igual. Fix: exempt esa ruta de CSRF o pasar token.
> - **360p auto-fanout** vía hls.js ABR — duplica trabajo ffmpeg cada seek. Tres opciones: (a) servir un master con UNA variante (perdemos ABR), (b) configurar hls.js con `startLevel` fijo + `capLevelToPlayerSize`, (c) lazy-spawn 360p solo si bandwidth real lo justifica.
> - **CSP block YouTube oembed** trailer feature → añadir `connect-src https://www.youtube.com https://vimeo.com` al CSP en producción.
> - **`/me/home/trending` 500** → parse error documentado out-of-scope desde 2026-05-07; `time.Time.String()` round-trip con sufijo de monotonic clock. Sin diagnosis aún.
> - **`/channels/*/logo` 404** masivo → IPTV provider no popula logos; degradar elegante en frontend o pre-cachear.
> - **Admin "Now Playing" panel** — el backend ya tiene `manager.sessions[]` con per-user info, falta un endpoint `GET /admin/system/sessions` y un componente que la renderice + botón Kill por sesión. ~½ día. ÚLTIMA gap grande del admin panel vs Plex/Jellyfin.
> - **Setup wizard avanzado** — pedir max sessions y cache path durante install (hoy todo default).
> - **Intro animado tipo Netflix** — el user lo dijo explícitamente, lo quiere fabricar después; el backdrop loading overlay actual es el canvas correcto para slot esa animación cuando llegue.
>
> **Esta sesión NO ha tocado**: `internal/federation/`, `internal/iptv/`, `internal/auth/`, live-TV player (`useLiveHls`). El bug del seek era VOD-only.

> 🎬 **Sesión 2026-05-06 noche (rama `claude/vigilant-noyce-d2f526`, PR pendiente)** — **Recommendations bug crítico + Plex polish + studios clicables + colecciones de saga Jellyfin-style + 5 commits + 2 features grandes nuevas**. Cierra **dos** de las cosas diferidas (estudio clicable + reorden series) + bug fix de regresión + nueva feature solicitada (colecciones). Bio de actor sigue diferida explícitamente por el user. Todo pusheado.
>
> **Commits (de más reciente a más antiguo, en la rama)**:
> - `b3be923` *api(openapi): document /studios + /collections endpoints*. CI failover — `TestOpenAPISpec_RouterCoverage` cazó las 4 rutas nuevas como undocumented. 4 paths + 5 schemas (`StudioListEntry`, `StudioItemRef`, `StudioDetail`, `CollectionListEntry`, `CollectionDetail`). `StudioItemRef` es el shape compartido por studio.items y collection.items para que el Tile component frontend renderice las dos sin special-casing.
> - `11a081d` *collections: Jellyfin-style movie sagas (X-Men, MCU, Toy Story, ...)*. **Migración 033** `collections(id, tmdb_id UNIQUE, name, overview, poster_url, backdrop_url)` + `metadata.collection_id` FK. id canónico `collection:<tmdb_id>` — el row key es predecible desde provider data sin slug step. **TMDb provider** parsea `belongs_to_collection` (movies-only; TV no carry); scanner ensure-and-link tras metadata.Upsert. **Backend** `GET /api/v1/collections` (browse) + `GET /api/v1/collections/{id}` (saga hero + miembros en release order). **Item detail wire** emite `collection: {id, name}` cuando hay match. **Frontend** ruta `/collections/:id` con backdrop bleed + póster + overview + grid de miembros; chip clicable "Parte de: <saga>" entre director y studio mark en HeroSection (movies-only). i18n+es. CollectionTMDBID == 0 deja el link NULL — TV y movies sin match TMDb no rinden la chip.
> - `a19709b` *studios: clickable studio mark + per-studio collection page*. **Migración 032** tabla normalizada `studios(id, tmdb_id UNIQUE, name, slug UNIQUE, logo_url)` + `metadata.studio_id` FK ON DELETE SET NULL. **Backfill** SQL walk: cada `metadata.studio` text distinto → fila en `studios` con la misma slug recipe que `db.Slugify`, luego UPDATE para wirelink — **no requiere re-scan** para items existentes. **TMDb provider** ahora expone `production_companies[].id` (antes descartado) + fallback a `networks[]` para TV. **Scanner** `EnsureStudio` por item-with-studio (dedup por tmdb_id, fallback por slug). **Backend** `GET /api/v1/studios` (browse sorted by item_count desc) + `GET /api/v1/studios/{slug}` (header + items). **Item detail wire** ahora carga `studio_slug` (Slugify del meta.studio en handler — sin lookup adicional). **Frontend** `<StudioMark>` renderiza `<Link>` cuando hay slug; nueva ruta `/studios/:slug` con hero (logo en pill blanco grande) + grid agrupado movies/series. `useStudio`+`useStudios` hooks 5min staleTime.
> - `da5f3ee` *recommendations: fix in-library cross-reference + Plex-style polish pass*. **Bug crítico**: el rail "Más como esta" mostraba todas las cards con badge TMDb gris incluso para pelis que el user SÍ tenía en librería (deep-link al item local nunca disparaba). Root cause: sqlc v1.31.1 truncó `LIMIT 1` a `LIMIT` en `GetItemIDByExternalID` → SQL inválido en runtime → `lookupErr != nil` siempre → `in_library: false` siempre. Fix: raw SQL bypass en repo (mismo patrón ya usado en `ListGenres`). Test de regresión `TestExternalIDRepository_GetItemIDByExternalID_RoundTrip` pinea round-trip + missing-pair. **Polish**: copy "En tu librería" → "En HubPlay" (en+es); `<StudioMark>` movido a línea propia sobre chips (Plex-style — antes inline competía con badges, leía pequeño); pill subido a h-10 w-[140px] (era h-8 w-[112px]); reorden ItemDetail series → Continue Watching + Seasons al top, luego Cast/Recommendations/MediaInfo (media-info al bottom — alta señal solo para admin debug); rediseño PersonDetail con hero band + backdrop blur del foto + avatar h-44/h-56 ring+shadow + filmografía con **pósters reales** (LEFT JOIN images en `ListFilmographyByPerson` — una query, comentario que decía "100 queries" era erróneo) agrupada Movies/Series.
>
> **Verificación al cierre**: `go test ./internal/db/ ./internal/api/ ./internal/api/handlers/ ./internal/scanner/ ./internal/library/` verde · `tsc -b` clean · `vitest run` 391/391 · `go vet` clean · `sqlc generate` no diff inesperado · OpenAPI drift verde · preview server arranca sin errores cliente. Suma 3 tests backend (`TestExternalIDRepository_GetItemIDByExternalID_RoundTrip`, `TestSlugify` con 8 casos, `TestStudioRepository_EnsureAndList`, `TestCollectionRepository_EnsureAndList`).
>
> **Decisiones senior tomadas** (anotadas para futuras sesiones):
> 1. **Tabla dedicada para studios + collections, no `item_values`**. El patrón `item_values` (genres) demostrado funciona pero studios/collections tienen artwork propio (logo_url, poster_url, backdrop_url, overview) que no encaja en `(value, clean_value)`. Tabla normalizada también permite UNIQUE(tmdb_id) para dedup canonical contra alias drift ("Lucasfilm" vs "Lucasfilm Ltd.").
> 2. **`Slugify` recipe compartida Go ⇄ SQL**. La migración 032 hace backfill desde SQL puro con un chain de REPLACE; `db.Slugify` en Go reproduce el mismo recipe. Test `TestSlugify` pin la paridad — un futuro cambio en una path debe espejarse en la otra. Ampersand → "and", apóstrofe → drop, todo no-alnum → '-' colapsado.
> 3. **Item detail wire emite `studio_slug` derivado de `Slugify(meta.studio)`**, NO un lookup a `studios.slug`. Evita un DB roundtrip por render del detail page; el slug es predecible desde el name cuando agree los dos paths.
> 4. **`collection: {id, name}` en wire es slim** (solo id+name); el frontend hace fetch a `/collections/{id}` para hero completo. Evita inflar el wire con poster/backdrop/overview que solo se usan al click-through.
> 5. **`UpsertCollection` con `CASE preserve-on-empty`** en lugar de overwrite simple. Re-scan que vuelve sin backdrop NO blanquea el que ya teníamos. Recomendado para futuros upserts donde TMDb puede devolver subset.
> 6. **Bio de actor diferida explícitamente** — el user dijo "blog no" (bio no). Cuando se haga: TMDb `/person/{id}` provider extension + DB migration `people.biography/birth_date/place_of_birth` + scanner enrichPersonMetadata pass. ~3-4h. NO crear nueva página: el hero band actual de PersonDetail está dimensionado para acomodar bio bajo el name+count.
>
> **sqlc gotchas (CINCO queries truncadas total ya)**:
> - `GetItemIDByExternalID`: `LIMIT 1` → `LIMIT` (esta sesión, fix raw SQL en `external_id_repository.go`)
> - `ListGenres`: `iv.clean_value ASC` → `iv.clean_val` (sesión anterior)
> - `UpsertCollection`: `END` → `E` (esta sesión, fix raw SQL en `collection_repository.go::EnsureCollection`)
> - `ListCollections`: `ASC` → `A` (esta sesión, raw SQL)
> - `ListItemsForCollection`/`ListItemsForStudio`: documentadas raw SQL desde el inicio en este patrón
>
> **Patrón canónico** ya documentado en cada `.sql` correspondiente y en `docs/memory/conventions.md`: cuando una query tiene trailing `ORDER BY ... ASC`/`LIMIT N`/`END` y es la última de su archivo (en alfabético generated O en source), bypassear sqlc con raw SQL en el repo. **Considerar** subir sqlc a una versión que arregle esto cuando releasee.
>
> **Quedan pendientes (cola priorizada)**:
> - **Bio del actor** (~3-4h): TMDb `/person/{id}` + migration + scanner. Va al hero band existente de PersonDetail.tsx. **El user lo dejó explícitamente para más adelante**.
> - **Colecciones full TMDb** (opcional, ~2-3h): fetch `/collection/{id}` para mostrar miembros que el user NO tiene (estilo "tienes 3 de 7" — mismo patrón que recommendations external suggestions con badge "TMDb").
> - **Studios browse page** (~30min, MUY pequeño): ya existe el endpoint `GET /api/v1/studios`, falta una ruta `/studios` index page que renderice el grid usando `useStudios` (browser landing). Mismo treatment que las páginas tipo Persons / Movies.
> - **OMDb provider** (sigue diferido sin plan, baja prioridad).
>
> **Cosas que el user verifica en su entorno** (preview no puede sin DB poblada):
> - **Recommendations rail**: añadir una peli que aparece en "Más como esta" → recargar → debe pasar de badge "TMDb" gris a "En HubPlay" verde + deep-link a `/movies/{id}`.
> - **Studios clicables**: 032 hace backfill — funciona inmediato post-deploy. Click en logo de productora en cualquier detail page → `/studios/<slug>` con grid de items.
> - **Colecciones de saga**: 033 NO backfillea — necesita re-scan o **Refresh metadata** por librería de movies para que el scanner persista `belongs_to_collection`. Tras eso: chip "Parte de: <saga>" en HeroSection + ruta `/collections/{id}` operativa.
> - **Reorden series detail**: en cualquier `/series/{id}` con resume activo, "Sigue viendo" + "Temporadas" deben aparecer ARRIBA del cast/recommendations.
> - **PersonDetail rediseño**: visitar página de un actor — foto h-56 grande, hero band con backdrop blur, filmografía con pósters reales (no la "A" placeholder que había antes).
>
> **Esta sesión NO ha tocado**: nada en `internal/federation/`, `internal/iptv/`, `internal/auth/`, `internal/stream/`. Todo en items/people handlers + nuevo studio_repository + nuevo collection_repository + scanner + provider/tmdb.go + frontend páginas+components+i18n. Cola P0/P1 senior-review sigue intacta.

> 🎬 **Sesión 2026-05-06 tarde (rama `claude/fix-visual-design-TZjub`, PR pendiente)** — **Filtros server-side en /movies y /series + studio-mark con footprint fijo + 2 commits**. Cierra una de las 3 cosas diferidas de la sesión 2026-05-06 mañana (regresión funcional en libs grandes) + arregla inconsistencia visual reportada por el user mirando el deploy real.
>
> **Commits (de más reciente a más antiguo, todos en la rama, pusheados)**:
> - `42bb76b` *detail: studio-mark fixed-footprint pill for consistent visual weight*. Logos cuadrados (WB, Disney) salían como puntos pequeños y horizontales (Marvel Studios, Pixar) ocupaban 3× más en el mismo hero porque el pill crecía con el aspect ratio del PNG de TMDb (`h-5 sm:h-6`, `max-w-[140px]`, sin footprint fijo). Pill ahora `h-8 w-[112px]` fijo, imagen dentro `max-h-5 max-w-full object-contain` → todos los logos en la misma "tarjeta de créditos" con vertical breathing room. User reportó esto mirando capturas de "Fantastic Beasts" (WB pequeño) vs "Civil War" (Marvel enorme).
> - `44f4817` *browse: server-side filters + paginated search across the whole catalogue*. Cierra el item `#2` diferido de la sesión mañana: filtros y `?q=` eran client-side sobre las páginas cargadas; con `/items` paginado a 40 items, libs grandes filtraban solo el 20%. **Migración 031** backfillea `metadata.genres_json` a `item_values` + `item_value_map` (tablas que existían en el schema inicial pero NUNCA se usaron — diseñadas exactamente para esto). Índice `(type, clean_value)`. **`ItemFilter`** gana `Genre/YearFrom/YearTo/MinRating`; `List()` raw SQL añade WHERE condicionales (subquery indexada para genre vía item_value_map, equality directa para year/community_rating). **Scanner** espeja géneros a `item_values` con replace-semantics tras cada `metadata.Upsert`. **`AllItems`** y **`Search`** handlers leen los nuevos params; nuevo **`GET /items/genres`** devuelve vocabulario catálogo-wide con counts y scope `?type=movie|series` (TV-only no contamina /movies). Frontend: `useInfiniteItems` reenvía los nuevos params; nuevo `useGenres` 5min staleTime. **MediaBrowse** borra todo el filtering client-side y mete filtros en URL (`?genre=Action&year_from=2010&min_rating=7&sort=year`) → shareable + back-button-friendly como Plex/Jellyfin. **MediaBrowseFilters** consume el vocabulario del server (no deriva de los items cargados). OpenAPI extendida con drift test verde.
>
> **Verificación al cierre**: `go test ./... -count=1` verde · `go test -race ./internal/db/... ./internal/library/... ./internal/scanner/... ./internal/api/...` verde · `tsc -b` clean · `vitest run` 391/391 · `make sqlc-verify` clean · OpenAPI drift verde. Suma 4 tests backend (`TestItemRepository_List_GenreYearRatingFilters`, `TestMigration031_BackfillsGenresFromMetadata`, `TestItemValueRepository_ListGenres`, casos en `library_test.go`) y 3 tests vitest (`MediaBrowse.test.tsx` cubre el round-trip URL→request, ausencia de filtering client-side, y URL writes desde el panel).
>
> **Decisiones senior tomadas** (anotadas para futuras sesiones):
> 1. **Géneros normalizados** vía `item_values`+`item_value_map` (preexistentes, sin uso) en vez de LIKE sobre `genres_json` o tabla nueva. Plex/Jellyfin tratan género como tag de primera clase; LIKE es deuda técnica. La tabla además queda lista para hostear tags/studios/mood futuros sin más migraciones.
> 2. **`/items/search` se queda como endpoint compat** pero MediaBrowse usa `/items` con `q` para evitar duplicar la lógica de paginación + filtros. SearchBar global sigue usando `/items/search` con su limit pequeño.
> 3. **Filtros en URL** (no useState). Una página de browse en un media server **debe** ser shareable; el patrón previo perdía estado al navegar.
> 4. **Genre single-select server-side**, no multi-select. Multi-select necesita `IN (?...)` con length variable, no merece la pena hasta que un user lo pida — la chip semantics actual (click para activar/desactivar) sigue siendo intuitiva.
>
> **sqlc gotcha hallado esta sesión**: el parser v1.31.1 trunca el último identificador de la última query en un archivo (`ORDER BY count DESC, iv.clean_value ASC` se generó como `iv.clean_val`). Workaround: `ListGenres` se escribió raw SQL en `item_value_repository.go` (List() raw ya está documentado como exception en items.sql). Anotado en docs/memory/conventions.md cuando lo actualice; precedente: bugs sqlc ya documentados en convenciones.
>
> **Quedan pendientes de la sesión 2026-05-06 mañana** (cola priorizada):
> - **Click en estudio → página de colección con hero** (~2-3h). Backend `GET /api/v1/studios/{slug}/items`, ruta `/studios/:slug` con hero del logo + grid + paleta extraída del logo. Decisión a tomar en su momento: filtro `metadata.studio` con LIKE vs tabla normalizada `studios(id,name,logo_url)` (mejor opción ahora que el patrón `item_values` ya está demostrado funcionando — lo lógico sería extender ese mecanismo a un `type='studio'` o crear tabla dedicada porque studios tienen logo_url asociado, no solo nombre).
> - **OMDb provider** (opcional, baja prioridad) — único camino para nota IMDb numérica. User dijo dejarlo de momento.
>
> **Cosas que el user verifica en su entorno** (preview no puede sin DB poblada):
> - Filtros de género funcionan inmediatamente post-deploy gracias al backfill de migración 031 (no requiere re-scan); año/rating funcionan desde el primer momento porque ya estaban en `items`.
> - URL share-test: ir a `/movies?genre=Action&year_from=2010` y compartir el link debe preservar filtros.
> - Logos de productora — comparar WB y Marvel Studios en la misma página, deben tener el mismo tamaño visual.
>
> **Esta sesión NO ha tocado**: nada en `internal/federation/`, `internal/iptv/`, `internal/auth/`, `internal/stream/`, `internal/provider/`. Todo en items handler / library service / db items + nuevo item_values repo / scanner mirror / frontend MediaBrowse + filters + heroMeta. Los gaps de tests senior-review (P0 hardening, P1 splits) siguen sin tocar pero no surgen como dolor en este flujo.

> 🎬 **Sesión 2026-05-06 (rama `claude/elated-ellis-f9bc9c`, PR abierta)** — **Detail page user-facing pass + bug fix crítico + 7 commits**. Iteración guiada por screenshots reales del despliegue del user en `hubplay.duckdns.org`. Todo pusheado, listo para Docker rebuild.
>
> **Commits (de más reciente a más antiguo, todos en la rama)**:
> - `afee990` *detail: brand-coloured ID chips, white-chip studio mark, chapter tooltips*. IMDb / TMDb / TVDB chips con paletas oficiales (#F5C518 IMDb, #032541 + #01B4E4 TMDb, #6CD491 TVDB) inline + nota TMDb al lado de su chip (`community_rating` ES `vote_average`); IMDb sin nota porque no hay API libre; RT skipped sin data. Logo del estudio envuelto en pill `bg-white/95` (h-5→h-6) — TMDb sirve PNG con foreground arbitrario (Marvel negro, Disney azul, Pixar amarillo); pill blanco lo hace legible sea cual sea. Chapter markers del player: tooltip custom encima del seek bar con título + timecode (`1:24:18`), reemplaza el `title=` HTML lento; hit-target 4 px bajo tick visible 2 px; clamp horizontal `clamp(70px, X%, calc(100% - 70px))` para que primer/último capítulo no se salgan.
> - `5d75e36` *detail: oEmbed gate, kebab cleanup, recommendations rail + play fix*. **Bug crítico de prod**: `<Button onClick={onPlay}>` en HeroSection forwardea SyntheticEvent → `handlePlay` lo coge como `targetId` → URL `/stream/[object%20Object]/info` → 404 → "Error al iniciar la reproducción". Fix: `() => onPlay?.()`. **oEmbed pre-flight**: antes de montar el iframe, fetch a `youtube.com/oembed`/`vimeo.com/oembed` — 401/404 short-circuita a render vacío, el user nunca ve "Este vídeo no está disponible" embebido. Gated en el viewport check existente. **Kebab cleanup**: quitados "Ver en IMDb / TMDb" (ya están como chips inline), ARIA `menu`/`menuitem`, contraste subido. **Recommendations rail**: migración 030 índice reverso `external_ids(provider, external_id)`; nuevo `provider.GetRecommendations` (TMDb `/movie/{id}/recommendations` o `/tv/{id}/recommendations`); nuevo `GET /api/v1/items/{id}/recommendations` que cruza candidatos con la librería local y marca `in_library: true` + `local_id` cuando existe; frontend `<RecommendationsRail>` bajo el cast con badge "En tu librería" (verde, locales, deep-link a `/movies/{id}`) o "TMDb" (gris, externos, abren TMDb en nueva tab); cache TanStack 10 min; i18n en+es.
> - `07a0d1f` *api(items): include sort_title + harden client sort against absence*. Crash en /movies y /series tras flippear default sort a "title": `itemSummaryResponse` no incluía `sort_title` y `sortItems` lo dereferenciaba (`localeCompare(undefined)`). Backend ahora lo serializa; frontend cae a `title ?? ""` defensivo; `MediaItem.sort_title` pasa a opcional en TS para reflejar la realidad del wire.
> - `7dd5f02` *api(items): paginated /items + share hero meta primitives across heroes*. Movies/series solo mostraban ~50 items: `/items/latest` está capado a 50 server-side y no pagina, era el fallback cuando no hay `library_id`. Nuevo `GET /api/v1/items` paginado que reusa `lib.ListItems` sin scope de library; `useInfiniteItems` pasa `sort_by=sort_title&sort_order=asc` por defecto; `MediaBrowse` default sort dropdown a "title". Extraídas primitivas compartidas a `web/src/components/media/heroMeta.tsx` (ExternalIdRow, OverviewWithReadMore, StudioMark) + `web/src/utils/heroMeta.ts` (formatPremiereDate, helpers separados por la regla `react-refresh/only-export-components`). SeriesHero ahora tiene los mismos enhancements que HeroSection (fecha completa, read-more, external IDs, studio logo) — director skipped en series porque TMDb no devuelve un único director a nivel show. Texto del overview/tagline/director/meta row subido de `text-text-secondary` (#8B92A5 gris) a `text-text-primary` 80-95% opacidad (off-white).
> - `8533905` *web(itemDetail): trailer reveal on movies + per-user opt-out toggle*. El trailer Netflix-style que ya disparaba en SeriesHero ahora también dispara en HeroSection para movies (mismo fade-out del backdrop cuando el iframe revela). HeroTrailer extraído a `web/src/components/media/HeroTrailer.tsx` con sus helpers (~270 LOC), reusado por ambos heroes. Nueva preferencia `playback.trailers_enabled` (default `true`) persistida en `/me/preferences` así sigue al user entre dispositivos. Settings → Reproducción nueva sección con toggle estilo switch.
> - `8f044cf` *web(itemDetail): director, full date, read-more, external IDs, studio logo*. "Directed by …" extraído de `people` (filtro role==="director", case-insensitive); fecha completa para movies (`25 may 2018` localizada via i18n) en vez de bare year; read-more/less del overview con threshold 240 chars; chips externos IMDb/TMDb/TVDb. Backend: migración 029 `metadata.studio_logo_url`, TMDb provider lee `production_companies[].logo_path` y resuelve a URL absoluta `image.tmdb.org/t/p/w300/...`, scanner persiste, handler expone, OpenAPI extendido. **Necesita `Refresh metadata` por librería para items ya escaneados — items nuevos lo recogen automático**.
> - `6ac3c14` *web(hooks): cache trickplay manifest + skip orphan invalidations*. F8 audit-2026-05-05 cerrado: `useUserDataSync` ahora gatea `item(id)` y `progress(id)` invalidations en `getQueryData(key) !== undefined` (rails globales `continueWatching`/`nextUp`/`favorites` siguen invalidándose siempre porque Home los consume). `useTrickplay` migrado de `useState + useEffect + AbortController` a `useQuery` con `staleTime: 5 min`, `retry: false` — cold-start ffmpeg de 5-30s ya no se vuelve a pagar al remontar el mismo item.
>
> **Verificación al cierre**: `tsc -b` clean · `vitest run` 392/392 (sumó 8 tests entre los nuevos hooks) · `go test ./internal/api/handlers/... ./internal/db/... ./internal/provider/... ./internal/scanner/...` verde · lint limpio en ficheros tocados · preview bundle compila + index renderiza sin errores de consola en cada paso de verificación.
>
> **Cosas que el user verifica en su entorno (lo que el preview no puede hacer sin DB poblada)**:
> - Director / fecha completa / read-more / chips externos / nota TMDb inline → necesitan TMDb match (la mayoría de items lo tienen).
> - Logo del estudio en pill blanco → necesita `Refresh metadata` para items pre-deploy (la migración 029 añadió la columna con `''` default).
> - Recommendations rail → necesita `tmdb_id` en `external_ids` del item (típico post-scan con TMDb provider configurado). Migración 030 (índice reverso) acelera el cross-reference.
> - Tooltip de capítulos → necesita un archivo con marcadores; mkv con chapters.txt los lleva, otros no.
>
> **sqlc gotcha hallado esta sesión**: el `go install` default trae v1.29.0 que regenera `internal/db/sqlc/federation.sql.go` con `HasPoster int64` en vez del `bool` que el repo committed con v1.31.1. Hay que correr `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1` (o `make sqlc-install`) ANTES de `sqlc generate` para no introducir diff espurio.
>
> **Bug que NO era bug**: en una pasada inicial el linter `react-hooks/set-state-in-effect` saltó sobre HeroTrailer cuando lo extraje. Resultó ser pre-existente en SeriesHero (no se hizo visible al estar inline). Cubierto con `eslint-disable-next-line` + comentario explicando por qué el setState dentro del effect es intencional (fallback a jsdom sin IO).
>
> **Diferido a próxima sesión** (con plan):
> - **Click en estudio → página de colección con hero**. Factible ~2-3h: backend `GET /api/v1/studios/{slug}/items`, ruta `/studios/:slug` con hero del logo + grid + paleta extraída del logo. Decisión a tomar: filtro por `metadata.studio` con LIKE (rápido, frágil con "Lucasfilm" vs "Lucasfilm Ltd.") vs tabla normalizada `studios(id, name, logo_url)` (limpio pero migración + scanner update). Hacer `<StudioMark>` clicable.
> - **Filtros de /movies y /series son client-side over loaded pages**. Funciona en libs <100 items; en libs grandes el género/year/rating solo filtra lo cargado, y el search en URL `?q=` solo busca en lo cargado (sentinel se desmonta con search activo). Fix correcto: pasar filtros y sort al server vía `useInfiniteItems` params, y cuando hay `?q=` llamar a `/items/search` en vez de filtrar local.
> - **OMDb provider** (opcional, lower priority): único camino para tener nota IMDb numérica. Nice-to-have. RT no es viable sin pagar.
>
> **Esta sesión NO ha tocado**: nada en `internal/federation/`, `internal/iptv/`, `internal/auth/`, `internal/stream/`. Todo está en items handler / provider / DB queries / frontend hero+player+settings. Los gaps de tests senior-review (P1-F2 LiveTV split, P1-F3 SidebarNavLink) siguen sin tocar pero no surgen como dolor en este flujo.

> 🧭 **Sesión 2026-05-05 tarde (rama `claude/quirky-archimedes-62902a`, read-only review)** — **Senior review consolidado + punch list documentado para próximas sesiones**. Cero código tocado esta sesión: 3 auditores en paralelo (backend Go, frontend React/TS, tests+observabilidad+seguridad) entregaron findings verificados contra código. **Resultado completo en [`docs/memory/audit-2026-05-05.md`](audit-2026-05-05.md)**.
>
> **Veredicto**: backend 8/10 (mejoró desde 7.5), frontend 6.5/10 (sin cambio). Cero regresiones desde audit 2026-04-28. Métricas verificadas: 196 .go producción / 124 _test.go (~63% vs 55% previo) · 50 tests frontend / 186 source (~27% vs 15% previo).
>
> **5 P0 nuevos (hardening pragmático, <3h cada uno)**:
> - **P0-1** `/health` devuelve 200 con DB caída — split en `/health/live` + `/health/ready` con 503 en ping fail
> - **P0-2** `RefreshToken` sin rate limit (solo `Login` lo tiene)
> - **P0-3** `auth.Setup` (gate "no users yet") sin test directo del 403 con count>=1
> - **P0-4** `CleanupOldPrograms` y `PruneAuditBefore` existen sin caller — daily ticker pendiente
> - **P0-5** `useUserDataSync` abre 3 EventSource al mismo `/me/events` — singleton EventBus pendiente (los propios comments lo pedían)
>
> **P1 estratégico** (high leverage refactor):
> - **B3 / P1-B1** `ItemHandler.Get` 207 LOC + 7 repos → service orchestrator + DTO tipado (mata el peor `map[string]any` de 387 ocurrencias)
> - **F4 / P1-F1** extraer `useHlsLifecycle` compartido (useHls / useLiveHls divergen, ~150 LOC dup con drift sutil — el fix `a061267` solo tocó VOD)
> - **B2 / P1-B2** split `internal/federation/manager.go` 1166 LOC por concern (mecánico)
> - **B4 / P1-B3** `scanner.New()` 13 params → Repos struct
> - **waitForShutdown / P1-B4** 14 params → runtime struct
> - **F2 / P1-F2** split `LiveTV.tsx` 557 LOC + tests
> - **F5 / P1-F3** Sidebar `NavRow` reutilizable (markup duplicado en `PeerLibrariesSection`)
> - **F8 / P1-F4** `useUserDataSync` invalida queries que pueden no existir
>
> **Plan de ejecución en 3 sesiones discretas** (ver §7 del audit):
> - **Sesión 1** (3-4h, single PR): los 5 P0 → cierra hardening pre-producción
> - **Sesión 2** (4-5h): B3 + DTO tipado + F4 useHlsLifecycle → mayor leverage downstream
> - **Sesión 3** (3-4h): B2 federation split + B4/waitForShutdown structs + F2/F5 → mecánico bajo riesgo
>
> **Hallazgos del review previo verificados**: B1 ✅ cerrado (commit `d7fc395`), B5 ✅ cerrado (`5313d11`), F1 ✅ cerrado (`a061267`), F9 ✅ cerrado (en/es i18n parity verificada). Resto del backlog B2/B3/B4/B6/B7/B8/B9/B10/B11/F2-F10 sigue abierto, ahora con plan de ejecución y prioridad pragmática.
>
> **Riesgo principal no mitigado**: 5 pages frontend god/críticas con acciones irreversibles sin tests (LiveTV, FederationAdmin, Settings, LibraryNewPage, SystemStatus, UsersAdmin, LibrariesAdmin). Las sesiones 2/3 incluyen escribir tests del área que se toca para no avanzar sin red.
>
> **Esta sesión NO tocó código** — solo `docs/memory/audit-2026-05-05.md` (nuevo) + esta entrada en `project-status.md`. Sigue el árbol limpio.

> 🛡️ **Sesión 2026-05-05 (rama `claude/compassionate-bardeen-76afea`, PR pendiente)** — **Refresh estético de peer-detail + senior review de mantenibilidad + 3 P1 cerrados (B5/B1/F1)**. 4 commits sobre la rama.
>
> **Commit `ff8f0d6`** — *ui(peers): peer item detail reuses HeroSection*. La página de detalle de items federados venía con un layout 2-col plano (póster pequeño + texto a la derecha) que rompía la paridad visual con el detail local cinematográfico. Refactor para que **`PeerItemDetail` reuse `<HeroSection>`** vía `federationItemToMediaItem` + aurora canvas page-wide con paleta extraída en runtime del póster (mismo look que un movie local). El wire de federación es estrecho (id/type/title/year/overview/poster_url) pero `HeroSection` degrada bien: sin backdrop cae al poster_url, sin logo cae al `<h1>`, sin géneros/rating los chips simplemente no se renderizan. Cambios mínimos a `HeroSection`: nuevo `playLabel?: string` opcional (defaults `t("common.play")`) para surfacear "Reanudar 0:58" cuando hay progress cross-peer guardado, y favorito condicional a que se pase `onToggleFavorite` (peer items no tienen favoritos cross-server). Atribución del peer en el slot `studio` + pill emerald-dotted "Compartida por X" debajo del hero. Resume: primary CTA = Reanudar, "Reproducir desde el inicio" en kebab.
>
> **Senior review consolidado** — 2 agentes en paralelo (backend Go + frontend React/TS) tras leer audit-2026-04-15 + audit-2026-04-28 para no duplicar findings. Verdict: backend **7.5/10**, frontend **6.5/10**. Top hallazgos:
> - **B1** middleware bypassing AppError envelope (audit-04-15 §2.2 abierto 3 semanas)
> - **B2** `internal/federation/manager.go` god file 1166 LOC, 8 áreas disjuntas
> - **B3** `ItemHandler.Get` 207 LOC orquestando 7 repos (handler haciendo trabajo de service)
> - **B4** `internal/scanner/scanner.go` 1193 LOC + `New()` con 13 params posicionales
> - **B5** dead `// backwards compat` fields en progress.go (audit-04-15 §3 abierto)
> - **F1** `useHls` con deps `[]` + eslint-disable → audio stuck del episodio anterior
> - **F2** `LiveTV.tsx` god page 557 LOC
> - **F3** `SeriesHero.tsx` mezcla hero + IO observer + sessionStorage + URL builders (699 LOC)
> - **F4** `useHls`/`useLiveHls` duplican lifecycle pero discrepan en disciplina
> - **F5** `Sidebar.tsx` markup `NavLink` copy-pasteado entre `NavRow` y `PeerLibrariesSection`
>
> **Commit `5313d11`** — *api(progress): drop dead backwards-compat id aliases*. Cierra **B5**. 3 líneas `// backwards compat` borradas en Continue Watching / Favorites / NextUp (`item_id` x2, `episode_id` x1). Grep frontend confirmó 0 consumidores. Test de NextUp asertando `id` canónico.
>
> **Commit `d7fc395`** — *api(errors): centralize AppError envelope writer*. Cierra **B1**. Nuevo paquete `internal/api/apperror/` con `Write` + `SetRecorder` — depende solo de `domain` + `chi/middleware` (sin import cycle, ambos `handlers` y `auth` lo consumen). `handlers/responses.go` reduce a thin wrappers (errorPayload struct + errorRecorder var borrados, centralizados). `auth/middleware.go` los 3 `http.Error` con JSON hardcoded → `apperror.Write` con `domain.NewUnauthorized/NewForbidden/AppError{Code:TOKEN_INVALID}`. `api/csrf.go` el `http.Error` de CSRF_FAILED → `apperror.Write`; **`generateCSRFToken` ya no panic-ea** dentro de un handler HTTP (un fallo de hardware-RNG no debería tumbar el server) — retorna `error`, el caller renderiza 500 vía `apperror`. Resultado: UNAUTHORIZED/TOKEN_INVALID/FORBIDDEN/CSRF_FAILED ahora aparecen en `hubplay_errors_total` con `request_id` correlation.
>
> **Commit `a061267`** — *player(useHls): re-attach when source changes*. Cierra **F1**. El effect corría con `deps=[]` + `eslint-disable-next-line react-hooks/exhaustive-deps` — un parent que cambiase `masterPlaylistUrl`/`directUrl`/`playbackMethod`/`sessionToken` dejaba el player con la fuente vieja. Síntoma más visible: `currentAudioTrack` quedaba con el índice del episodio anterior cuando next-up avanzaba. Patrón de `useLiveHls`: real deps `[videoRef, masterPlaylistUrl, directUrl, playbackMethod, sessionToken]` + reset de estado source-bound al inicio del effect + tear-down defensivo (`hls.destroy()` + `video.removeAttribute("src") + video.load()`) cubriendo strict-mode double-mount y la transición direct_play → transcode + `startPosition` en ref para no re-attach cuando el padre recompute resume seconds.
>
> **Verificación**: `tsc -b` EXIT 0 · `vitest run` 384/384 · `go test ./internal/api/... ./internal/auth/... ./internal/domain/...` ok · build + vet limpio. Pre-existing failures NO causados por esta sesión (verificados con `git stash`): `TestStreamHandler_Segment_HappyPath`, `TestPreflight_HappyPath` (ffmpeg env), `TestSQLC_GeneratedFilesMatchQueries`, `pathmap`, `scanner` — Windows env stuff que el audit-04-28 ya documentaba.
>
> **Cola P1 senior-review pendiente** (priorizada por impacto/esfuerzo restantes):
> - **B3** `ItemHandler.Get` → service orchestrator (2-3h, el refactor más rentable: testabilidad + DTO tipado, reduce el `map[string]any` count)
> - **B2** split de `federation/manager.go` por concern (1-2h, file-only refactor, habilita Phase 6 limpio)
> - **F2/F3/F6/F7** split de god-pages frontend (LiveTV, SeriesHero/HeroTrailer, LibraryNewPage, FederationAdmin) — paralelizables, 6-8h totales
> - **B4** scanner Repos struct + 3 ficheros (3-4h)
> - **B6/B8** servicios → AppError + handlers → DTOs tipados (gradual, semana de trabajo)
>
> **Cola P2/P3 senior-review** (ver review consolidado guardado en transcripción de esta sesión, no en docs aún): F4 hlsLifecycle compartido, F5 SidebarNavLink, F8 useUserDataSync optimistic write, F10 metaParts namespace, B6 service AppError migration, B7 Dependencies struct grouping, B9 me_home tests, B10 transmux split, B11 sqlc raw-SQL drift (33 vs 5 documented), F9 Settings i18n drift.

> 🎬 **Sesión 2026-05-04 / 05 (rama `claude/federation-progress-cross-peer`, mergeada a `main`)** — **Continue Watching cross-peer cerrado + bug de config arreglado + smoke real con dos peers paireados verificado end-to-end con vídeo y browser**. 3 commits sobre la rama (Continue Watching, config fix, memory handoff). El gap UX más grande post-Phase-5 cerrado.
>
> **Commit `05c6b4a`** — *federation: cross-peer Continue Watching*. Items federados nunca viven en `items` local, así que `user_data` no puede guardar su posición. Surface dedicado `federation_progress`:
> - **Migración 028** — tabla `federation_progress(user_id, peer_id, remote_item_id, position_ticks, duration_ticks, completed, last_played_at, updated_at)` + índice `(user_id, last_played_at DESC)`. Cascada en revoke de peer (`ON DELETE CASCADE`) y en delete de user.
> - **sqlc** — 4 queries (`UpsertFederationProgress`, `GetFederationProgress`, `DeleteFederationProgress`, `ListFederationContinueWatching`). El upsert preserva un `duration_ticks` previo no-cero cuando el caller pasa 0 (duración aprende del manifest tras varios segmentos; primer save típicamente llega antes). El JOIN del rail filtra por `peer.status='paired'` para que **revoke quite filas del rail sin purga explícita**, y por `<90%` (mismo gate que el local Continue Watching).
> - **Repo + Manager**: `FederationRepository.UpsertProgress/GetProgress/DeleteProgress/ListContinueWatching` thin wrappers. `Manager.RecordProgress` valida que el peer esté `paired` y dropea silenciosamente writes contra peers revocados (admin puede revocar mientras el user reproduce; no rompemos al user). `Manager.GetProgress` y `Manager.ListContinueWatching` añadidos a la `Repo` interface.
> - **3 endpoints HTTP** documentados en `openapi.yaml` (drift test pasa):
>   - `GET  /api/v1/me/peers/{peerID}/items/{itemId}/progress` → zero default cuando no hay row
>   - `POST /api/v1/me/peers/{peerID}/items/{itemId}/progress` → upsert (`{position_ticks, duration_ticks?, completed?}`)
>   - `GET  /api/v1/me/peers/continue-watching?limit=20` → cross-peer rail
> - **Frontend**: `useProgressReporter` acepta `peerId?` opcional → enruta a `/me/peers/.../progress` (con `duration_ticks` desde `video.duration` para que el rail compute el porcentaje); mantiene `keepalive: true` en unmount. `VideoPlayer` propaga `peerId` al reporter. **`PeerItemDetail`** carga progreso al montar, ofrece **Resume {{tiempo}}** + **Play from start** cuando hay posición guardada (<90%), pasa `startPosition` al player, refresca progreso al cerrar. Nuevo `PeerContinueWatchingRail` (auto-hide cuando vacío) wireado en Home **antes** del `PeerRecentRail`. i18n `home.peerContinueWatching`, `peers.resume`, `peers.playFromStart` (en + es).
> - **Tests**: `TestFederationRepository_Progress` (upsert + duration preservation con segundo POST `duration=0` + completed-drop + near-complete-drop + delete) y `TestFederationRepository_Progress_PeerRevokedDropsFromRail`. Stub `inMemoryFedRepo` en `middleware_test.go` extendido.
>
> **Commit `013550c`** — *config: apply env overrides when hubplay.yaml is missing*. Bug expuesto por el smoke con dos containers Docker: `config.Load` retornaba defaults temprano cuando no existía `hubplay.yaml`, **saltando `applyEnvOverrides`**. Resultado: `HUBPLAY_SERVER_BASE_URL` (y todos los demás `HUBPLAY_*`) se ignoraban en silencio en deployments fresh-install. La federación pairing fallaba contra el SSRF guard porque cada peer auto-derivaba su `advertised_url` del Host header del navegador del admin (`localhost:8198`) en vez de la URL Docker-network. **Fix**: colapsadas las dos ramas en un único path `applyEnvOverrides + Validate`. La lógica especial "DB junto al yaml" se queda solo en la rama "no-file" donde tiene sentido. Tests: `TestLoad_NonexistentFile` reescrito (`t.TempDir()` en vez de `/nonexistent/path/`, mejor reflejo del caso producción) + nuevo `TestLoad_EnvOverrides_NoFile` que pinea el bug específico.
>
> **Verificación end-to-end real con vídeo (smoke nunca hecho antes — heredado del proyecto)**:
> - **2 containers Docker** en red bridge compartida con `Juego de Ladrones (2018).mkv` (14 GB) montado en peer A
> - Pareados via API + via UI admin (segunda pasada)
> - Library share + browse cross-peer ✅
> - **Cross-peer stream session** con `strategy: Transcode` ✅
> - HLS master `/me/peers/.../master.m3u8` proxiado A→B ✅ (4 variants: 1080p/720p/480p/360p)
> - Quality playlist `/720p/index.m3u8` ✅ (dispara ffmpeg en peer A)
> - **`segment00000.ts` real descargado** — 3.0 MB de MPEG transport stream cruzando A→origen→our-server→cliente en 24 ms ✅
> - POST progress @ 50% → GET retorna `position+duration+last_played_at` ✅
> - Continue Watching rail `data:[{title:"Juego de Ladrones (2018)", percentage:50, peer_name:"HubPlay Server"}]` ✅
> - Edge cases: duration preservation (0 no clobberea), completed=true drop, near-complete (>=90%) auto-drop, **revoke peer drops from rail** (filas DB intactas, JOIN gates en `status='paired'`), POST contra revoked peer → 204 silent no-op ✅
>
> **Smoke con browser real** (Chrome MCP, dos pestañas en `localhost:8198` y `localhost:8199`):
> - Setup wizard A + B vía form (admin + libraries + ajustes + finalizar) ✅
> - Login + scan automático ✅ (2 movies ingestados, durations correctas: 140 min / 132 min)
> - Pair via UI: invite generation + Probe (fingerprint + palabras de confirmación visibles) + paste invite + tick checkbox + Emparejar → "Emparejado" ✅
> - Share library (vía API; el checkbox UI funciona — el "no firing" del smoke previo era ruido del driver de test, ver Findings abajo)
> - **Click Reproducir en B** → video real renderizando en `<video>` ✅ (escena del planeta visto desde el espacio, timestamp `0:04 / 0:09` → `0:35 / 3:48` → `0:58 / 17:18` mientras el manifest crece)
> - Progress save automático cada 10s vía `useProgressReporter` ✅ (`position_ticks=568292160, percentage=14.26%` en `federation_progress`)
> - Home rail "Sigue viendo en tus pares" ✅ con poster + barra de progreso + badge "HubPlay Server"
> - Detail page muestra **"▶ Reanudar 0:56" + "Reproducir desde el inicio"** ✅
> - **Click Reanudar** → video arranca exactamente en **0:58** (escena del casino HUSTLER, NO desde el principio) ✅
>
> **Findings — bugs que NO eran bugs**: durante el smoke pareció haber dos issues UI (1) checkbox "Compartida" no disparaba mutation (2) botón "Generando..." stuck. **Re-investigado en clean run y NINGUNO se reproduce**: (1) era yo usando `cb.click()` desde JS sobre input controlado de React — el click pixel-real desde Chrome MCP sí dispara `POST /shares` 201. (2) era una race con el JWT TTL de 15 min expirado tras mucho rato dándole a la UI; en flow normal el botón vuelve a "Generar nuevo invite" en <1s. **Cero código que arreglar**.
>
> **Verificación final**: `go test -race ./... -count=1` → todos los paquetes verdes · `vitest run` 384/384 · `tsc --noEmit` limpio · production build limpio · `make sqlc-verify` limpio · container `hubplay-dev` arrancado, migración 028 aplicada.
>
> **Pendientes que quedan en federación user-facing** (cola al final, sin cambios):
> - **Subtítulos federados** (~2h) — el master.m3u8 federado no proxia tracks de subs.
> - **`MaxConcurrentStreams` por peer** — hoy comparten cap global con locales.
> - **Promover `peer_recent` + `peer_continue_watching` a `HomeSection` configurables** — hoy viven fuera del layout-driven dispatch.
> - **Phase 6 Live TV peering** — sin empezar.
> - **Phase 7 Download to local + audit log UI** — sin empezar.
>
> **El smoke con dos servers paireados ya NO está pendiente**: hecho con vídeo real + browser real, end-to-end. La memoria de proyecto puede tachar ese item de la cola.

> 🌐 **Sesión 2026-05-04 (rama `claude/federated-search-frontend-6rbQm`)** — **Federación user-facing: search frontend + dropdown federado + Recently-Added rail**. 2 commits limpios sobre la rama, ambos pusheados.
>
> **Commit `5059191`** — *federated search wireado al `/search`*. El backend de fan-out ya estaba live desde la sesión anterior pero la página nunca lo consumía. Tapado el gap de wire (`SharedItem.LibraryID` plumb end-to-end por SQL FTS5 → client decoder → `peerSearchHitWire`) para que el click navegue a la ruta `/peers/{peer}/libraries/{lib}/items/{id}` ya registrada. Frontend: `FederationSearchHit` types, `api.searchPeers`, `usePeersSearch` hook + queryKey, sección "From your peers" en `SearchResultsView` con `PeerResultCard` (badge de peer sobre el póster). i18n `search.peerSection` + `search.fromPeer` (en + es). OpenAPI spec drift fix. Tests: backend `go test ./... -count=1` verde · frontend 384/384 · tsc + build limpios.
>
> **Commit `888c20c`** — *peer hits en topbar dropdown + "Recently added on peers" home rail*. Backend nuevo end-to-end: sqlc query `ListRecentSharedItems` (JOIN con `federation_library_shares` para ACL, `ORDER BY i.added_at DESC`, selecciona `library_id`) · origen expone `GET /peer/recent` bajo `RequirePeerJWT` · `Manager.FetchPeerRecent` + `RecentFromAllPeers` (mismo timeout/skip-error pattern que el search) · consumidor expone `GET /me/peers/recent` (mismo `peerSearchHitWire`) · OpenAPI + drift allowlist actualizados · test `TestRecentFromAllPeers_AggregatesAndAttributes`. Frontend: `usePeerRecent` (60s staleTime) · `PeerRecentRail` con `PosterCard` + `cornerBadge` "Shared by Pedro" · wired bajo las layout-driven sections en `Home.tsx`, **auto-oculta cuando no hay hits** (deployment solitario ve home idéntico al pre-federación). `SearchBar` topbar ahora dispara `usePeersSearch` mientras escribes — local hits aparecen al instante, peer hits joinan como sección sin volver el panel a spinner; click cierra el dropdown y limpia query.
>
> **Verificación**: `go test ./... -count=1` → todo verde (incluye `internal/federation`, `internal/api`, `internal/api/handlers`, `internal/db`) · web `vitest run` 384/384 · `tsc --noEmit` limpio · production build limpio · sin nuevos errores de lint en archivos tocados.
>
> **Diferido a próxima sesión** (multi-hour, scope grande): `federation_progress` table + Continue Watching cross-peer. Migración 028 + sqlc + endpoint `PUT /me/peers/{peerID}/progress/{remoteItemId}` + integración en `VideoPlayer` para distinguir item federado (hoy llama a `updateProgress` local que solo soporta items locales) + endpoint `GET /me/peers/continue-watching` o extensión del local con flag `is_remote` + adapter en el rail Continue Watching existente. Sin esto, un user que ve un movie federado pierde la posición.
>
> **También pendientes** (cola priorizada al final del archivo): subtítulos federados (~2h), Phase 6 Live TV peering (sin empezar), smoke real con dos servers paireados (manual, nunca hecho), `MaxConcurrentStreams` por peer, promover `peer_recent` a `HomeSection` configurable.

> 🛡️ **Sesión 2026-05-04 (rama `claude/review-federation-implementation-BZyli`, PR abierta)** — **Senior review de federación + 3 slices**. Tras review exhaustivo cerrados 4 P1 declarados (sweeper HLS sin wirar, `IssuePeerToken` con `context.Background()`, sin retry/backoff en client peer, JWT no se refresca mid-stream), añadido cap+LRU al nonce cache, sanitizer de body en `decodeRemoteError`, observabilidad Prometheus completa (5 collectors federation), **migración `federation_repository.go` a sqlc cerrando ADR-001** (760 LOC raw → 480 LOC tipado), y **federated search con fan-out backend completo** (origin `GET /peer/search` + manager `SearchAllPeers` con timeout 2s/peer + consumer `GET /me/peers/search` + 11 tests). 4 commits sobre la rama. Backend `go test -count=1 ./...` → todo verde. Frontend de search deferred (no hay `pnpm` instalable en el entorno).

> 🎯 **Sesión 2026-05-04 (rama `claude/plex-federation-implementation-HUycl`, mergeada a `main`)** — **Phase 5 Slice 2 cerrada end-to-end Plex-style**. Federación P2P es ahora una feature completa visible-y-jugable en la UI: el peer aparece en el sidebar como navegación de primer nivel, `PeerLibraryItemsPage` usa el mismo `PosterCard` + `MediaGrid` que las locales con badge "shared by X" inline, nueva `PeerItemDetail` con Play que llama a `POST /me/peers/{peerID}/stream/{itemId}/session` y reproduce con el `VideoPlayer` existente, **posters proxiados** por origen (nuevo `GET /api/v1/peer/items/{itemId}/poster` server-to-server + `GET /api/v1/me/peers/{peerID}/items/{itemId}/poster` consumer), migración 027 añade `has_poster` al cache de federación, **tests de handler** (404 sin `can_play`, 404 sin share en poster). Backend verde · frontend 384/384 · tsc + build limpios. Phase 5 entera cerrada.

> 🎬 **Sesión 2026-05-04 (rama `claude/prepare-tv-app-fb0cj`, mergeada a `main`)** — 4 commits sobre main. Plan: dejar el proyecto pulido antes del Kotlin TV nativo, foco en cerrar features visibles medio-hechas. **Federación Phase 5 backend cerrado** (slice 1 de 2; falta frontend). **sqlc desbloqueado para siempre** (era la mina del repo). **OpenAPI ampliado a Live TV + Home + meta** (24 paths nuevos al spec con drift test en CI).

> ✅ **sqlc desbloqueado para siempre** (sesión 2026-05-04). `make sqlc` regenera limpio con la versión pineada (1.31.1); `make sqlc-verify` falla CI si el árbol commiteado deriva. Drift test en `internal/db/sqlc_drift_test.go`. Los bugs del parser que motivaron el lockdown están documentados en `conventions.md` con patrones SQL a evitar.

> Snapshot: **2026-04-30 mañana (continuada) — rama `claude/openapi-spec` con OpenAPI 3.0.3 spec embed-y-servida**. 1 commit sobre `main`. Cierra el último item P1 pre-Kotlin TV. **Cola P0+P1 vacía**: todos los prerequisitos para empezar la app Kotlin Android TV están completos.
>
> Estado prev (2026-04-30 mañana, ya en main vía PR #124): 3 fixes federación + device-code login (RFC 8628) end-to-end. Cierra 3 findings P0 + 1 P1.
>
> Estado prev² (2026-04-29 noche): Federación P2P entera (Phases 1-4 + plug-and-play + UX) — 8 commits.
>
> **tests al cierre: backend verde salvo `internal/config` preflight (env-only, ffmpeg no en PATH local; CI verde) · frontend 364/364 · tsc clean**.

## 🛡️ Sesión 2026-05-04 — Senior review + hardening (rama `claude/review-federation-implementation-BZyli`)

> **HANDOFF — leer al inicio de la siguiente sesión.** El usuario pidió "revisa lo que queda y como va para hacerlo como senior perfecto". Tras un review estructurado con findings priorizados (P1/P2/P3), la sesión despachó **3 slices** sobre la rama de review. La rama tiene **4 commits limpios** y **PR abierta** (ver al final).

### Slice A — P1 hardening (commit `ea54cb1`)

Cuatro fixes que un senior con experiencia en streaming pillaría en code review:

1. **Sweeper de sesiones HLS no se ejecutaba**. `Manager.SweepStreamSessions` existía pero NADIE llamaba al ticker. Sesiones idle vivían forever. Wireado en `NewManager` con `time.NewTicker(streamSweepInterval=1min)` controlado por context, parado en `Close` con `<-sweepDone`.
2. **`IssuePeerToken` usaba `context.Background()`** rompiendo la cancelación. Cambiada signatura a `IssuePeerToken(ctx, audiencePeerID)`. 4 callsites en `client.go` + handshake roundtrip test actualizados.
3. **Sin retry+backoff en client peer**. Nuevo helper `doIdempotentPeerGET` con 3 intentos, exp backoff 250ms→500ms→1s, retry en transport error y 5xx, no retry en 4xx, honra ctx cancellation. Aplicado a `FetchPeerLibraries`/`FetchPeerItems`.
4. **JWT expira mid-stream**. `ProxyPeerStreamRequest` ahora hace one-shot refresh: si la primera respuesta es 401/403, drena, mintea JWT fresco, reintenta. La sesión del peer está keyed por `session_id` (no por JWT) → fresh token resume cleanly. **Manager.Close()** ahora cierra idle conns del transport para que SIGTERM no espere `HTTPTimeout`.

Tests añadidos (`stream_test.go`, `client_retry_test.go`):
- `TestSweepStreamSessions_DropsExpired`/`LookupBumpsLastSeen` (mock clock).
- `TestManager_CloseStopsSweeperGoroutine` (no leak across 25 cycles).
- `TestManager_CloseIdempotent`.
- 7 tests de retry: retries-on-5xx, gives-up-after-max, no-retry-on-4xx, ctx-bails-fast, JWT-refresh-on-401, no-retry-on-200, no-double-refresh.

### Slice B p1 — Bounded + observable (commit `8b029f0`)

1. **Nonce cache cap + soonest-expiry eviction**. Defensa contra peer paireado-pero-hostil que mintea tokens con `exp = far-future` para pinear entradas. Cap a `nonceCacheMaxEntries=10_000`; en overflow drop `nonceCacheEvictBatch=2500` entradas con menor expiry. Amortised O(1) por insert; sort O(n log n) solo cada batch.
2. **Sanitizer de body en `decodeRemoteError`**. Antes embeddaba 4 KiB raw en el error chain. Ahora `sanitiseRemoteBody` cap 256 bytes, control chars → `.`, marcador `<N bytes total>` cuando trunca.
3. **Prometheus federation observability** (5 collectors):
   - `hubplay_federation_paired_peers` (GaugeFunc)
   - `hubplay_federation_peer_stream_sessions` (GaugeFunc)
   - `hubplay_federation_nonce_cache_size` (GaugeFunc)
   - `hubplay_federation_handshake_duration_seconds` (Histogram, direction × outcome)
   - `hubplay_federation_outbound_requests_total` (Counter, kind × outcome)
   Wireado vía sink pattern: `internal/federation/metrics.go` define `MetricsSink` + accessors `PairedPeers()/PeerStreamSessions()/NonceCacheSize()`. `internal/observability/federation.go` provee `FederationSink` (counter+histogram) + `RegisterFederationGauges` (gauges). Manager instrumentado en `AcceptInvite`/`HandleInboundHandshake` (handshake duration, named-returns para deferred capture) + counters en client (`OutboundRequest(kind, outcome)`).

Tests: `TestNonceCache_EvictsOldestOnOverflow`, `TestSanitiseRemoteBody_*`, `TestRegisterFederationGauges_ReportsLiveValues`, `TestFederationSink_RecordsCounterAndHistogram`, etc.

### Slice B p2 — sqlc migration (commit `80162f4`)

**Cierra ADR-001 explícitamente**. `federation_repository.go` era el último archivo del repo con raw SQL (760 LOC). El bloqueo de sqlc se resolvió en `dc80538` (sesión previa); esta sesión hace la migración:

- Nuevo `internal/db/queries/federation.sql` con 22 named queries (identity, invites, peers, audit log, library shares, item cache).
- `sqlc generate` produce `internal/db/sqlc/federation.sql.go`.
- `federation_repository.go` reescrito como adapter thin: 760 → 480 LOC. Mismo public API; cero cambios en callers.
- **Nota**: `UpsertCachedItems` mantiene su transacción explícita (DELETE + N×INSERT atómico) usando `q.WithTx(tx)`.
- **Nota**: `UpdatePeerRevoked` usa `:execrows` para preservar `domain.ErrPeerNotFound` en fila no encontrada.
- **Nota**: `MAX(cached_at)` se tipa `interface{}` por sqlc (NULL-able aggregate); coerce defensivo a `time.Time`.
- Drift test `TestSQLC_GeneratedFilesMatchQueries` verde.

### Slice C — Federated search backend (commit `ec4f385`)

User-facing federated search end-to-end backend. Sin esto, "Inception" solo daba resultados locales aunque un peer paireado lo tuviese.

- **Repo** (`SearchSharedItems`): SQL crudo con `items_fts MATCH ?` + JOIN `federation_library_shares` como gate ACL. Documentado en `federation.sql` que la query queda raw porque sqlc 1.31 no parsea FTS5 virtual tables (mismo precedente que `item_repository.go::List`).
- **Cliente** (`FetchPeerSearch` en `client.go`): reutiliza `doIdempotentPeerGET` → mismo retry+backoff. Imports `net/url` aliased como `neturl`.
- **Manager** (`SearchAllPeers`): fan-out paralelo a peers paireados, timeout 2s/peer (separate `context.WithTimeout` por goroutine), agregación con atribución (`SharedItemFromPeer{Peer, Item}`), peer caído = log `Info` + skip (no blanquea).
- **Origin handler** (`FederationPublicHandler.SearchLibraries`, `GET /peer/search`): bajo `RequirePeerJWT`, 400 si `q` vacío, returns `{items, total}` (mismo shape que ListLibraryItems).
- **Consumer handler** (`MePeersHandler.SearchPeers`, `GET /me/peers/search`): bajo user auth, emite `{hits: [{peer_id, peer_name, id, type, title, year, overview, poster_url}]}` con `poster_url` sintetizado same-origin via `/api/v1/me/peers/{peerID}/items/{itemID}/poster`.
- **Router**: 1 ruta consumer + 1 ruta origin.
- **OpenAPI**: spec actualizado con `/me/peers/search`; `/peer/search` añadida al allowlist p2p del drift test.

Tests (11 nuevos, todos verdes):
- Repo: `TestFederationRepository_SearchSharedItems` con DB real + FTS5 — verifica ACL gate (peer-Z sin share → 0 hits), happy path (peer-A con share → 1 hit del library compartido, ignora `lib-private` con título matching), empty query, no match.
- Manager: `TestSearchAllPeers_AggregatesAndAttributesByPeer`, `_SkipsErroringPeer` (5xx en peer no blanquea), `_HonoursPerPeerTimeout` (slow peer cortado por wallclock), `_EmptyQueryShortCircuits`. Plus `TestFetchPeerSearch_DecodesItemsResponse` y `_NotPaired`.
- Handler: `TestFederationSearch_NoShare_ReturnsZeroHits` (ACL gate HTTP-layer), `_WithShare_ReturnsMatchingItems`, `_EmptyQuery_Returns400`, `_NoToken_Returns401`.

### Estado al cierre

- 4 commits limpios sobre `claude/review-federation-implementation-BZyli`, pusheados.
- `go test -count=1 ./...` → todos los paquetes verdes (`internal/api`, `handlers`, `db`, `federation`, `observability`, etc.).
- Drift tests (OpenAPI, sqlc) verdes.
- Frontend NO tocado esta sesión (no hay `pnpm` instalable en el entorno).
- PR abierta: `feat: federation senior review — 4 slices (hardening, sqlc, observability, search)`.

### Decisiones técnicas relevantes

1. **Retry policy diferenciada por método HTTP**. Idempotent GETs (libraries, items, search) → 3 attempts con exp backoff. POST `StartPeerStreamSession` → no retry (side effect: spawn transcode session). Proxy stream → one-shot refresh on 401/403 (defensive, key rotación / clock skew).
2. **Per-peer timeout en search, no global**. Un peer lento NO debe arrastrar la respuesta del usuario; un global timeout daría timeout cuando un peer lento está casi listo. Goroutine per-peer + `context.WithTimeout` per-call.
3. **Per-peer limit en search, no global**. Peer chatty no crowdea quieter peers fuera del result set. UI ve fairness.
4. **GaugeFunc, no Gauge.Set()**. Cero drift risk: scrapes leen estado in-memory live. Three accessors (`PairedPeers`, `PeerStreamSessions`, `NonceCacheSize`) en Manager; observability usa interfaz local (`FederationStatsSource`) con int methods → arrow observability → federation, nunca al revés.
5. **Sanitizer en decodeRemoteError, no en logger**. La sanitización ocurre en el momento de wrap, no en el logger downstream. Garantiza que cualquier error chain (Sentry, slog handler future, audit log) ve el body acotado.
6. **Search via FTS5 con raw SQL**. sqlc 1.31 no soporta FTS5 virtual tables; documentado como excepción (mismo precedente que `item_repository.go`).
7. **Slice C frontend deferred, no parcheado**. El entorno no tiene `pnpm`; en lugar de hacer cambios sin poder validar, dejo el frontend para sesión con env funcional. Wire shape ya documentado en OpenAPI.

### Lo que NO está hecho (pendiente real)

**Frontend de federated search** (~1-2h en sesión con `pnpm`):
- Página `/search` ya existe local. Añadir consumo de `GET /api/v1/me/peers/search?q=...` paralelo al search local; renderizar hits con `PosterCard` + badge de origen (mismo patrón que `PeerLibraryItemsPage`); click → `/peers/{peerId}/libraries/{libraryId}/items/{id}` (ruta ya registrada).
- i18n keys: `search.fromPeer`, `search.peerSection`.
- Adapter: el `peerSearchHitWire` del backend mappea trivialmente a `MediaItem` vía `federationItemToMediaItem` ya existente.

**`federation_progress` table** (~3h, backend + frontend):
- Migración 028 con tabla `federation_progress(user_id, peer_id, remote_item_id, position_seconds, duration_seconds, is_completed, last_played_at)`.
- Queries sqlc (`UpsertFederationProgress`, `GetFederationProgress`, `ListFederationContinueWatching`).
- Handler `POST/GET /api/v1/me/peers/{peerID}/progress/{itemID}`.
- Wireado en `PeerItemDetail` para resume (carga al abrir, save al pause/seek).
- Sin esto, un user que ve un movie federado pierde la posición.

**Home rail "Más en tus servidores conectados"** (~½ día):
- Backend: `GET /api/v1/me/home/peer-latest` que pulle items recientes de cada peer paireado (fan-out paralelo, similar a search).
- Frontend: nueva sección en Home component, mismo `PosterCard` + badge.

**Slice 2.4 (cuando aparezcan casos reales)**:
- `MaxConcurrentStreams` por peer (hoy comparten cap global con locales — slice 1 declaró pendiente).
- Subtítulos federados.
- Filtros origin chip en lists ("Local · Pedro · Maria").

**Phase 6 — Live TV peering**: completamente intocada. Sección 9.5 de `federation.md`.

**Phase 7 — Download to local**: completamente intocada.

### Smoke manual pendiente (heredado, sigue sin hacerse)

Aún NO se ha probado el botón Play end-to-end con dos servers reales paireados. `docker-compose.federation-test.yml` existe en el repo pero el smoke sigue siendo trabajo manual no hecho. La próxima sesión que toque federación podría empezar por ahí.

---

## 🎯 Sesión 2026-05-04 — Phase 5 Slice 2 (rama `claude/plex-federation-implementation-HUycl`)

> **HANDOFF — leer al inicio de la siguiente sesión.** La conversación previa había cerrado el backend de Phase 5 (slice 1, commit `bbafd56`). El usuario pidió "haz la federación bien implementada estilo Plex". Esta sesión cierra el círculo: peer-as-library en el sidebar + grid unificado + Play funcional + posters proxiados + tests de los gaps de seguridad declarados.

### Contexto al arranque

Punto de partida: backend de remote streaming funcionaba (slice 1) pero el usuario nunca había podido probar Play end-to-end porque NO había frontend wirado. El catálogo del peer se renderizaba con un `ItemCard` custom poster-less, no había sidebar para peers, no había detail page para items federados, y no había proxy de posters (la decisión Plex-style estaba tomada pero sin implementar — ver entrada previa "Decisión clave de UX que QUEDÓ TOMADA pero NO IMPLEMENTADA").

### Commits sobre la rama

`209aa53` `federation(phase-5): Plex-style remote browse + play (slice 2)` — un commit gordo que cierra el slice. 25 ficheros, ~1370 LOC.

### Backend

**Modelo de datos**:
- `SharedItem.HasPoster bool` añadido (wire field `has_poster`, omitempty cuando false). Propagado por:
  - `internal/db/federation_repository.go::ListSharedItems`: SQL extendido con `EXISTS(SELECT 1 FROM images WHERE item_id=i.id AND type='primary' AND is_primary=1) AS has_poster`. Subconsulta, no JOIN — el listing path no necesita el image id, solo el flag.
  - `UpsertCachedItems` / `ListCachedItems`: cache de federación lee/escribe la columna nueva.
  - `client.go::remoteSharedItem` decodifica el flag de la respuesta del peer.
- **Migración 027** `federation_cache_has_poster.sql`: ALTER TABLE federation_item_cache ADD COLUMN has_poster BOOLEAN NOT NULL DEFAULT 0. Default 0 para filas existentes — el siguiente refresh las repuebla. Down recrea la tabla sin la columna (SQLite legacy DROP COLUMN, aceptable para dev rollback).

**Origin side (peer B)**:
- Nuevo handler `internal/api/handlers/federation_image.go::ItemPoster` para `GET /api/v1/peer/items/{itemId}/poster` bajo `RequirePeerJWT`.
- ACL pipeline (cada paso conflata a 404 para no leakear existencia):
  1. `items.GetByID` — 404 si no existe.
  2. `mgr.GetLibraryShare(peer.ID, item.LibraryID)` — 404 si no hay share o `!CanBrowse`.
  3. `images.GetPrimaryURLs([itemID])` — 404 si el item no tiene primary.
  4. Delega a `ImageHandler.ServeImageByID(w, r, imageID)` para reuso de pathmap + thumb cache + ETag.
- `ImageHandler.ServeFile` refactorizado en dos: `ServeFile` lee chi.URLParam, `ServeImageByID(w, r, imageID)` hace el trabajo. La federación usa el segundo; el local usa el primero. Mismo path-mapping, mismo thumb cache, mismo ETag — el peer obtiene exactamente la misma imagen que el local.
- Wiring en `router.go`: el `ImageHandler` y el `imageDir` se construyen al inicio del router (antes del grupo `Auth.Middleware`), de modo que el grupo de federación (que NO usa la auth de usuario, solo `RequirePeerJWT`) y el grupo local comparten una sola instancia del handler. Cero duplicación de pathmap.

**Consumer side (peer A, user-facing)**:
- Nuevo handler `internal/api/handlers/me_peer_image.go::ProxyPeerItemPoster` para `GET /api/v1/me/peers/{peerID}/items/{itemId}/poster` bajo auth de usuario.
- Llama a `mgr.ProxyPeerItemPoster(ctx, peerID, itemID)` (nuevo método en `client.go`, helper sobre `ProxyPeerStreamRequest`).
- Forwardea `Content-Type`, `Cache-Control`, `ETag`, `Content-Length` desde el peer. Same-origin: el `<img src>` del browser solo habla con nuestro origen.
- `BrowsePeerItems` en `me_peers.go` ahora sintetiza `poster_url` per-item solo cuando `it.HasPoster`. Wire shape `peerItemWire` añadida con el campo. Items sin poster no llevan el campo (la card cae al placeholder de letra).

**OpenAPI**:
- Documenta `/me/peers/{peerID}/items/{itemId}/poster` con responses 200/304/404/502 (image/jpeg, image/webp, image/png, ErrorEnvelope).
- Drift test allowlist: `GET /peer/items/{itemId}/poster` añadida como p2p-only.

**Tests de handler integración** (`internal/api/handlers/federation_stream_test.go`, NUEVO archivo, 305 LOC):
- Usa `testutil.NewTestDB` (SQLite real con todas las migraciones) + `db.NewFederationRepository` + `federation.NewManager`. Inserta peer + library + item directamente vía SQL.
- Helper `fedTestEnv` construye el setup completo: manager, peer paireado con keypair conocido (para mintar JWTs como si el peer nos llamase), librería, item, share configurable, fakeStreamManager, router con `RequirePeerJWT`.
- `Manager.RefreshPeerCache` exportada (era unexported `refreshPeerCache`) para que el test pueda forzar refresco de caché tras inserción directa de peer. Producción nunca llama a esto; pasa por ProbePeer/AcceptInvite/RevokePeer que ya refrescan.
- Tests:
  - `TestFederationStream_StartSession_NoCanPlay_Returns404`: el gap declarado en project-status previo. Share con `CanBrowse=true, CanPlay=false` → 404 con `code=ITEM_NOT_FOUND`. Verifica conflación deliberada (no 403).
  - `TestFederationStream_StartSession_CanPlay_Returns200`: complemento positivo. Share con CanPlay=true → 200 con `session_id` + `master_path` con prefijo correcto.
  - `TestFederationImage_ItemPoster_NoCanBrowse_Returns404`: el gate del poster. Sin share → 404 (no 403, no 200 con bytes).

### Frontend (Plex-style)

**Reuso de componentes locales**:
- `PosterCard` ahora acepta `href?: string` (override del default `/movies/{id}` o `/series/{id}`) y `cornerBadge?: ReactNode` (badge en bottom-left del poster). Cero cambios visuales para el caller local; los federados pasan estos props.
- `MediaGrid` reenvía `hrefFor?: (item) => string` y `cornerBadgeFor?: (item) => ReactNode` al PosterCard. Defaults inert.
- **Adapter `federationItemToMediaItem`** (`web/src/api/federationAdapter.ts`): convierte `FederationRemoteItem` (slim) en `MediaItem` (canonical) para que el grid lo trate como uno más. Campos sin equivalente (genres, ratings, blurhash, etc.) salen como vacíos / null. Type narrowing al union `MediaType`; unknown types caen a "movie" defensivamente.

**Páginas**:
- `PeerLibraryItemsPage`: reescrita. Antes usaba un `ItemCard` custom poster-less con grid grid de 3-col. Ahora usa `MediaGrid` con `hrefFor: (item) => '/peers/{peerId}/libraries/{libraryId}/items/{item.id}'` y `cornerBadgeFor: () => <span>{peerName}</span>`. Resultado visual idéntico a /movies pero con badge de peer.
- **`PeerItemDetail` NUEVA** (`web/src/pages/PeerItemDetail.tsx`, 200 LOC): ruta `/peers/:peerId/libraries/:libraryId/items/:itemId`. Detail page mínima — solo poster + título + año + overview + Play (la wire shape de federación es slim por diseño; cast/ratings/chapters viven en el peer y no se pullean en v1). El Play llama a `api.startPeerStreamSession(peerID, itemID)` → recibe `master_playlist_url` same-origin → mete en `VideoPlayer` existente con `playbackMethod` derivado del `strategy`.
- Item lookup: `useMemo` que busca el item en cualquier página cacheada de `usePeerItems` (vía `queryClient.getQueriesData`). Fallback: fetch página 0 de la librería. Si tampoco aparece, EmptyState "Item not available". Funciona para deep-links porque el queryclient es persistente en sesión.

**Sidebar Plex-style**:
- `web/src/components/layout/Sidebar.tsx`: añadida sección "Connected servers" después del MAIN group, antes del divider que precede a PERSONAL. Powered por `useAllPeerLibraries`.
- Agrupación por peer client-side (Map con orden de primera aparición). Cada peer tiene su sub-header con el nombre, y debajo una row por librería compartida.
- Icono por content_type: movies → `Film`, series → `Tv`, livetv → `Radio`. Default `Film`.
- Active highlight comparte el `layoutId` con el resto del sidebar (animación spring continuada al cambiar de sección). Estilos consistentes con `NavRow`.
- Cuando no hay peers paireados o ninguno tiene libraries compartidas, la sección no se renderiza — sidebar idéntico al pre-federación.

**API client + tipos**:
- `client.ts::startPeerStreamSession(peerID, itemID)`: nuevo método.
- `types.ts`:
  - `FederationRemoteItem.poster_url?: string` añadido (lo emite el backend cuando `has_poster`).
  - `PeerStreamSessionResponse` interface (post-unwrap del envelope `{"data": ...}`).

**i18n**:
- `nav.peerServers` (en + es): "Connected servers" / "Servidores conectados".
- `peers.play`, `peers.playFailed`, `peers.backToLibrary`, `peers.itemNotFoundTitle`, `peers.itemNotFoundDescription` en ambos idiomas.

**Routing**:
- `App.tsx`: nueva lazy route `peers/:peerId/libraries/:libraryId/items/:itemId` → `PeerItemDetail`. La existente `/peers/:peerId/libraries/:libraryId` sigue como `PeerLibraryItemsPage`.

### Estado al cierre

- Tests al cierre:
  - Backend `go test ./...` → todo verde (federación, db, api/handlers, openapi drift).
  - Frontend `pnpm test --run` → 384/384 (eran 364, +20 nuevos? no, los 384 incluyen tests nuevos del player; este slice no añade tests frontend específicos).
  - `pnpm exec tsc -b --noEmit` → sin errores.
  - `pnpm build` → bundle generado limpio.
- Working tree limpio en `claude/plex-federation-implementation-HUycl`.
- 1 commit (`209aa53`) sobre el commit base de slice 1.

### Decisiones técnicas relevantes

1. **Posters proxiados, no signed-URL**. Considerado emitir URLs firmadas que el browser pudiese cargar directamente del peer. Rechazado:
   - El user no debe ver IPs de peers (privacy).
   - CORS y CSP serían pesadillas (el browser tendría que aceptar `connect-src` arbitrario por peer).
   - Permite re-verificar `CanBrowse` en cada fetch (un peer que pierda el share deja de servir bytes inmediatamente).
   - Cost: doble hop bytewise. Aceptable para v1 — los posters son <100KB y se cachean cliente-side via ETag.

2. **`HasPoster` flag en lugar de `PosterURL` directa**. El cliente sintetiza la URL del proxy a partir de (peerID, itemID), no la recibe del peer. Razones:
   - Si el peer nos diese su URL directa, podríamos inferir su hostname accidentalmente.
   - Se evita un solo formato de URL en el wire que después tendríamos que migrar.
   - El flag es 1 bit, la URL serían ~100 bytes por item.

3. **`ImageHandler.ServeImageByID` extracted from `ServeFile`**. La federación necesitaba el byte-serving sin chi.URLParam dependency. Refactor mínimo: `ServeFile` ahora es `ServeImageByID(chi.URLParam("id"))`. Federación llama directo. Cero duplicación, mismo cache.

4. **`SharedItem` lleva `HasPoster`, NO `PrimaryImageID`**. Considerado exponer el image_id directamente. Rechazado: leakea la estructura interna del peer (los image_ids son UUIDs que mañana podrían cambiar de schema). El flag binario es estable.

5. **`PeerItemDetail` minimalista, no fetch de detail**. Considerado añadir `GET /api/v1/peer/items/{itemId}/detail` con cast/ratings/chapters. Rechazado para v1: la wire shape actual es deliberadamente slim (Sección 7 de federation.md), el detail "rico" viene cuando el usuario quiera *jugar* el item, no para *navegar*. Add-it-when-you-need-it.

### Lo que NO está hecho aún (pendiente real para próximas sesiones)

**Slice 2.3 (alto valor, ~1 día)**:
- **Federated search con fan-out**. `/items/search` actual no hace fan-out; un usuario buscando "Inception" solo ve resultados locales aunque el peer lo tenga. Implementar `GET /api/v1/me/peers/search?q=...` con fan-out paralelo (timeout 1-2s por peer, errores tolerantes). Página Search reusa los results con badge de peer.
- **Home rail "Más en tus servidores conectados"**. Backend: `/api/v1/me/home/peer-latest` que pulle items recientes de cada peer paireado. Frontend: nueva tarjeta en Home con esos items, mismo poster-card-pattern.

**Slice 2.4 (cuando aparezcan casos reales)**:
- Filtros origin chip en lists ("Local · Pedro · Maria") para mezclar/separar.
- `MaxConcurrentStreams` por peer (Phase 5 v1 las federation streams compiten contra `MaxReencodeSessions` global con las locales).
- `federation_progress` table — cross-peer watch state (slice 1 lo dejó pendiente). Hoy un user que ve un movie federado no recupera la posición la próxima vez.
- Subtítulos federados.

**Phase 6 — Live TV peering**: completamente intocada. Sección 9.5 de federation.md.

**Phase 7 — Download to local**: completamente intocada.

### Smoke manual pendiente

El usuario aún NO ha probado el botón Play end-to-end con dos servers reales paireados. Los tests de wire están verdes (slice 1 incluyó round-trip httptest, slice 2 añade integration tests del handler), pero un docker-compose con dos servers y un media file real sigue siendo trabajo manual no hecho. La próxima sesión que toque federación podría empezar por ahí (`docker-compose.federation-test.yml` ya existe en el repo).

### Deuda explícitamente NO abordada esta sesión

- `internal/db/federation_repository.go` (770 LOC raw SQL violando ADR-001) sigue diferida — pero ya **NO está bloqueada** (sqlc se desbloqueó en la sesión anterior). Trabajo manual cuando alguien tenga ganas.
- Bcrypt cost 12 → 13 (auditoría 2026-04-28).
- Tests frontend siguen ~15% cobertura. Páginas y admin sin tests específicos.

---

## 🎬 Sesión 2026-05-04 (rama `claude/prepare-tv-app-fb0cj`) — 4 commits

> **HANDOFF — leer al inicio de la siguiente sesión.** Resumen completo de qué se hizo, qué se decidió, y qué toca después.

### Contexto al arranque

El usuario dijo que NO va a empezar la app Kotlin Android TV todavía. La conversación derivó hacia "deja el HubPlay web pulido antes". Bajaron de la cola: D-pad navigation, Chromecast, app nativa Kotlin (las tres deferred). Subieron: cerrar deuda real (sqlc), cerrar features medio-hechas (federación Phase 5).

**Pregunta que el usuario respondió en sesión**:
- Federación: SÍ tiene peers reales en mente, sigue en cola.
- Multi-versión 4K+1080p: NO ripéa en múltiples calidades, baja a P3.
- Confirmó "como Plex" para la UX de federación (item content mezclado en el sidebar, no aparte).

### Commits sobre main

1. `4a9c8ca` `api(openapi): close the spec for Kotlin TV — Live TV + Home + drift test`
   - 24 paths nuevos en `internal/api/handlers/openapi.yaml` (Live TV consumer, Home rails, /openapi.yaml self).
   - 8 schemas nuevos: Channel, ChannelDetail, EPGProgram, BulkScheduleResponse, HomeLayout, HomeSection, TrendingItem, LiveNowEntry.
   - **Drift test AST-based** en `internal/api/openapi_drift_test.go`: parsea `router.go`, exige que cada ruta esté documentada o en allowlist explícito. Inverso también (no documentación muerta).
   - 65 paths en spec total (antes 50).

2. `153e4de` `db(sqlc): lock down make sqlc until proper migration session`
   - Decisión inicial conservadora (rollback en commit 3): puse un guard en el Makefile para rechazar `make sqlc` con un mensaje claro. Documenté los bugs.
   - Era una solución pragmática mientras el usuario decidía. Le ofrecí "ahora candado / luego cirugía" y pidió "robusto para siempre".

3. `dc80538` `db(sqlc): unlock regen for good — full migration + drift test in CI` ⭐
   - Ejecución completa del playbook de 6 pasos. **Esto es ahora la fuente de verdad para sqlc**.
   - **Bug raíz descubierto**: chars no-ASCII en comentarios SQL (em-dashes, acentos, backticks, comillas tipográficas) desplazan la cuenta de bytes UTF-8 del parser de sqlc. A partir de ahí, queries posteriores salen truncadas en los últimos 1-2 caracteres. Confirmado en sqlc 1.27, 1.29 y 1.31.
   - **Bug raíz #2**: `?` placeholders dentro de `NOT (...)` no se detectan; el campo desaparece del Params struct y `QueryContext` queda con un `?` colgando. Workaround: DeMorgan. `sqlc.arg()` NO rescata el caso (sqlc 1.31 lo deja como literal en el SQL).
   - **Aplicado**: em-dashes/acentos → ASCII en `chapters.sql`, `people.sql`, `user_data.sql`, `channel_watch_history.sql`. ContinueWatching reescrita con DeMorgan + `IS NOT NULL` guard para preservar three-valued semantics.
   - **Type drift propagado** en repos: `image_repository.go` (IsLocked: NullBool → bool), `people_repository.go` (nullableString/Int64 wrappers), `user_data_repository.go` (rename `AbandonedThreshold` → `LastPlayedAt` por sqlc auto-naming, `SeriesID` → `ParentID`).
   - **Makefile**: `SQLC_VERSION=v1.31.1` pineado, target `sqlc-install` idempotente, `sqlc-verify` para CI.
   - **Drift test Go**: `internal/db/sqlc_drift_test.go` regen-against-scratch, falla en CI si el árbol commiteado deriva. Skipea si sqlc no está en PATH (dev local sin tool no se bloquea).
   - **conventions.md**: sección "Regeneración sqlc" reescrita con los bugs documentados + patrones a evitar.
   - El proyecto-status.md previo tenía la versión INVERTIDA ("1.29 es la rota"). Corregido.

4. `bbafd56` `federation(phase-5): remote streaming backend (slice 1 of 2)` ⭐
   - **Cierra el medio-hecho más visible de la federación**. Antes: ves catálogo del peer, le das play, 404. Ahora: backend round-trip funcional.
   - **Origin side (peer B, server-to-server)**:
     - `POST /api/v1/peer/stream/{itemId}/session` (peer JWT): valida share.CanPlay, spawn stream.Manager session con userID = `peer:{peerID}`, registra UUID → (peer, item, profile) en memoria, devuelve session_id + master_path + method.
     - `GET /api/v1/peer/stream/session/{sid}/master.m3u8` con HLS de variantes relativas (`1080p/index.m3u8`).
     - `GET /api/v1/peer/stream/session/{sid}/{quality}/index.m3u8` (transcode manifest).
     - `GET /api/v1/peer/stream/session/{sid}/{quality}/{segment}` (.ts segments).
     - **ACL doble**: cada request re-verifica `peer.ID == session.PeerID` para que un peer no pueda enumerar UUIDs de otros peers.
   - **Consumer side (peer A, user-facing proxy)**:
     - `POST /api/v1/me/peers/{peerID}/stream/{itemId}/session` (user session): forwarda caps del header `X-Hubplay-Client-Capabilities` al peer; devuelve URL same-origin.
     - `GET /api/v1/me/peers/{peerID}/stream/session/{sid}/master.m3u8|{quality}/index.m3u8|{quality}/{segment}`: proxy transparente con peer JWT (server-side only).
     - El user's HLS player solo habla con tu origin; el peer nunca ve su IP.
     - URLs relativas en el manifest → cero rewriting necesario.
   - **Sin tablas DB nuevas**. Sesiones en memoria con TTL 5min idle. `federation_progress` es slice 2.
   - **Reuso del stream.Manager** (no se duplica infra). Sesiones federation compiten con locales por el `MaxReencodeSessions` cap — intencional v1, cap por peer es slice 2.
   - **Tests**:
     - `internal/federation/client_stream_test.go`: round-trip wire con httptest.Server canned. Verifica POST + auth header + JSON body shape + decode.
     - Drift test catch los 4 paths user-facing nuevos → documentados en openapi.yaml; los 4 server-to-server al allowlist.
   - **OpenAPI**: 5 operations añadidas bajo tags `[federation, stream]`.

### Estado al cierre

- 4 commits limpios en `claude/prepare-tv-app-fb0cj`, pusheados a origin.
- `go test -count=1 ./...` → **todo verde**.
- tsc / frontend: NO tocado esta sesión, sigue como estaba (364/364 al cierre previo).
- Working tree limpio.

### Decisión clave de UX que QUEDÓ TOMADA pero NO IMPLEMENTADA: federación al estilo Plex

El usuario preguntó cómo lo hace Plex y dijo "me gusta como Plex entonces". Implicaciones:

- **El servidor del peer aparece en el sidebar como una biblioteca más tuya**, con badge sutil de origen.
- **`/movies` y `/series` siguen siendo locales** (no se mezcla — confunde sin filtros).
- **El peer library tiene su propia entrada nav** (ej. "Movies · Pedro" debajo de las locales), y al click va al MISMO componente de detalle de biblioteca con un badge en el header.
- **Search global** hace fan-out a todos los peers paired con timeout corto y muestra resultados mezclados con badge de origen.
- **Home rails**: nada especial, el badge en la card cubre la atribución de origen.

### 🎯 Slice 2 (siguiente sesión) — frontend Plex-style

**Slice 2.1 (esta sería la próxima sesión, ~2-3h)**: fundación visible.
- **Sidebar**: añadir sección "Servidores conectados" / "Peer Servers" después de MAIN. Cada peer's libraries aparecen como nav rows. Hook `useAllPeerLibraries` ya existe. Click → `/peers/{peerID}/libraries/{libraryID}` (ruta ya registrada en App.tsx).
- **PeerLibraryItemsPage** (existe en `/web/src/pages/PeerLibraryItemsPage.tsx`, 198 LOC): reemplazar el `ItemCard` custom por **el mismo `PosterCard` que usan Movies/Series**. Usar `MediaGrid`. Añadir badge de origen del peer.
- **Adaptador `FederationRemoteItem` → `MediaItem`**: posters siguen sin proxiar (eso es slice 2.2 backend). Por ahora placeholder con dominant color del título (función `getInitialsColor` o similar).
- **i18n**: claves nuevas en `web/src/i18n/locales/en.json` y `es.json` para "nav.peerServers", "peer.sharedBy", etc. Las que ya hay están bajo `peers.*`, mantenlas.

**Slice 2.2 (otra sesión)**: cerrar el círculo.
- **Backend**: extender `FederationRemoteItem` JSON para incluir `poster_url` (proxiado a `/api/v1/me/peers/{peerID}/items/{itemId}/poster`). Otra ruta nueva al openapi.yaml + drift test.
- **Frontend**: ItemDetail page para items federados (puede ser una variante de `pages/ItemDetail.tsx`). Botón Play → llama `POST /me/peers/{peerID}/stream/{itemId}/session`, mete el `master_playlist_url` devuelto en el reproductor HLS existente.
- **Test smoke manual**: docker-compose con dos servers paireados → reproducir end-to-end.

**Slice 2.3 (otra sesión, opcional pero alto valor)**:
- **Federated search**: extender `/items/search` o crear `/me/peers/search` con fan-out paralelo (timeout 1-2s). Página Search reusa los results con badges. ~1 día.
- **Home rail "Más en tus servidores conectados"**: nueva tarjeta en el Home con items recientes de peers. Backend: endpoint `/me/home/peer-latest`. ~½ día.

**Slice 2.4 (cuando aparezcan casos reales)**: polish.
- Filtros origin chip en lists ("Local · Pedro · Maria").
- Cap por peer (MaxConcurrentStreams).
- federation_progress table (cross-peer watch state).
- Subtítulos federados.

### Lo que NO está hecho aún (para que la próxima sesión sepa)

- **El usuario nunca ha probado el botón Play de federación end-to-end** porque no hay frontend wirado todavía (eso es slice 2.2). El backend tiene tests del wire, pero el smoke manual con dos servers reales está pendiente.
- **Tests frontend** siguen como estaban (~15% cobertura). No abordados esta sesión.
- **Seguridad de share.CanPlay**: el handler verifica el flag, pero NO hay test de "peer sin can_play recibe 404 al intentar abrir sesión". Test pendiente.

### Deuda explícitamente NO abordada esta sesión

- `federation_repository.go` (750 LOC raw SQL violando ADR-001) sigue diferida — pero ya **NO está bloqueada por sqlc** (era el bloqueo declarado en el snapshot previo). Lo bloquea solo el trabajo manual.
- Bcrypt cost 12 → 13 (auditoría 2026-04-28).

---

## 📜 Sesiones previas

**Rama `claude/openapi-spec` (mergeada a main vía PR #125)**:
- `[pending-hash]` api(openapi): hand-written 3.0.3 spec embed-y-servida en `/api/v1/openapi.yaml`. Cubre auth + browse + stream + me + federation user surface (consumer-facing); admin/setup/peer-to-peer fuera de v1 deliberadamente.

**Rama `claude/federation-hardening` (mergeada a main vía PR #124)**:
- `b0b0613` federation(security): close JWT replay window with per-nonce cache
- `c6e0d84` federation(security): SSRF gate on peer-controlled AdvertisedURL
- `3b24e92` federation(docs): RevokePeer atomicity contract documented
- `[hash-pending]` auth(device): RFC 8628 device authorization grant — TV-friendly login
- `683c512` auth(device): /link page — operator approves a device code

---

## ⚠️ Deudas operacionales

### sqlc desbloqueado — playbook ejecutado (2026-05-04)

**Estado**: `make sqlc` funciona limpio. `make sqlc-verify` regenera y
falla si hay diff (lo usa CI). `internal/db/sqlc_drift_test.go` lo
ejecuta como test de Go también. Detalles de los bugs del parser y los
patrones SQL a evitar → `conventions.md` sección "Regeneración sqlc".

**Lo que se ejecutó esta sesión** (los 6 pasos del playbook que el
snapshot anterior listaba como "para una sesión futura"):

1. ✅ Sustituidos em-dashes (`—`) y backticks (`` ` ``) por ASCII
   en `chapters.sql`, `people.sql`, `user_data.sql`,
   `channel_watch_history.sql`. Los caracteres no-ASCII en
   comentarios SQL desplazan la cuenta de bytes UTF-8 del parser.
2. ✅ `ContinueWatching` reescrita con DeMorgan para sacar el `?`
   del `NOT (...)`. Guard `IS NOT NULL` añadido para preservar la
   semántica three-valued original (rows con NULL last_played_at
   se siguen excluyendo).
3. ✅ `SQLC_VERSION=v1.31.1` pineado en Makefile. Target
   `sqlc-install` lo instala idempotente con `go install`.
4. ✅ Regen limpia, byte-identical reproducible.
5. ✅ Type drift propagado en adapters:
   - `image_repository.go`: `IsLocked` ahora `bool` puro (no
     `sql.NullBool`), 3 sitios actualizados.
   - `people_repository.go`: `nullableString`/`nullableInt64`
     wrappers en `CreatePerson`, `SetPersonThumbPath`,
     `InsertItemPerson`.
   - `user_data_repository.go`: rename `AbandonedThreshold` →
     `LastPlayedAt` (sqlc auto-naming) en `ContinueWatching` y
     `SeriesID` → `ParentID` en `SeriesEpisodeProgress`.
6. ✅ Drift test reemplaza el guard del Makefile que existía
   temporalmente.

**Lo que esto desbloquea**:
- Cualquier query nueva. Antes estaba bloqueado por miedo a corromper
  el output. Ahora `make sqlc` es seguro.
- Conversión de `internal/db/federation_repository.go` (750 LOC raw
  SQL violando ADR-001) a sqlc, cuando alguien tenga ganas — sigue
  diferida porque es trabajo, no porque haya bloqueo técnico.

### `federation_repository.go` no es sqlc (ADR-001)

Lo documenta el propio fichero ([línea 14-26](../../internal/db/federation_repository.go:14)). Diferida desde Phase 1 como housekeeping. Ya **no** está bloqueada por sqlc — abrir cuando convenga.

---

**Sesión 2026-04-29 noche → 2026-04-30 madrugada (en `main`)** — 8 commits federación:
- `f8a9e3a` + `9967e23` — Phase 1 backend + admin UI (identidad Ed25519, invites, handshake)
- `5a4b05a` + `ea5a141` — Phase 2 (JWT middleware, audit log, ratelimit) + integration test
- `1c67800` — Phase 3 (library shares opt-in)
- `8073d5c` — Phase 4 (browse remoto end-to-end, item cache)
- `29af48e` + `aecfdf0` + `81d6d7c` + `73b749c` — lint CI, plug-and-play AdvertisedURL, fix overview JOIN, grid unificado /peers

---

## 📜 Sesión 2026-04-30 mañana (continuada) — OpenAPI 3.0.3 spec (rama `claude/openapi-spec`)

Sesión corta y enfocada que cierra el último item P1 pre-Kotlin TV. Sin OpenAPI spec versionado, la app Kotlin Android TV iba a redescubrir el wire format por trial-and-error — peligro multiplicado por federación añadiendo 12+ endpoints `/peer/*` extra. Esta rama lo cierra: 1 commit, sin código nuevo de aplicación, solo el contrato.

### Decisiones

**Hand-written, no annotation pass.** Considerada la opción de [`swag`](https://github.com/swaggo/swag) (anotaciones encima de cada handler que generan el spec en build), pero rechazada:
- 74 rutas registradas en el router → ~200 anotaciones (cada operación + cada parámetro + cada response). Coste ~2-3 días de comment churn que no añade safety.
- El comment volume hace los handler files más difíciles de leer.
- swag sigue draft-04 JSON Schema con extensiones propias; menos portable que un YAML/JSON OpenAPI canónico.
- El surface API se versiona en `/api/v1`; estabilidad mayor que una doc auto-generada que cambia con cada handler refactor.

Hand-written con foco en el **consumer surface** es el camino correcto para v1.

**OpenAPI 3.0.3, no 3.1.** Tooling Kotlin (especialmente `openapi-generator`'s plantillas Kotlin) lag detrás de 3.1. El trade-off es JSON Schema draft-04-ish constraints; nada bloqueante para lo que el API expresa.

**Embed via `//go:embed`, no serve-from-disk.** Tres razones:
1. El spec ships con el binario que describe — imposible que drift por un mount mal configurado.
2. Single-binary deploy story (mismo patrón que `web/dist` embed) extiende al contrato del API.
3. Hot-reload del spec no tiene sentido — clients lo cachean igual, y un cambio de spec implica un cambio de código que implica restart.

**Servido en `/api/v1/openapi.yaml`** con ETag (304 honored), Cache-Control 1h, soporte HEAD. No hay `/openapi.json` mirror — el spec es público y el cliente Kotlin / `openapi-generator` aceptan YAML directamente; quien necesite JSON corre `yq -o json` localmente.

**Sin Swagger UI bundleada.** Considerado: añadir `swagger-ui-dist` y servir un `/api/v1/docs`. Rechazado para v1 — añade ~3MB de assets estáticos al binario para una superficie que el target user (developer integrando con el API) consume vía openapi-generator, no via web UI. Si en el futuro hay demanda real, fácil añadir.

### Surface cubierta (v1 del spec)

In scope (consumer-facing):
- **Auth** — login, refresh, logout, RFC 8628 device flow (start/poll/approve)
- **Identity** — `/me`, `/me/preferences` (CRUD)
- **Events** — `/me/events` SSE con tres tipos (`user.progress.updated`, `user.played.toggled`, `user.favorite.toggled`)
- **Browse** — libraries, items (latest, search, detail, children), trickplay
- **Stream** — info, master.m3u8, variants, segments, direct-play, subtitles (embedded + external)
- **User data** — progress (read/write/played/unplayed/favorite), continue-watching, favourites, next-up
- **People** — detail con filmografía, thumbnails
- **Images** — `/images/file/{id}` con `?w=` query param para variants
- **Federation user surface** — `/me/peers/*` para browse de catalogos de peers
- **Health** — liveness probe

Out of scope (intencional, NO TODO):
- `/admin/*` — admin UI es browser-only.
- `/setup/*` — wizard de primera ejecución, web-only.
- `/peer/*` — server-to-server, autenticado por Ed25519 JWTs no por sesiones de usuario; documentado en `docs/architecture/federation.md`.
- IPTV (Live TV) — channel browse en scope cuando la app TV lo pida; el surface admin (transmux/EPG) waits.

### Tests

3 test files al handler del spec:
- `TestOpenAPIHandler_ServesYAML` — Content-Type, ETag, Cache-Control headers correctos.
- `TestOpenAPIHandler_HonoursIfNoneMatch` — re-request con If-None-Match → 304 sin body.
- `TestOpenAPIHandler_HEADSendsHeadersOnly` — HEAD = 200 + Content-Length, sin body.

2 test del spec en sí:
- `TestOpenAPISpec_ParsesAsValidYAML` — gopkg.in/yaml.v3 unmarshal pasa, top-level `openapi: 3.x.x` + `paths` + `components` presentes. Catches a broken spec edit at compile-time-of-tests rather than first-client-fetch.
- `TestOpenAPISpec_CoversCriticalPaths` — sanity-check que las rutas críticas (`/auth/login`, `/auth/refresh`, `/auth/device/start`, `/me`, `/libraries`, `/items/{id}`, `/stream/{itemId}/master.m3u8`, `/me/progress/{itemId}`, `/me/peers`) están en el spec. Si alguien borra una operación crítica por accidente, falla aquí.

Sin dependencia nueva (`gopkg.in/yaml.v3` ya estaba para sqlc.yaml).

### Surface fuera del spec, scoped para sesiones futuras

Si alguna vez la app Kotlin TV necesita más surface:
- IPTV consumer (channels, EPG, schedule, favorites): añadir `tags: [livetv]` con ~10 paths.
- Image upload (clientes que quieren contribuir posters): añadir `/items/{id}/images/{type}/upload`.
- Federation Phase 5+ surfaces (cuando se shipean): remote streaming, live TV peering, download to local.

### Estado al cierre

- 1 commit en `claude/openapi-spec`. Working tree clean.
- Backend tests verde (incluyendo nuevos tests OpenAPI).
- Cola P0+P1 **vacía**.
- Próximo hito estratégico **desbloqueado**: app Kotlin Android TV.

### Lo que la app Kotlin TV puede hacer ahora

Día 1 del proyecto Kotlin:
1. `curl https://hubplay.example.com/api/v1/openapi.yaml -o openapi.yaml`
2. `openapi-generator generate -i openapi.yaml -g kotlin -o ./hubplay-client --additional-properties=library=jvm-okhttp4,serializationLibrary=kotlinx_serialization`
3. Cliente tipado generado: data classes para todos los wire types, métodos para cada operación, OkHttp interceptors para auth.

---

## 🛡️ Sesión 2026-04-30 mañana — federation hardening + device-code login (rama `claude/federation-hardening`)

Sesión doble: cierra los 3 findings P0 detectados en la auditoría de la noche previa + envía un item P1 grande del roadmap pre-Kotlin TV (device-code login). Cinco commits sobre `main`, lista para PR.

### Bloque A — Federación hardening (3 commits)

**`b0b0613` close JWT replay window**

`internal/federation/jwt.go:90-92` documentaba que el cache de nonces vivía en el Manager — pero nunca se implementó. Cualquier peer JWT capturado podía replicarse durante los 5 min de TTL.

- `internal/federation/nonce.go` — `nonceCache` con map `nonce → exp`. Sweep inline en cada check (O(n), n bounded por peers × ratelimit × TTL ≈ 3000 entries en defaults). Mutex única; federation no es hot path comparado con SQLite writes downstream.
- `Manager.CheckAndStoreNonce` retorna false en replay.
- `RequirePeerJWT` lo llama tras `ValidatePeerToken` y antes del ratelimit. Replay → 401 PEER_REPLAY + audit row con `ErrorKind="replay"`.
- `domain.ErrPeerReplay` sentinel.
- Tests: 5 unit + 1 end-to-end (middleware) + actualización de `TestRequirePeerJWT_FiresRateLimitAfterBurst` para emitir tokens frescos (su patrón previo —reusar el mismo JWT 4 veces— ahora correctamente rechazado como replay).

**`c6e0d84` SSRF gate on peer-controlled AdvertisedURL**

`HandleInboundHandshake` persistía `peer.BaseURL = remote.AdvertisedURL` verbatim. Un peer hostil con invite válido podía pairing advertando `BaseURL=http://127.0.0.1:8080`; cualquier `FetchPeerLibraries` del admin nuestro = TCP probe a localhost.

- `internal/federation/url.go::validatePeerURL` — rechaza http(s) ausente, host vacío, y cualquier URL cuyo host (literal IP o resolución DNS) sea loopback / link-local / unspecified / multicast. **No** bloquea RFC1918 — `docker-compose.federation-test.yml` usa Docker bridge 172.x, y federación homelab LAN es legítima en v1.
- Wired tanto en `HandleInboundHandshake` (peer-controlled, threat principal) como en `AcceptInvite` (admin-pasted, defense in depth).
- `domain.ErrPeerURLUnsafe` sentinel.
- Tests: 10 unit + 1 end-to-end (`TestHandleInboundHandshake_RejectsHostileLoopbackAdvertisedURL`).
- Existing `handshake_roundtrip_test.go` (httptest.Server bound a 127.0.0.1) cubierto por nuevo helper `allowLoopbackForTests(t)`.

**`3b24e92` RevokePeer atomicity comment**

Documenta la sub-millisecond window entre `UpdatePeerRevoked` (DB) y `refreshPeerCache` (memoria). Trade-off consciente: cerrar la ventana requeriría holding la cache write lock durante un SQLite roundtrip → blocking todas las JWT validations concurrentes. Comment-only.

### Bloque B — Device-code login (RFC 8628) (2 commits)

Cierra el segundo P1 pre-Kotlin TV. Una TV remote no puede teclear password; el operator va a /link en el móvil, pega el code, aprueba. Mismo flow que Netflix / Spotify / YouTube TV.

**Endpoints**:
- `POST /api/v1/auth/device/start` (no auth) — devuelve `{device_code, user_code, verification_url, expires_in, interval}`.
- `POST /api/v1/auth/device/poll` (no auth) — devuelve JWT pair o protocol error (`authorization_pending` / `slow_down` / `expired_token` / `access_denied`).
- `POST /api/v1/auth/device/approve` (auth gated) — operator (login session distinto) liga su user_id al user_code.

**Backend (commit pre-`683c512`)**:

- Migración 024 — `device_codes(device_code PK, user_code UNIQUE, device_name, user_id NULLABLE, expires_at, approved_at, consumed_at, last_polled_at)`.
- `internal/db/queries/device_codes.sql` + sqlc (ADR-001 respetado).
- `internal/auth/device.go::DeviceCodeService` — orchestrator. Tokens issued via `s.auth.createSession` (la misma máquina de password login: indistinguible del JWT pair regular, mismo `MaxSessionsPerUser` eviction, mismo refresh path).
- User code format: 8 chars de alfabeto sin ambigüedad (excluye 0/O, 1/I/L, 5/S). Display: `ABCD-EFGH`. Canonical (sin dashes, uppercase) on input.
- Slow-down detection: gap entre polls < 4s → `slow_down` error.
- Single-use: `consumed_at` set tras issuance; future polls = `expired_token`.
- Tests: 5 service-level (happy path / slow_down / unknown user_code / unknown device_code / normalisation).

**`683c512` /link page (frontend)**

- `web/src/pages/LinkDevice.tsx` — single form, monospace input grande, client-side canonicalise mirror del backend.
- `web/src/api/hooks/deviceAuth.ts::useApproveDeviceCode` (TanStack mutation).
- Route `/link` behind `ProtectedRoute` (auth-gated).
- `humaniseError` mapea códigos API a strings localizadas.
- i18n: 11 strings bajo `link.*` en en + es.
- Verificación: tsc clean · 364/364 frontend · `pnpm build` produce `LinkDevice-b6gMQCFI.js` chunk OK · live preview blocked en este Windows por ffmpeg env (mismo issue que afecta `internal/config` preflight test).

### Decisiones a recordar

- `DeviceCodeService` en package `auth` (no en `auth/device/` separado) para acceder a `Service.createSession` private. Encapsulación local-only.
- Persistencia DB-backed para device codes — TTL-bounded pero idempotente under restart, y el sweep es trivial.
- No rate limiter dedicado al `/poll` — el token-bucket existente per-IP cubre el degenerate case; `slow_down` cubre el cliente legítimo.
- SSRF filter NO bloquea RFC1918 deliberadamente (docker-compose federation testing y LAN homelab son legítimos). El threat real cerrado es probe-to-loopback. Si en el futuro se necesita stricter, añadir flag config `federation.strict_url_validation` opt-in.

### Estado al cierre

- 5 commits sobre `main` en rama `claude/federation-hardening`. Working tree clean.
- Backend tests verde (federation, auth, db, api). Frontend 364/364.
- Cola P0 vacía. P1 queda **OpenAPI spec versionado** (~½ día) como único item pre-Kotlin TV pendiente.

---

## 🤝 Sesión 2026-04-29 noche → 2026-04-30 madrugada — federación peer-to-peer (Phases 1-4 + extras)

La iniciativa federación, anunciada por mucho tiempo en `docs/architecture/federation.md` (15 secciones, 854 líneas), aterriza en código. Diseño de Sección 13 ("Implementation reuse map") cumple su promesa: el lift es **integración** sobre primitivos existentes (keystore, JWT/EdDSA, event bus, ratelimit token bucket, `imaging.SafeGet`, `stream.Manager`) en lugar de reescritura paralela. La rama `main` recibe 8 commits en orden cronológico estricto — cada fase merge a `main` antes de empezar la siguiente, con tests verde como gate.

### Phase 1 — Identity + Pairing (commits `f8a9e3a`, `9967e23`)

**Backend** (`f8a9e3a`, +2466/-2):
- `internal/federation/identity.go` — `IdentityStore` carga/genera el keypair Ed25519 del servidor en migración 020. La privkey vive en `server_identity.private_key` (BLOB encrypted-at-rest TBD; v1 cleartext igual que las JWT signing keys originales antes del keystore). Fingerprint hex-grouped (`a8f3:k2m9:x4p1:c7e2`) + BIP-39 wordlist (`fingerprint_words.go`, 256 palabras) para confirmación voice-friendly tipo SSH.
- `internal/federation/invite.go` — invites de 24h con código `hp-invite-<10-char-base32>`. `ValidateCodeFormat` + `CanonicalCode` (lowercase, dashes-collapsed) tolera typos del admin pegándolo desde un chat. `IsUsable` ata expiry + `accepted_at` no nulo.
- `internal/federation/jwt.go` — `IssuePeerToken` y `ValidatePeerToken`. EdDSA explícito (no algorithm-confusion), aud=peer-uuid, iss=our-uuid, exp 5min, nbf con skew ±1min. `PeerLookup` interface para cortar el ciclo de tests sin importar Manager. **El campo `Nonce` se emite pero no se valida — ver "Bugs detectados" al final.**
- `internal/federation/manager.go` — orquestador. `ProbePeer`, `AcceptInvite` (outbound: nosotros recibimos invite del remoto), `HandleInboundHandshake` (inbound: el remoto pegó nuestro invite), `RevokePeer`, `LookupByServerUUID` (cache in-memory de paired peers, refrescado en cada mutación).
- `internal/db/federation_repository.go` — repo plano (no sqlc todavía — siguió la urgencia de ship antes que la pureza ADR-001). 5 ops invite + 6 ops peer + identity helpers.
- Migración 020 — `server_identity` (singleton row) + `federation_peers` + `federation_invites`. UNIQUE en `(server_uuid)` y `(code)`. ON DELETE CASCADE para `accepted_by`.
- Endpoints: `GET /api/v1/federation/info` (público, no auth), `POST /api/v1/federation/invites` (admin), `POST /api/v1/admin/federation/probe`, `POST /api/v1/admin/federation/accept`, `GET /api/v1/admin/federation/peers`, `DELETE /api/v1/admin/federation/peers/{id}`, `POST /api/v1/peer/handshake` (inbound, no peer JWT — el code es la autenticación).
- `cmd/hubplay/main.go` — wired al boot phase. `Manager.Close()` en graceful shutdown para flush del audit queue.

**Admin UI** (`9967e23`):
- `web/src/pages/admin/FederationAdmin.tsx` — pestaña en /admin con tres flujos:
  1. *Identity card* — fingerprint hex + words copy-to-clipboard, descubrible para confirmación out-of-band.
  2. *Invite generation* — botón "Crear invitación", muestra el código grande para pegar en chat.
  3. *Add server* — input de URL + invite code; pasa por probe → confirma fingerprint en modal → accept → peer aparece en lista con badge `paired`.

**Tests Phase 1**: identity (4), invite (5), jwt (8), manager partial (TODO Phase 2 tras tener middleware). 17 nuevos en `internal/federation/`.

### Phase 2 — JWT middleware + audit + ratelimit (commit `5a4b05a` + integration test `ea5a141`)

**`5a4b05a`** (+1255/-22):
- `internal/federation/middleware.go` — `RequirePeerJWT` middleware chi-style. Pipeline: extract Bearer → `ValidatePeerToken` → ratelimit gate → wrap responsewriter para capturar status+bytes → handler → audit. Falla mapea a 401/403/429 con códigos legibles (`PEER_AUTH_REQUIRED`, `PEER_KEY_MISMATCH`, `PEER_TOKEN_EXPIRED`, `PEER_REVOKED`, `PEER_RATE_LIMITED`, `PEER_UNKNOWN`). `normaliseEndpoint` reemplaza UUID-shaped segments por `:id` para que el audit no explote en cardinality.
- `internal/federation/audit.go` — `Auditor` con goroutine background + canal de 256 entradas. `Record` no bloqueante (drop-on-full + log throttled cada 5s). `flush` cada 30s o cuando llega el `done`. Persistencia 1-row-at-a-time (acepta el coste para no perder un batch entero por un único error SQLite). Lifecycle ata a `Manager.Close`.
- `internal/federation/ratelimit.go` — `RateLimiter` token-bucket per-peer. Doble-checked locking en `bucketFor`, mutex per-peer para arithmetic. `Reset` en revoke para que un re-pairing arranque limpio.
- Migración 021 — `federation_audit_log` (id INTEGER PK AUTOINCREMENT + idx peer/time + idx endpoint/time) + `federation_rate_limit_state` (per-peer tokens persisted; no se usa todavía — la ratelimit es 100% en memoria, persistencia es Phase 2.5+).
- `GET /api/v1/peer/ping` — primer endpoint protegido por la cadena nueva, sirve de smoke test.

**`ea5a141`** integration test:
- `internal/federation/handshake_roundtrip_test.go` — instancia dos Managers en proceso, hace pairing real (probe → invite → accept handshake), luego peer A emite JWT a peer B y B valida + sirve `/peer/ping`. Verifica que la cadena entera está enchufada: identity → JWT → middleware → audit row.

**Tests Phase 2**: middleware (8), ratelimit (5), roundtrip (1) + audit cubierto via roundtrip. 14 más.

### Phase 3 — Library shares (commit `1c67800`)

**Backend** (+1240/-31, half frontend):
- `internal/federation/share.go` + `share_test.go` — `LibraryShare` domain type, `ShareScopes` (CanBrowse/CanPlay/CanDownload/CanLiveTV). `Manager.ShareLibrary` (UPSERT idempotente — re-shareing con scopes nuevos no requiere unshare manual), `UnshareLibrary`, `ListSharesByPeer`, `ListSharedLibrariesForPeer`, `ListSharedItems`. **Decisión Plex-style**: si el peer pide items de una library no compartida, devolver `ErrPeerNotFound` (404 → `PEER_UNKNOWN`), **no 403**. No-existence-leak — un peer no puede enumerar IDs de libraries que no le compartimos.
- Migración 022 — `federation_library_shares(peer_id, library_id, can_browse, can_play, can_download, can_livetv, extra_scopes JSON, created_by, created_at)` con UNIQUE(peer_id, library_id). El JOIN sobre la repo se hace server-side para que filtros sean unbypaseables a nivel SQL.
- Endpoints inbound: `GET /api/v1/peer/libraries` (lista con scopes), `GET /api/v1/peer/libraries/{id}/items?offset&limit` (paginated, gated por share + CanBrowse).
- Endpoints admin: `POST /api/v1/admin/federation/peers/{id}/shares`, `DELETE /api/v1/admin/federation/peers/{id}/shares/{shareID}`, `GET /api/v1/admin/federation/peers/{id}/shares`.

**Admin UI**:
- `FederationAdmin.tsx` extendido — cada peer card ahora expande lista de libraries + checkboxes de scopes inline. Save publishes `federation.share_added` event; el admin sees actualización inmediata.
- i18n en/es para los strings nuevos.

**Tests Phase 3**: share (10) + middleware extendidos para 403 path (3). 13 más.

### Phase 4 — Remote browsing UI (commit `8073d5c`)

**Backend** (+1143/-2):
- `internal/federation/client.go` — `FetchPeerLibraries` y `FetchPeerItems` outbound. JWT freshly issued per request via `IssuePeerToken`. `decodeRemoteError` mapea el envelope `{error: {code, message}}` del peer a `fmt.Errorf` legible.
- `internal/federation/manager.go` extendido con `BrowsePeerLibraries` (live-only — libraries son lista corta, no cachear) y `BrowsePeerItems` (read-through cache: si cache fresh < 1h serve cache, si stale o vacío → live, fallback a stale cache si live falla).
- `PurgeCache` para el botón "force refresh" del admin.
- Migración 023 — `federation_item_cache(peer_id, remote_id, type, title, year, overview, poster_url, parent_remote_id, cached_at)` + idx peer.
- Endpoints user-facing: `GET /api/v1/me/peers/libraries` (todos los peers, lista plana), `GET /api/v1/me/peers/{peerID}/libraries`, `GET /api/v1/me/peers/{peerID}/libraries/{libraryID}/items`, `GET /api/v1/me/peers` (lista de peers paired).

**Frontend** — 5 páginas nuevas:
- `PeersPage` — landing /peers, lista de peers paired con badges status.
- `PeerLibrariesPage` — /peers/{peerID}, libraries del peer en grid.
- `PeerLibraryItemsPage` — /peers/{peerID}/libraries/{libraryID}, paginated browse.
- Sidebar entry "Servidores conectados" cuando hay peers ≥ 1.
- Hooks: `useMyPeers`, `usePeerLibraries`, `usePeerLibraryItems`, `useUnifiedPeerLibraries`.

### Extras post-Phase 4 (4 commits de 30 min cada uno)

**`29af48e` lint fixes** — CI verde tras las 4 fases. Cosas como `errcheck` en image_test, refactor de `client.go` para que el shape de `decodeRemoteError` no triggere `lll`, golangci-lint config pequeñas. -28 LOC sin cambiar funcionalidad.

**`aecfdf0` plug-and-play AdvertisedURL** — el admin no debería tener que setear `HUBPLAY_SERVER_BASE_URL` antes de pairing. `internal/api/handlers/federation_url.go::deriveURLFromRequest` infiere de X-Forwarded-Proto/Host (con fallback a r.Host + r.TLS) y lo pasa como `fallbackAdvertisedURL` a `Manager.AcceptInvite`. **Trade-off documentado in-line**: X-Forwarded-* no se valida porque la auth real es la firma Ed25519. Worst case de spoofing es "advertise URL incorrecta → peer no nos puede llegar después" → falla ruidosamente, no compromiso silencioso. `docker-compose.federation-test.yml` añadido como setup de dos servidores en local para probar el flujo end-to-end sin un proxy real.

**`81d6d7c` overview en shared items** — bug-fix mínimo: `ListSharedItems` no estaba haciendo JOIN a `metadata` para sacar `overview` → la card en el peer remoto mostraba título + año pero overview vacío. JOIN añadido. 8 LOC, ningún test añadido (visible al ojo en la UI).

**`73b749c` unified library grid** — UX final de Phase 4. `BrowseAllPeerLibraries` fan-out paralelo a todos los peers paired (un goroutine cada uno, channel collector); errors per-peer se loguean pero no rompen el view. La PeersPage ahora es UNA grid unificada con la library de cada peer marcada con badge del peer; click navega al item view del peer correcto. Resultado: el usuario ve "Lo Bueno de Pedro" como vería sus propias libraries, sin click extra para "entrar al servidor de Pedro".

### Estado al cierre

- `main` 2026-04-30 02:57, working tree clean, sincronizado con origin.
- Backend `go test -count=1 ./...`: verde salvo `internal/config` (TestPreflight_HappyPath / TestPreflight_CacheDirGetsCreated fallan en este Windows porque ffmpeg/ffprobe no están en PATH — env, no regresión; CI tiene la dep instalada).
- Frontend: 46 ficheros, 364 tests, todos verde. tsc clean.
- Migraciones: 19 → 23 (4 nuevas). Sin breaking changes — DBs viejas migran sin tocar tablas existentes.
- Federación es **opcional**. Sin invites generados ni peers paired, todo el surface peer-to-peer es lazy (no goroutines, no cache, no audit writes). Operadores que no la usen no notan nada.

### 🚨 Bugs detectados en auditoría posterior (sesión actual)

Tras el merge a `main`, esta sesión hace un peer-review crítico del código federación. Tres findings reales, dos falsos positivos descartados.

**1. `🚨` Replay protection no implementada (`internal/federation/jwt.go:90-92`)**

El JWT incluye un campo `Nonce` (uuid fresh per token) y el comentario explícitamente dice *"Replay protection is the caller's responsibility — the per-nonce cache lives in the Manager so it can scope nonces by peer + window"*. Pero **el cache de nonces no existe**. `grep -r 'nonce' internal/federation/` solo devuelve la generación + el campo struct; ninguna validación. `RequirePeerJWT` no llama a ningún `seenNonce`/`checkReplay`.

Impacto real: un atacante que capture **un** JWT válido (TLS termination en un proxy comprometido, observabilidad mal configurada, SSRF intermedia) puede replicarlo durante los 5 min de TTL. Eso significa N consultas a `/peer/libraries/{id}/items` con un solo token capturado, defeating per-peer ratelimit refill window.

Defensa primaria sigue intacta (Ed25519 signature verifica que el token vino del peer; un atacante MITM no puede *forjar* un nuevo token). Lo que cae es la defensa secundaria (un token capturado y replicado).

**Fix sugerido**: nonce cache LRU en `Manager` (capacidad ~10k entries × 5min TTL ≈ trivial), check en middleware después de validate y antes de servir. ~50 LOC + 3 tests.

**2. `⚠️` SSRF parcial via peer-controlled BaseURL (`internal/federation/manager.go:441-450`)**

`HandleInboundHandshake` persiste `peer.BaseURL = remote.AdvertisedURL` directamente desde el wire del peer, sin validación más allá de `joinBaseURL`'s check de scheme http(s) (manager.go:784-796). Un peer hostil que consiga un invite válido (que se le filtró a un atacante) puede hacer pairing advertando `BaseURL=http://127.0.0.1:8080` o `http://192.168.1.1`. Cualquier futuro `FetchPeerLibraries`/`FetchPeerItems` (admin browsing su catálogo) hace HTTP outbound a esa URL.

Impacto **acotado**: la respuesta tiene que pasar `decodeRemoteError`/JSON decode. Localhost no va a tener Ed25519 signing, así que no puede pretender ser el peer real — pero el TCP connect probe **sí** ocurre (timing visible: ¿está el servicio interno arriba? ¿qué status devuelve?). En egress logs se ve tráfico desde HubPlay hacia IPs internas no-públicas. SSRF probe real, no exfil.

**Fix sugerido**: validate `AdvertisedURL` en `HandleInboundHandshake` — rechazar `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `::1`. Reusar la lista de `internal/imaging/safe_get.go::SafeGet` que ya tiene este filtro. Validate también que el TLD no sea `.local`/`.localhost`/`.internal` salvo opt-in explícito por config. ~40 LOC + 5 tests. Para Tailscale legítimo (ts.net) la URL es pública DNS-resolved.

**3. `💡` RevokePeer TOCTOU micro-window (`internal/federation/manager.go:727-745`)**

`UpdatePeerRevoked` (DB) seguido de `refreshPeerCache` (cache). Entre los dos, una request en flight que ya pasó `LookupByServerUUID` sigue. Window real: sub-millisegundo en SQLite local, micro-segundos. Cualquier request iniciada después de que el cache se refresca falla con `PEER_REVOKED`.

No es un bug explotable — un atacante no puede forzar un timing tan apretado, y tras la ventana el rechazo es inmediato. Vale la pena un comentario en `RevokePeer` explicando la atomicidad esperable, no más.

**Falsos positivos descartados (para no resolver fantasmas la próxima sesión)**:
- ❌ "Cache survives unshare" — `UnshareLibrary` afecta libraries OUTBOUND (las que YO comparto con peer X). El `federation_item_cache` es items INBOUND (los que YO descargué de peer X). Diferentes superficies, sin relación.
- ❌ "Audit drops on backpressure" — by design, documentado, log throttled. Federation hammered hard ya está rate-limited; perder algunos audits durante el spike es preferible a crear backpressure en el hot path.
- ❌ "Auditor.persist context leak" — `defer cancel()` está en línea 172. El reviewer lo perdió.

---

## 🌐 Sesión 2026-04-29 tarde (PRs #122 + #123 a `main`) — 3 commits foundation pre-Kotlin TV

Sesión doble que cierra **dos items P1 grandes** del roadmap "preparar la API
para que la consuma una app nativa Android TV". Hasta hoy `Decide()` asumía
"el cliente es un navegador web con codecs típicos" y el progreso de
reproducción no se sincronizaba entre dispositivos. Ambos asuntos quedan
cerrados.

### Commit 1 — `5a37ed2` `stream(caps)`: capability negotiation server-side

`stream.Decide()` deja de hard-codear codecs y acepta un `*Capabilities`
parseado del header `X-Hubplay-Client-Capabilities` (formato semicolon
separated key=value-list, mirror de `Accept-CH`/`Vary`):

```
video=h264,h265,vp9,av1; audio=aac,opus,eac3; container=mp4,mkv
```

Reglas clave:
- Tokens lower-cased y trimmed; keys desconocidas se ignoran (forward-compat).
- Segmentos malformados se descartan silenciosamente — un typo no envenena el resto.
- Header ausente → `DefaultWebCapabilities` (= comportamiento legacy exacto).
- Declaración parcial → `effectiveCapabilities` rellena con defaults.

Resultado real: una Chromecast que decodifica EAC3 nativamente deja de
recibir AAC stereo downmixed cuando el wire ya soportaba 5.1.

Tests: 15 backend (parse, backfill, decisión nil/declarada/parcial,
DirectPlay/DirectStream/Transcode). Bonus mientras estaba: fix de
`TestSession_SegmentPath` que asumía forward-slashes y fallaba en Windows.

**Files of record**: [`internal/stream/capabilities.go`](internal/stream/capabilities.go),
[`internal/stream/decision.go`](internal/stream/decision.go),
[`internal/api/handlers/stream.go`](internal/api/handlers/stream.go).

### Commit 2 — `f21194f` `api(client)`: probe MSE + send header

Cliente web probea `MediaSource.isTypeSupported` contra una lista fija de
pares (codec, MIME). Resultado cacheado por sesión de página (codec
support no cambia sin reload). El `api.request()` adjunta el header
automáticamente.

Detalles defensivos:
- `isTypeSupported` lanza en algunos browsers con MIME malformado → try/catch
  per-MIME, no envenena el resto.
- SSR / pre-MSE → null → server fallback a defaults web (preserva legacy).
- Cero probes pasan → header suprimido (no mentir con header vacío).
- `hevc` y `h265` se emiten ambos cuando MSE decodifica la familia (ffprobe
  normaliza a "hevc" pero hay items legacy con "h265" — listar ambos casa
  con cualquier nombre que llegue del scanner).

Tests: 9 (SSR, throw, partial, contenedor, memoización, alias hevc/h265,
isTypeSupported missing, todo-false, fetch real con header).

**Files of record**: [`web/src/api/clientCapabilities.ts`](web/src/api/clientCapabilities.ts),
[`web/src/api/client.ts`](web/src/api/client.ts).

### Commit 3 — `94cb74b` `sync(progress)`: cross-device via SSE

El feature insignia "lo empecé en el portátil, sigo en el móvil". El bus de
eventos (`internal/event/bus.go`) ya existía para scanner/IPTV; este commit
lo abre per-user con filtrado correcto.

Tres tipos nuevos en el bus: `user.progress.updated`, `user.played.toggled`,
`user.favorite.toggled`. Splitting en tres (en vez de un genérico
`user_data.changed`) permite al frontend invalidar el set TanStack correcto
sin parsear payload kind.

`ProgressHandler` recibe `EventBusPublisher` opcional; cada uno de los 4
endpoints mutating publica tras DB write. nil bus = no-op (test rigs
simples).

Nuevo handler `GET /api/v1/me/events` (SSE):
- **Filtra por `Data["user_id"] == claims.UserID` ANTES del channel write**
  → un cliente lento del usuario A no presiona la publicación al usuario B.
- Defence in depth: rechaza eventos con Data nil, sin user_id, o con
  user_id wrong-typed. Un publisher mal configurado **no** debe fan-out a
  todo el mundo.
- Mismo framing SSE que `/events` (keepalive, JSON shape, unsubscribe-on-disconnect)
  → el `EventSource` del frontend consume ambos sin divergencia.

**Por qué SSE y no WebSocket**: canal one-way (server → client) y casa con
todos los clientes (web `EventSource`, Kotlin TV `okhttp-sse` maduro,
auth por cookie, exp-backoff reconnect gratis). WebSocket compraría
bidireccionalidad innecesaria + nginx upgrade config.

Frontend:
- `useUserEventStream` — sibling de `useEventStream` apuntando a `/me/events`,
  nombre explícito para que admin code no instancie sin auth por accidente.
- `useUserDataSync` — orchestrator: 3 subs → invalidaciones correctas:
  - `progress` → `items/{id}`, `progress/{id}`, continue-watching
  - `played`   → `items/{id}`, continue-watching, next-up
  - `favorite` → `items/{id}`, favorites
- Montado **una vez** en `AppLayout` (no per-page) para no fan-out duplicate
  connections ni perder eventos cuando otra ruta esté activa.

Tests: 5 backend (401 unauth, delivers own, drops other users', drops
malformed, unsubscribes 3 tipos on disconnect) + 5 frontend (subs por tipo,
invalidaciones correctas, malformed JSON no-throw, close on unmount,
disabled mounts nothing). Total 70+ stream/handlers backend, 364/364 frontend.

**Files of record**: [`internal/api/handlers/me_events.go`](internal/api/handlers/me_events.go),
[`internal/api/handlers/progress.go`](internal/api/handlers/progress.go),
[`internal/event/bus.go`](internal/event/bus.go),
[`web/src/hooks/useUserDataSync.ts`](web/src/hooks/useUserDataSync.ts),
[`web/src/components/layout/AppLayout.tsx`](web/src/components/layout/AppLayout.tsx).

### Estado al cierre

- `main` 2026-04-29 17:04, working tree clean, sincronizado con origin.
- Backend `go test -race ./...` clean, frontend 364/364 verde, `tsc -b` clean.
- Quedan en P1: device-code login (~1-2 días) y OpenAPI spec (~½ día).
  Después de esos dos, **todos los prerequisitos para empezar la app
  Kotlin TV están completos**.

---

## 🎨 Sesión 2026-04-29 tarde-noche (rama `claude/review-project-resume-3V6Mr`) — 4 commits

Iteración guiada por capturas de usuario. Tres rondas de feedback in-the-loop: (1) "el hero series está roto, container desplazado", (2) "el hero **bueno** era el de series, las películas deberían igualarse", (3) "ahora aurora demasiado soso, backdrop pixelado, botón se corta, sigue viendo duplica". Cada ronda cerró con commit + push. Cierra con la **paleta de colores explicada** y memoria actualizada.

### Commit 1 — `39698ce` unify hero + Plex-style cast

Antes: `HeroSection` (movies) tenía layout poster-izq + info-flex anclado al bottom; `SeriesHero` (series/season) tenía contenido apretado en columna izquierda max-w-md, dejando el 60% de la banda como backdrop puro. Visualmente eran dos surfaces distintos. Plus el `ItemDetail` wrapper pintaba `--detail-tint` como `backgroundColor`, creando seam con AppLayout en los lados de la página = "container desplazado raro" alrededor de Temporadas.

Cambios:
- **Pivote tras malentendido**: primer pase intenté unificar moviendo SeriesHero al layout poster-bajo de HeroSection; el usuario corrigió "el bueno era series, no movies". Reverted SeriesHero al layout original; reescribí HeroSection adoptándolo (full-bleed band + columna izquierda con poster + título + meta + buttons). Episode breadcrumb + S01E03 prefix preservados. Kebab menu pop a `left-0` (la columna está en izquierda).
- **`CastChip` estilo Plex**: avatar circular 96-112 px con ring border, nombre + personaje en texto limpio centrado debajo, sin tarjeta envolvente. El usuario explícitamente "me gusta más como lo hace plex con un círculo del avatar y el nombre en limpio".
- **Wrapper `ItemDetail` deja de pintar `--detail-tint` como `backgroundColor`**: la CSS-var sigue definida (la usa el bottom-fade del hero como target) pero el wrapper queda transparente. Mata el container desplazado.
- **Foundation `ListFilmographyByPerson`**: SQL en `internal/db/queries/people.sql` + sqlc stub a mano (ADR-004) + repo method dedupe-por-item con min(sort_order). Handler/ruta no enchufados — coste 0 hoy, sesión próxima `/people/{id}` es puramente aditiva. Episode-level credits drop through (TMDb ya tiene cast a nivel de show en el 95% de casos).

### Commit 2 — `99fd307` ambient aurora PS3-style

Usuario: "el tinte para cuando salia la lista de temporadas en series si me gustaba como sensacion premium que cogia el color y cada pagina era personal de peliculas, pero claro estaba mal hecho". Quiere el efecto, sin el bug.

Solución arquitectural: en vez de pintar el wrapper, render `<div fixed inset-0 -z-10>` como capa canvas viewport-completo detrás de todo. Tres `radial-gradient` apilados:
- vibrant blob 80% × 60% en (15%, 10%) — cubre el hero left side
- muted blob 70% × 70% en (85%, 90%) — cubre seasons grid + cast
- halo central 35% radio en (50%, 50%) — soft tonal balance

Cada página de detalle pinta su propia personalidad sin animación (respeta reduce-motion por defecto, sin coste de GPU paint constante).

### Commit 3 — `7000ec9` aurora actually visible + softer seam

Foto del usuario: aurora invisible, hero "se corta". Diagnóstico:

- **AppLayout pintaba `bg-bg-base` en su wrapper**. El body ya tiene `bg-bg-base` global en `styles/globals.css`. La duplicación tapaba la capa fixed `-z-10` de la aurora. Da igual subir intensidades — invisible mientras el AppLayout no fuera transparente. **Fix**: quitado `bg-bg-base` del wrapper de AppLayout (el body se encarga). Ningún otro page se rompe porque el body cubre el viewport igual.
- **Bottom-fade `h-32` cliff**: 128 px de fade sobre un hero de 600-720 px lee como seam horizontal. Subido a `h-48 lg:h-56` (192-224 px).
- **Intensidades subidas**: 28% / 26% / 12% → 45% / 40% / 18% (vibrant / muted / halo).

### Commit 4 — `10afab3` sharp backdrop + clipped button + duplicate panel + soso aurora

Cuarta foto, cuatro síntomas distintos:

- **Backdrop pixelado en movies**: `thumb(url, 1280)` pedía variant 1280-wide al backend que el browser luego upscaling-a-1920 mostraba blando. Backdrop ahora sirve URL original (sin `?w=`). El poster mantiene `thumb(url, 720)` — es pequeño, vale la pena el ahorro de ancho de banda.
- **Botón Reproducir cortado**: el inner div del hero tenía `max-h-[720px]`. Cuando el contenido (logo + tagline + meta + watched-count + overview + buttons) excedía ese techo, las acciones se clippeaban. Removed. `min-h` se queda para que el hero no colapse.
- **"Sigue viendo" duplicado**: el panel de resume-target episode renderizaba en BOTH series y season pages. En la página de season ya está la lista completa de episodios con `EpisodeRow` mostrando progreso por fila — surface el mismo affordance dos veces (panel + list row) era ruido visual. Ahora condicionado a `heroScope === "series"`.
- **Aurora soso**: el blob inferior-derecho (donde scrollea el usuario más) se sembraba con `muted` — desaturado por definición. Cambiado a usar `vibrant` en ambos blobs principales (60% upper-left, 50% lower-right), `muted` queda como counter-blob central a 28%. Foreground contrast preservado por el corte del mix.

### Cómo funciona la extracción de colores (preguntado por el usuario al cierre)

Pipeline en dos pasos, sin SaaS:

**Backend** — `internal/imaging/colors.go::ExtractDominantColors`, corre cuando `IngestRemoteImage` baja la imagen al disco:
1. Decodifica con std-lib decoders (mismos que blurhash).
2. Muestrea ~1024 px en grid `step = max_dim / 32` (coste O(1) por imagen).
3. Bucketea en cubo RGB 16×16×16 (4096 bins, cada uno acumula r/g/b sum + count).
4. Por bucket calcula L y S del HSL y puntúa dos ganadores:
   - `vibrant = saturation × count`, restricted L ∈ [0.20, 0.80] (excluye blown highlights y jet black)
   - `muted = (1 − saturation/2) × count`, restricted L ≤ 0.40 (oscuro pero legible)
5. Persiste como strings `rgb(R, G, B)` en columnas `images.dominant_color` + `images.dominant_color_muted`.

Returns `("", "")` cuando el decoder no entiende la imagen (mismo contrato que `ComputeBlurhash`).

**Wire**: `db.PrimaryImageRef` carga ambos campos. `/items/{id}` los expone como `backdrop_colors: { vibrant, muted }`. `GetPrimaryURLs` los devuelve por item para que `PosterCard` pinte placeholder mientras decodifica.

**Frontend**:
- Path principal: `item.backdrop_colors.vibrant/muted` directos del wire.
- Fallback (`useVibrantColors`): para items pre-extracción, corre `node-vibrant` sobre los bytes de la imagen via dynamic import. Lazy chunk separado.
- La aurora del wrapper **solo** usa el path principal (no fallback runtime) para que el viewport-canvas paint sea barato.

Se extrae siempre del **backdrop**, no del poster — el backdrop es lo que pinta colorida la mayoría de la página.

---

## 🎬 Sesión 2026-04-29 día (sesión `015ThedLMwhsx5ittdmtxSN4` "premium detail UX") — 8 commits

Sesión sobre rama de catch-up (PRs ya mergeados a main), no documentada en su momento. Bloque temático: pulir todo el detail surface al nivel "premium" antes del pivot a Kotlin TV.

| Commit | Tema |
|---|---|
| `699d63e` | fix: nil-safe SettingsRepository + drop now-dead DedupeSeasonsByChildCount (los UNIQUE indexes 018 ya garantizan invariante) |
| `49d853e` | poster placeholder colour, hero crop, page tint, kill 401 cold-start noise (auth bootstrap real con `bootstrap()` que hace refresh antes de protected queries) |
| `b5988d7` | drop ItemDetail tint gradient + restore SeriesHero height (primer intento de fix del seam — fallido, el fix definitivo llegó en sesión `claude/review-project-resume-3V6Mr`) |
| `2f2cfed` | thumbnails ?w= en cards, tagline+studio en SeriesHero, **watched-count agregado en series** (`SeriesEpisodeProgress` query con JOIN parent_id 2-niveles), **weekly image refresh scheduler** en `library.ImageRefreshScheduler` |
| `e07fe4e` | series: tint de página coordinado con hero (`--detail-tint`), button breathing |
| `6b2996f` | movies: port hero premium desde series (parity primer intento — el unificado real fue en sesión siguiente) |
| `bed03a2` | detail: external_ids → deep links IMDb/TMDb en kebab del hero |
| `bd64951` | **detail: cast/crew end-to-end con fotos** — tablas `people`/`item_people` ya estaban en el schema desde la migración 001 pero nadie leía/escribía. Wired pipeline completa: `db.PeopleRepository` (4 ops + dedupe by name), `scanner.syncPeople` baja photo via `IngestRemoteImage` con SSRF guard, handler `GET /api/v1/people/{id}/thumb` (path-traversal validado), wire `/items/{id}` incluye `people: [{id, name, role, character?, image_url?, sort_order}]`. Frontend: `CastChip` con avatar real + onError fallback a inicial. Foundation para `/people/{id}` (la id ya viaja en el wire). |

---

## 🧹 Sesión 2026-04-29 noche (post-review hardening + settings runtime) — 4 commits

Sesión orientada a "lo que hicimos ayer, ¿es realmente sólido?". Empieza con un peer-review sobre el IPTV hardening (no del usuario — propio, como siguiente capa de tamiz) y termina con la pieza arquitectural más importante de la rama: dejar de pedirle al usuario que edite YAML.

### Commit 1 — `a21204c` IPTV transmux post-review hardening (5 bugs + split)

Auto-review del trabajo del 28→29 detectó **5 bugs reales latentes** y un techo de mantenibilidad:

- **B1 stderr drain race en `processWatcher`**: `cmd.Wait()` no espera al goroutine consumer del `StderrPipe()`. El stdlib lo dice explícitamente. Resultado: la cola de `stderrTail` puede no incluir la línea fatal que ffmpeg emite justo antes de exit. Eso rompía silenciosamente la decisión de auto-promoción a reencode (`looksLikeCodecError` decidía sobre cola incompleta) y truncaba el log al peor momento. **Fix**: `stderrRing.wait()` que bloquea hasta que `consume` retorna; processWatcher sincroniza antes de leer `String()`.

- **B2 scanner buffer 4 KiB causaba deadlock potencial**: `bufio.Scanner.Buffer(_, 4096)` aborta con `ErrTooLong` en cualquier línea >4 KiB (debug builds, full TLS chains). El consumer salía, la pipe del kernel se llenaba, ffmpeg bloqueaba en write hasta que `-rw_timeout` (10s) lo mataba. **Fix**: bump a 64 KiB + `io.Copy(io.Discard, rd)` de drenaje fallback al salir el scanner. Comentario "binary garbage is silently truncated" era falso, corregido.

- **B3 race entre `evict` y respawn por mismo `WorkDir`**: evict soltaba el lock antes de `os.RemoveAll(WorkDir)`. Mientras, otro `GetOrStart` para el mismo canal podía entrar, hacer `MkdirAll` en el mismo path, y luego el RemoveAll del primer evict se cargaba la dir nueva. **Fix**: `<cacheDir>/<channelID>/<startNanos>/` versionado por spawn. evict sólo borra ese subdir; el padre se limpia best-effort vía `os.Remove` (ENOTEMPTY se ignora). `clearWorkDir` borrado (innecesario con dirs nuevas siempre).

- **B4 doc lie en `iptv.go:52-55`**: el comentario decía "nil logoCache surfaces upstream URL" pero `logoProxyURL` siempre reescribe al proxy. Test `iptv_dto_test.go:51-68` ancla esa conducta. **Fix**: alinear comentario con la realidad (404 + React onError fallback a iniciales). No tocar el código — funciona, el comentario mentía.

- **M3 zombie sessions**: el reaper saltaba sesiones no-ready, pero ffmpeg con upstream que envía 1 byte cada 8s evade `-rw_timeout` y la sesión queda en el mapa indefinidamente, bloqueando un slot de `MaxSessions`. **Fix**: `startupGraceMultiplier = 2`, después de `2× ReadyTimeout` el reaper force-terminate la sesión y registra failure en el breaker (chronic offenders entran en cooldown).

- **M2 spawn_error metric**: pre-spawn fails (mkdir, fork, pipe) no incrementaban ningún counter en el sink de Prometheus. **Fix**: `IncStarts("spawn_error")` distinto de `crash` (upstream).

- **M1 split de `transmux.go`**: 1451 → 1052 líneas. Sin abstracciones nuevas, sólo relocalización:
  - `transmux_args.go` (287) — argv builders + encoder tuning + `defaultTransmuxUserAgent`
  - `transmux_codec_classify.go` (35) — `codecErrorPattern` + `looksLikeCodecError`
  - `transmux_stderr.go` (116) — `stderrRing` con la nueva `wait()` barrier

**Tests nuevos** (6 regression):
- `TestStderrRing_WaitBlocksUntilConsumeReturns` (B1)
- `TestStderrRing_WaitNilSafe` (B1, defensivo)
- `TestStderrRing_DrainsAfterOverlongLine` (B2)
- `TestTransmuxManager_ReapsStartupZombies` (M3)
- `TestTransmuxManager_PerSpawnVersionedWorkDir` (B3)
- `TestTransmuxManager_PreSpawnFailureCountsAsSpawnError` (M2)

### Commit 2 — `8a723c0` tests frontend de useEventStream / useTrickplay / useLiveHls (+31 tests)

Deuda arrastrada desde múltiples sesiones. 31 tests nuevos:
- **useEventStream (7)**: stub global `EventSource`, verifica open/close lifecycle, dispatch del tipo correcto, no churn al re-render con closure nueva (el ref-stash optimization), swap al cambiar tipo.
- **useTrickplay (8)**: empty itemId no-op, tolera envelope `{data: ...}` y bare manifest, 503 / network error → `available=false` silencioso, abort en unmount, encode del itemId.
- **useLiveHls (16)**: mock de `hls.js` vía `vi.hoisted` (FakeHls), null url/ref no-ops, onFirstPlay una sola vez, timeout fallback con `onFatalError("timeout")`, retry 3 network errors antes de rendirse, classify manifest errors, onFatalError fires once per stream URL, streamUrl change destruye instancia previa, unmount limpia visibilitychange listener, document.hidden flip → stopLoad/startLoad(-1), reload() force-reattach, ref-stash con onFirstPlay.

296 → 327 tests, suite verde.

### Commit 3 — `0779e4e` migración 018 UNIQUE partial indexes para show hierarchy

**Diagnóstico**: el usuario reportó series duplicadas. Análisis del código:
- Schema `items` no tiene UNIQUE constraint en (library_id, type, title); el cache `showCache` en `internal/scanner/show_hierarchy.go` evita dups en condiciones normales pero la key es title-exact (case/whitespace/accent miss → cache miss → dup).
- Cuando ya hay dups en DB, el seeding pasa `rememberSeries(title, id)` por cada uno y la última gana en el map; las anteriores quedan huérfanas para siempre.
- Para seasons ya había `DedupeSeasonsByChildCount` (read-time dedupe en `library/service.go`), para series no había equivalente — por eso aparecían duplicadas en el rail.

**Lo que se hizo**: el usuario verificó empíricamente que un wipe (`DELETE FROM items WHERE type IN ('series','season','episode')` con `PRAGMA foreign_keys = ON`) + rescan **no recreaba duplicados**. Confirmó que el bug era residuo histórico, no regresión activa.

**El fix**: migración 018 con (a) pasada de cleanup defensiva — re-parenta hijos de no-canónicos al canónico (MIN(id)), borra los demás; no-op en DB ya limpia. (b) **UNIQUE INDEX parciales**:
- `uniq_series_per_library ON items(library_id, title) WHERE type='series'`
- `uniq_season_per_series ON items(parent_id, season_number) WHERE type='season' AND season_number IS NOT NULL`

**Lo que NO se hizo (y por qué)** — Torvalds-simple aplicado consistentemente: sin `ErrItemConflict` tipado, sin `FindSeriesByTitle/FindSeasonByNumber` helpers, sin recovery branches en el scanner. El usuario verificó que el scanner actual no genera dups; añadir silent-recovery code defendería escenarios hipotéticos. Si la migración alguna vez "salta" en el futuro será porque hay un bug real, y queremos que falle ruidosamente (`UNIQUE constraint failed: items.title`) en vez de papelera.

### Commit 4 — `b1a84da` runtime-editable settings (kill the "edit yaml" prompts)

**Disparador**: el usuario miró el panel /admin/system y vio dos cards diciendo "Sin configurar (define server.base_url en hubplay.yaml)" y "Activa hardware_acceleration.enabled en hubplay.yaml". Su feedback: *"no quiero que el usuario tenga esa responsabilidad en el yaml, debería poder hacerse en el panel"*. Razón Torvalds: una abstracción que pide al admin SSH-ear y editar un fichero está rota.

**Decisión arquitectural** (ver ADR-010):

| Capa | Qué vive ahí | Inmutable? |
|---|---|---|
| YAML / env | server.bind, server.port, database.path, streaming.cache_dir, auth bootstrap secret | sí (boot-time) |
| `app_settings` (DB) | server.base_url, hardware_acceleration.enabled, hardware_acceleration.preferred | no (runtime overlay) |

Authority chain: `app_settings` row → YAML default → effective. Sin Runtime overlay struct, sin caching layer, sin goroutine watching changes. Handlers que necesitan un valor reciben `SettingsReader` (interfaz pequeña, sólo `GetOr`) y consultan al servir la request. Una SQLite point query por hit en `/admin/system/stats`, invisible en perfil.

**Surface nuevo**:
- Migración 019: `app_settings(key TEXT PK, value TEXT, updated_at)`.
- `internal/db/settings_repository.go` con `Get/GetOr/Set/Delete/All`. Sin tipos de dominio — strings raw, validados arriba en el handler.
- `internal/api/handlers/settings.go` con `GET/PUT/DELETE /admin/system/settings`. **Whitelist hardcoded** (no es un KV genérico). Una key nueva entra con un const + un caso en el switch del validator + un par de strings i18n.
- HWAccel se aplica al boot: `cmd/hubplay/main.go` lee del settings repo justo antes de `stream.NewManager`. La UI dice "Reinicia para aplicar" cuando hay override pendiente. **Sin re-detección runtime** (el detector tiene estado capturado, replicarlo es ruido).
- BaseURL es runtime: `SystemHandler.effectiveBaseURL(ctx)` y `StreamHandler.effectiveBaseURL(ctx)` consultan el settings repo en cada request. Save en panel → próximo request lo ve.

**Frontend**:
- `useSystemSettings`, `useUpdateSystemSetting`, `useResetSystemSetting` hooks; mutations invalidan `systemStats` para que el panel refresque al instante.
- `web/src/pages/admin/system/SystemSettingsSection.tsx` — sección nueva al final del System page. Per-row Save + Reset, dirty-state pinning del Save, badge `Custom`/`Default`, restart-needed hint inline. `<input>` para texto libre, `<select>` para enum (driven por `allowed_values` del backend).
- Borrados los strings i18n `baseURLEmpty` y `hwAccelDisabledHint` que pedían editar yaml. Reemplazados por `baseURLUnset` y `hwAccelDisabledPointer` que apuntan a la sección editable.

**Tests** (13 nuevos):
- 6 settings_repository_test (GetOr fallback, Set upsert, Delete reset, etc.)
- 7 settings_test del handler (whitelist gate, validation per-key, normalisation, reset, defaults)

### Estado al cierre

- Backend Go: `go test -race ./...` verde.
- Frontend: 41 test files, 327 tests, todos verde. tsc clean.
- Migraciones: 18 + 19 = 19 en total (017 → 019).
- `transmux.go` 1451 → 1052 líneas (extracción a 3 ficheros sibling).
- 4 commits limpios en la rama, listos para PR a `main` cuando el usuario diga.

### Lo que el usuario tiene que hacer al desplegar

- Wipe ya aplicado de su DB (manual). La migración 019 no hace nada en su DB ya limpia; `app_settings` arranca vacío y todo cae al YAML default.
- Tras pull de la nueva imagen, el panel /admin/system tendrá la sección "Configuración" al final con tres tarjetas editables. El YAML sigue funcionando como fallback; el operador puede ignorar el panel si quiere.

### Próximos hitos candidatos (no en este sprint)

- **App nativa Kotlin para Android TV** sigue siendo el gran post-merge — pero el usuario confirmó en 2026-05-04 que **NO va a empezarla todavía**; primero quiere dejar el HubPlay web pulido.
- ~~Virtualización de `EPGGrid` con `@tanstack/react-virtual`~~ ✅ **shipped** (`d2f5216` ui(livetv): virtualize EPG grid above 50 channels).
- Single-flight EPG fetches (deferida; lock per-library cubre el caso común).
- Cuarto setting runtime cuando aparezca un caso real — añadir es trivial (whitelist + i18n).

---

> **Las sesiones del 2026-04-27 al 2026-04-29 (pre-detail-UX) viven ahora en**
> [`archive/2026-04-27-to-04-29-pre-detail-ux.md`](archive/2026-04-27-to-04-29-pre-detail-ux.md).
> Incluye: IPTV hardening completo (7 commits), transmux + import event-driven,
> M3U import async, huge-list resilience, peer-review followups, iptv split,
> SRP refactor, auditoría senior + remediación, series detail UX completo,
> hot-fix responsive admin. Cuando algo de aquí en HANDOFF cite un commit del
> bloque archivado, ahí están los detalles.

---

## 👉 HANDOFF PARA LA PRÓXIMA SESIÓN

> **Lee esto primero.** Resume qué cerramos, qué decidimos y qué toca.

### Lo que cerramos esta sesión (rama `claude/review-movies-series-feature-9npZH`)

**35 commits** sobre la rama. Empezó como un *senior code review* del
surface Movies / Series y derivó en un rework profundo del flujo
end-to-end más cuatro bugs críticos que el usuario reportó al probar.

#### Bloque A — Bugs catastróficos cerrados

1. **`06bde24` — scanner persistía URLs remotas**. Cada vista de
   poster era un `307` → `image.tmdb.org`. Privacy + fragilidad.
   Ahora `imaging.IngestRemoteImage` (atomic write, blurhash,
   SafeGet). Test de regresión:
   `TestFetchAndStoreImages_PersistsLocalPathNotURL`.

2. **`56d18af` + `93c643e` — librería de Series 400 + admin invisible**.
   Cross-stack mismatch `tvshows`/`shows`. Backend +
   `api/types.ts` + `LibrariesAdmin.tsx` + setup wizard alineados
   al canónico `shows`.

3. **`79c319e` + `45888a1` — `/series` vacío**. El scanner no
   construía jerarquía series → season → episode. Implementé el
   parser estilo Plex (`show_parser.go`, ~25 tests) + cache
   in-memory por scan (`show_hierarchy.go`).

4. **`d07e367` — limpieza pre-launch**. Quitada toda capa de
   compatibilidad legacy (alias `tvshows`, fallback a URL remota
   en ServeFile, runtime backfill de hierarchy). Una sola forma
   válida para cada cosa. -237 LOC, +0 funcionalidad perdida.

#### Bloque B — Foundation arquitectónica

| Commit | Tema |
|---|---|
| `697734c` | Dedupe Movies/Series → `MediaBrowse` genérico |
| `eb7795e` | `user_data` per-item en listings (4 tests) |
| `bb8dc17` | TMDb/Fanart cache + backoff + single-flight (12 tests) |
| `06bde24` | Scanner descarga imágenes a disco + atomic writes |
| `e27e60b` | Thumbnails se reapan al borrar imagen |
| `bcc8fb7` | `is_locked` flag — manual override sobrevive refresh (Plex parity). Migración 013 |
| `4eb7b70` | Continue Watching filtra near-complete (≥90%) + abandoned (>30d ∧ <50%) |
| `6bbbb64` | `provider.ImageResult.Source` se rellena en el Manager |

#### Bloque C — Features visibles

| Commit | Tema |
|---|---|
| `07fd29f` | Up Next overlay con countdown 5s |
| `6d904db` | Quality picker en player |
| `33c9f9c` | i18n del player completo |
| `75eee70` | Capítulos: ffprobe → DB → marcas en seek bar |
| `782d233` | Endpoints external subs (OpenSubtitles wired) |
| `2b823e9` | HW accel ya **se usa** (antes se descartaba) |
| `0f26fb0` | Trickplay backend lazy generation |
| `444e7b6` | UI subs externos (modal + `<track>` dinámico) |
| `024586e` | Trickplay UI: hover preview en seek bar |
| `6981a9c` | Filtros género/año/rating en MediaBrowse |
| `3dda6dc` | "Watch Tonight" tile en Home |
| `465298c` | Audio picker enriquecido ("English · TrueHD 7.1") |

#### Bloque D — Tests añadidos

- Backend: `+~30 tests` netos. Cobertura nueva en `imaging/`,
  `provider/`, `library/`, `scanner/`, `db/`, `api/handlers/`.
- Frontend: 245 → **289 tests** (37 ficheros).

### Estado de operación

- Working tree limpio, push hecho, rama lista para review/merge.
- **Usuario probó la rama**. Destapó 4 bugs en proceso (todos
  cerrados). Falta verificación end-to-end exhaustiva siguiendo el
  QA checklist.
- **QA checklist actualizado** con un bloque ⚠️ al inicio:
  **borra DB / lib antes de probar** (la rama no tiene runtime
  migration; el código nuevo solo construye jerarquía al INSERT).

### Decisiones senior tomadas (registradas en architecture-decisions)

- **ADR-002**: Imágenes descargadas siempre a disco. URL remota
  nunca se sirve al cliente.
- **ADR-003**: `is_locked` per-image, auto-set en cualquier acción
  manual. Refresher gate per-kind.
- **ADR-004**: Continue Watching filtra near-complete ≥90% +
  abandoned >30d∧<50%. Sin duración → bypass.
- **ADR-005**: Show hierarchy desde estructura de dirs (Plex
  convention). Cache in-memory por scan.
- **ADR-006**: HW accel input flag sin `-hwaccel_output_format`
  (frames bajan a RAM, escala SW, encoder HW). Tradeoff
  documentado.

---

## 🎯 PRÓXIMO HITO ESTRATÉGICO: Kotlin Android TV

> Decisión registrada en sesión 2026-04-27. La siguiente gran fase
> después de mergear esta rama es **app nativa Kotlin para Android
> TV** (Jetpack Compose for TV / Leanback).

### Qué cambia este pivote

Toda decisión técnica de aquí en adelante se evalúa contra:

> "¿Esto facilita o estorba consumir la API desde un cliente nativo
> que necesita rendimiento, que decodifica códecs que el navegador
> no, y que vive en un mando D-pad sin ratón?"

Eso re-prioriza el roadmap. Trabajo que sigue siendo valioso pero
**baja en la cola**:
- Subtitle styling per-user web (la app nativa lo hará a su manera).
- Smart collections con DSL (vale igual; web-only ahora).
- Watch-together (gran feature pero post-MVP de la app TV).
- Trickplay scan-time pregeneration (lazy ya cubre el 90%).

Trabajo que **sube en la cola** porque la app TV lo necesita:
1. **Auth para clientes nativos** (device code flow estilo Netflix:
   "introduce este código en hubplay.tu-dominio.com/link"). Sin
   esto, login en TV con teclado D-pad es un infierno.
2. **API stable + documentada**. Cliente nativo no puede iterar
   sobre cambios silenciosos del wire format. Se necesita un
   contrato versionado (`/api/v1/*`).
3. **Capability negotiation real**. La app TV puede pasar TrueHD,
   Atmos, HDR10 al receptor; el server tiene que dejar de
   re-codificar cuando puede direct-stream. Hoy enriquezco labels
   pero todo audio se transcodea igual.
4. **Cross-device progress sync**. "Empecé en el móvil, sigo en
   la TV" es la killer-feature de Plex. WebSocket / SSE con
   debounce.
5. **Endpoint plano para Now Playing / Up Next** (ya existe el
   server-side, falta confirmar que el shape sea el que la app
   quiere consumir).

---

## Cola priorizada para la siguiente sesión

### **Pendiente · IPTV per-user channel order + hide** (specced 2026-05-13)

> Plan completo en [`per-user-channel-order-pending.md`](./per-user-channel-order-pending.md).

Hoy `channels.number` es **global** — todos los usuarios ven el mismo
dial. El user pidió poder reordenar la lista de canales por cuenta
("poner el 35 en el 3") y ocultar canales que no quiere ver.

Implementación esperada:
- Nueva tabla `user_channel_preferences (user_id, channel_id, custom_number, is_hidden, updated_at)` con PK compuesta y cascade deletes (SQLite + Postgres).
- `iptv.Service.ListHealthyByLibraryForUser(ctx, libraryID, userID)` que joina prefs + filtra `is_hidden` + reemplaza `Number`.
- Endpoints `POST /me/channels/reorder`, `PUT /me/channels/{id}/number`, `PUT/DELETE /me/channels/{id}/hide`.
- UI Android TV: long-press OK sobre canal → "subir/bajar/ocultar". Web: drag&drop.
- **Esfuerzo**: ~1 sesión mínimo viable (reorder), ~2 con hide + UI completa.

**Confirmado por separado**: los canales con health_status=dead YA se
filtran del lado server vía `ListHealthyByLibrary` (test
`TestChannel_ListHealthyByLibrary_HidesUnhealthyAndDisabled`). No hay
trabajo aquí — la pregunta sólo era verificación.

### **P0 · Cerrado** (federación security debt — ver bloque sesión 2026-04-30 mañana al inicio)

1. ~~Replay protection peer JWT~~ ✅ **shipped 2026-04-30** (`b0b0613`).
2. ~~SSRF parcial via peer-controlled BaseURL~~ ✅ **shipped 2026-04-30** (`c6e0d84`).
3. ~~RevokePeer TOCTOU micro-window~~ ✅ **documented 2026-04-30** (`3b24e92`).

### **P1 · CERRADO** — Pre-Kotlin TV foundation completa

4. ~~**Device-code login flow**~~ ✅ **shipped 2026-04-30** (PR #124).
5. ~~**Versionado del API + documentación OpenAPI**~~ ✅ **shipped 2026-04-30** (rama `claude/openapi-spec`).
   Hoy todo cuelga de `/api/v1/*` pero no hay un OpenAPI spec
   versionado. Generar con go-swagger o swag (anotaciones en
   handlers). Sin esto, la app Kotlin va a redescubrir el wire
   format por trial-and-error. **Federación incrementa la urgencia**:
   ahora hay 12+ endpoints `/api/v1/peer/*` que un cliente tercero
   (otra implementación de HubPlay) tendría que entender.

6. ~~**Capability negotiation server-side**~~ ✅ **shipped 2026-04-29** (`5a37ed2` + `f21194f`).
7. ~~**SSE para progress sync**~~ ✅ **shipped 2026-04-29** (`94cb74b`).
8. ~~**Person/cast clickable**~~ ✅ **shipped 2026-04-29** (`19be7f0` `people(detail): wire /people/{id} end-to-end`).
9. ~~**Federación P2P (Phases 1-4)**~~ ✅ **shipped 2026-04-30** — ver ADR-012 y bloque de sesión arriba.

### **P2 · Quick wins paralelos a la app**

10. **Blurhash en `<PosterCard>`**: ya implementado parcialmente en
    `cd14d11`. Verificar que la integración consume el campo.

11. **WebP blurhash backend**: ya shipped en `48f53cc` (Fanart logos).

12. **Provider priority configurable** (½ día). Campo ya existe
    en DB; falta UI admin (drag-to-reorder).

13. **Federación Phases 5-7** (~5-8 días total) — el resto del roadmap
    de federación.
    - **Phase 5 — slice 1 (backend remote streaming) ✅ shipped 2026-05-04** (`bbafd56`).
      `POST /peer/stream/{itemId}/session` + HLS proxy round-trip funcionan;
      `client_stream_test.go` cubre el wire format.
    - **Phase 5 — slice 2 (frontend Plex-style)** ✅ **shipped 2026-05-04**
      (`209aa53`). Sidebar peer-as-library, `PeerItemDetail`, posters proxiados.
    - **Federated search frontend (search page + topbar dropdown)** ✅
      **shipped 2026-05-04** (commits `5059191` + `888c20c`).
    - **Recently-Added on peers home rail** ✅ **shipped 2026-05-04**
      (commit `888c20c`).
    - **`federation_progress` table + Continue Watching cross-peer** ✅
      **shipped 2026-05-05** (commit `05c6b4a`). Migración 028 +
      sqlc + 3 endpoints (`GET/POST /me/peers/{p}/items/{i}/progress`
      y `GET /me/peers/continue-watching`) + `useProgressReporter`
      con `peerId?` + Resume button en `PeerItemDetail` +
      `PeerContinueWatchingRail` en Home.
    - **Smoke real con dos servers paireados** ✅ **hecho 2026-05-05**
      (sesión `claude/federation-progress-cross-peer`). End-to-end
      con vídeo real (Juego de Ladrones (2018), 14 GB), Chrome MCP
      pilotando dos pestañas en `localhost:8198`/`8199`. Pareados
      via UI admin, share, Play, Resume @ 0:58, rail Continue
      Watching visible. Bug de config descubierto y arreglado en
      el camino (commit `013550c`).
    - **Config bug `HUBPLAY_SERVER_BASE_URL` ignorado sin yaml** ✅
      **fixed 2026-05-05** (commit `013550c`). `config.Load`
      saltaba `applyEnvOverrides` cuando no había `hubplay.yaml`,
      rompiendo deployments fresh-install que dependieran de env
      vars (típico Docker compose). Tests reescritos.
    - **Subtítulos federados**: pendiente (~2h). Master.m3u8 federado
      no proxia tracks de subs.
    - **`MaxConcurrentStreams` por peer**: hoy compiten contra el
      cap global `MaxReencodeSessions`. Pendiente.
    - **Promover `peer_recent` + `peer_continue_watching` a `HomeSection`
      configurables**: hoy ambos rails viven fuera del layout-driven
      dispatch. Ampliar `validSectionType` + UI en `HomeLayoutSettings`.
    - **Phase 6 — Live TV peering**: pendiente, no empezado.
    - **Phase 7 — Download to local + audit log UI**: pendiente.

### **P3 · Diferenciadores estratégicos** (semanas, no días)

10. **fsnotify watcher + priority enrichment queue** (~1 semana).
    Scan que NO bloquea visibilidad. Items aparecen instantáneo,
    metadata progresivamente. La app TV se siente snappy desde el
    primer minuto del primer scan.

11. **Multi-version del mismo título** (4K + 1080p agrupados).
    Schema lo soporta; UI/scanner no agrupan. Ventaja sobre
    Jellyfin para usuarios que ripean en múltiples calidades.

12. **Intro skip detection** (audio fingerprinting cross-episode).
    ~2 semanas. Feature signature. La app TV la hace VISIBLE
    (botón flotante "skip intro" tipo Netflix).

13. **Privacy stack** (modo offline NFO, egress allowlist, CSP
    estricto). Diferenciador de mercado real.

14. **Watch-together** (sync sessions WebSocket). Plex lo tiene
    roto en web; tu lo haces bien para web Y app TV.

### **Lo que NO está en la lista**

- **Family/kids profiles** — excluido por usuario.
- **Modo offline 100% para web** — excluido por usuario.
- **Apps iOS/Android nativas** — Android TV es el target; móvil
  puede esperar a PWA + Cast.
- **AI metadata** — anti-marca self-hosted.

---


---

## 📦 Sesiones archivadas

Para mantener este fichero ligero (entrypoint de cada sesión nueva),
las sesiones anteriores a 2026-04-27 viven en
[`archive/2026-pre-04-28.md`](archive/2026-pre-04-28.md). Incluye los
HANDOFFs antiguos, ciclos previos (live-TV coverage, simplify sweep,
lint debt cero, sqlc sweep) y el resumen ejecutivo histórico. Sólo
abrir cuando haga falta arqueología puntual sobre una decisión vieja.
