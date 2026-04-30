# Estado del proyecto

> Snapshot: **2026-04-30 mañana (continuada) — rama `claude/openapi-spec` con OpenAPI 3.0.3 spec embed-y-servida**. 1 commit sobre `main`. Cierra el último item P1 pre-Kotlin TV. **Cola P0+P1 vacía**: todos los prerequisitos para empezar la app Kotlin Android TV están completos.
>
> Estado prev (2026-04-30 mañana, ya en main vía PR #124): 3 fixes federación + device-code login (RFC 8628) end-to-end. Cierra 3 findings P0 + 1 P1.
>
> Estado prev² (2026-04-29 noche): Federación P2P entera (Phases 1-4 + plug-and-play + UX) — 8 commits.
>
> **tests al cierre: backend verde salvo `internal/config` preflight (env-only, ffmpeg no en PATH local; CI verde) · frontend 364/364 · tsc clean**.

**Rama `claude/openapi-spec` (sesión actual, 1 commit sobre main)**:
- `[pending-hash]` api(openapi): hand-written 3.0.3 spec embed-y-servida en `/api/v1/openapi.yaml`. Cubre auth + browse + stream + me + federation user surface (consumer-facing); admin/setup/peer-to-peer fuera de v1 deliberadamente.

**Rama `claude/federation-hardening` (mergeada a main vía PR #124)**:
- `b0b0613` federation(security): close JWT replay window with per-nonce cache
- `c6e0d84` federation(security): SSRF gate on peer-controlled AdvertisedURL
- `3b24e92` federation(docs): RevokePeer atomicity contract documented
- `[hash-pending]` auth(device): RFC 8628 device authorization grant — TV-friendly login
- `683c512` auth(device): /link page — operator approves a device code

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

- **App nativa Kotlin para Android TV** sigue siendo el gran post-merge.
- Virtualización de `EPGGrid` con `@tanstack/react-virtual` (deuda agendada conscientemente).
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
    de federación. Phase 5 (remote streaming + watch state, blocked en
    arreglar bugs P0), Phase 6 (Live TV peering), Phase 7 (download to
    local + audit log UI).

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
