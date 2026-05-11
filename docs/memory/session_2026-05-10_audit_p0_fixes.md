# Sesión 2026-05-10 — Auditoría senior + 11 fixes pushados

Rama `claude/relaxed-lederberg-48e836`, base `4aa83e7`. **Mergeado a `main` localmente** vía fast-forward; pendiente push de `main` a `origin/main` (la rama remote y main están al día con todos los commits).

## Punto de partida

El usuario pidió "revisa mi proyecto, quiero comprobar cosas que todo lo que tenemos funciona de verdad… piensa como senior, puedes rebuildear en local, a último commit y probar todo". Auditoría completa con 4 sub-agentes en paralelo (backend Go / frontend React / streaming-IPTV / infra-seguridad) + verificación visual end-to-end via Chrome MCP contra el contenedor Docker.

## Bugs P0 encontrados y arreglados (5)

### P0-A — Reproductor VOD totalmente roto

**Síntoma**: cualquier película o episodio → click "Reproducir" → solo se ve el backdrop, video nunca arranca. `segment00000.ts` daba 404 en bucle infinito.

**Causa raíz**: el contenedor Docker corría una imagen de 17 horas atrás (`hubplay:fingerprint`), antes del commit `44566b9` que arregló este mismo bug. Ese commit añadió la propagación de `?audio=N` desde el manifest sintetizado a las URLs de segmento.

**Fix**: rebuild docker desde el código actual de `main`. Cero líneas de código nuevas.

**Lección**: las imágenes Docker se quedan obsoletas silenciosamente. Si un commit fix está en `main` pero el contenedor sigue con una imagen vieja, el bug persiste para el usuario aunque la rama lo tenga arreglado.

### P0-B — `/api/v1/me/profiles` 500 en cada llamada

**Síntoma**: SQL syntax error `near "?": syntax error`. Bloquea el selector de perfiles ("Who's watching?"). TanStack Query reintentaba 6 veces por montaje → ~36 requests fallidas por cambio de ruta.

**Causa**: bug del codegen de **sqlc 1.31.1**. Cuando un `ORDER BY` combina una expresión booleana (`parent_user_id IS NOT NULL`) con un trailing `COLLATE NOCASE`, sqlc emite un literal Go con un `?;` espurio al inicio y trunca el último token de la query (e.g. `NOCASE` → `NOCA`). Un workaround previo de añadir `ASC` no funcionó para este shape.

**Fix** ([commit 251cd7a](commit/251cd7a)): mover la query a raw SQL en `internal/db/user_repository.go`, siguiendo el patrón de los otros 5 holdouts ya documentados (item, image, EPG, metadata, user_data). `UserRepository` gana un campo `db *sql.DB`. La query se ejecuta directamente y escanea las filas a `User`.

**Tests**: `TestUserRepository_ListProfilesForOwner` (parent + 3 profiles, ordenamiento case-insensitive vía `LOWER(display_name)`) + `_NoProfiles` (single-user install).

### P0-C — Endpoints de setup públicos para siempre

**Síntoma**: `/api/v1/setup/{browse,libraries,settings,complete}` accesibles sin auth tras primer install. `GET /setup/browse` lista filesystem (root-level blocklist solo bloquea `/etc /proc /sys /dev /boot /root /var/run`; `/home`, `/srv`, `/var/lib`, `/config`, las librerías de media son visibles).

**Fix** ([commit 7c9149d](commit/7c9149d)): helper `requireSetupActive(w, r)` en `SetupHandler` que devuelve `403 SETUP_COMPLETE` cuando `setup.NeedsSetup()` es false. Aplicado a `Browse`, `Capabilities`, `CreateLibraries`, `UpdateSettings`, `Complete`. `Status` queda público (el frontend lo necesita on-boot para decidir si redirigir al wizard).

`/api/v1/auth/setup` (creación del primer admin) ya tenía su propia comprobación `count > 0` → 403 `SETUP_COMPLETED`.

**Tests**: `TestSetupHandler_PostCompletion_*_403` cubre los 5 endpoints + `_Status_StillOpen` pinea el carve-out.

### P0-D — 147 strings sin tildes/eñes en `es.json`

**Síntoma**: visible literalmente en cada pantalla. "Contrasena", "Iniciar sesion", "Administracion", "Anadir a favoritos", "Programacion de las proximas 24h", "Mas como esta", "Leer mas", "Inténtalo", "Categorias", "Música"…

**Causa**: alguien transcribió strings con un teclado que descarta acentos y eñes. Audit exhaustivo identificó 147 entradas (no 78 como estimó el primer grep grueso).

**Fix** ([commit 5b47630](commit/5b47630)): script Python one-shot con dict de 147 mappings keypath → corrected. Validación JSON-OK al final. Idempotente. Script borrado tras uso (no commit).

### P0-E — Refresh tokens nunca rotaban + reuse-detection

**Síntoma**: `/auth/refresh` reusaba el mismo refresh token durante todo el `RefreshTokenTTL` (30 días). Un leak permanecía válido un mes sin detección.

**Fix dividido en 2 commits**:

1. [73418a9 — rotación simple](commit/73418a9): cada refresh genera un secret nuevo + UPDATE atómico de `refresh_token_hash`. El viejo muere al instante. Cap del leak window de 30 días a "hasta que el cliente legítimo refresque" (típicamente minutos). El UPDATE es hand-rolled porque sqlc 1.31.x trunca UPDATEs con 4+ placeholders (mismo bug que P0-B). `SessionRepository.RotateRefreshToken` añadido.

2. [97c6698 — reuse-detection con revocación de cadena](commit/97c6698): migración `038_session_previous_refresh_hash.sql` añade columna `previous_refresh_token_hash` + índice. `RotateRefreshToken` ahora hace `previous = refresh_token_hash, refresh = ?` en el mismo UPDATE (atomicidad). Nuevo método `GetByPreviousRefreshTokenHash`. En `auth.RefreshToken`: si el token miss el primary hash pero matchea el previous, **reuse detectado** → revoca toda la sesión + WARN log con session_id + user_id + ip. One-step memory (no full chain) — suficiente para el threat model "atacante leak + race vs dueño".

**Verificado e2e contra el container**:
```
POST /auth/login → RT0
POST /auth/refresh {RT0} → 200, RT1 ≠ RT0
POST /auth/refresh {RT0} (attacker) → 401 + WARN "reuse detected — revoking session"
POST /auth/refresh {RT1} (legit, post-revoke) → 401 (sesión muerta)
```

**Tests**: `TestService_RefreshToken_RotatesSecret` (well-behaved client) + `TestService_RefreshToken_ReuseDetection_RevokesSession` (e2e revoke chain) + `TestSessionRepository_RotateRefreshToken` (verifica `previous_refresh_token_hash` carry) + `TestSessionRepository_GetByPreviousRefreshTokenHash` (incluye empty-string guard).

## Mejoras UX adicionales (5 commits)

### Auto-play desde Continue Watching ([3be7dce](commit/3be7dce))

Click en card de "Seguir viendo" antes paraba en la página de detalle. Ahora arranca el player directamente. `LandscapeCard` gana prop opcional `autoPlay` que tagea el href con `?play=1`. `ItemDetail` ya tenía el deep-link consumer wireado pero las rails no lo usaban. Para episodios: link directo a `/items/${ep.id}?play=1` (un redirect menos vs ir al parent_id).

### Resume desde posición guardada + back inteligente ([ee6836c](commit/ee6836c))

Click en CW resumía desde 0. `handlePlay` ahora hace `api.getItem(playId)` y lee `user_data.progress.position_ticks` → pasa al `VideoPlayer` como `startPosition`. `/items/{id}` se eligió sobre `/me/progress/{id}` porque el shape de este último no matcheaba el tipo `UserData` declarado (la firma mentía).

Cuando el player se cierra después de un auto-play deep-link, en vez de dejar al usuario en la página fea del episodio individual:
- Episodio → `navigate(/items/${parent_id}, replace)` (lista de capítulos de la temporada).
- Movie / serie → `navigate(-1)` (de vuelta al home o de donde vino).

Manual play (Reproducir desde un detail page navegado a mano) no toca — se queda donde estaba.

### Botón atrás universal en TopBar ([f488ea0](commit/f488ea0))

ArrowLeft icon visible en cada página `pathname !== "/"`. Click → `navigate(-1)`. Edge case: si `window.history.state.idx === 0` (URL directa / bookmark / share-link), fallback a `navigate("/")` para no dejar al usuario varado.

### Continue Watching usa thumb (16:9 "miniatura") para movies ([6c046ce](commit/6c046ce))

El usuario señaló que existe un tipo de imagen llamado **"Miniatura"** en el modal de Gestionar imágenes (1000×562, 16:9 nativo). Es la imagen específica que TMDb / Fanart shipean para listing cards horizontales — distinta del backdrop wide y del poster vertical.

**Backend**:
- `ImageRepository.GetPrimaryURLs` ahora incluye `type='thumb'` en su SELECT.
- `ProgressHandler.ContinueWatching` emite `thumb_url` en cada entry.
- `ItemHandler.attachImages` también lo expone en `/items/{id}`.

**Frontend**:
- `MediaItem.thumb_url?: string | null` añadido al type.
- `LandscapeCard`: para movies prefiere `thumb_url > backdrop_url > poster_url`. Episodios siguen con `backdrop_url > poster_url` (el screencap ya es 16:9 nativo).
- `ContinueWatchingRail` colapsado a un único `LandscapeCard` por item — la rail vuelve a tener forma uniforme 16:9 (revierte el experimento intermedio en `d78f17d` que mezclaba PosterCard + LandscapeCard).

**Verificado**: `Juego de Ladrones (2018)` pasa de mostrar el backdrop wide → al thumb "DEN OF THIEVES" landscape, mismo shape que los screencaps de Daredevil.

## Hallazgo cross-cutting: bug del parser de sqlc 1.31.1

Visto **3 veces** en esta sesión:
1. ORDER BY con expresión + `COLLATE NOCASE` → `?;` espurio + truncamiento.
2. UPDATE con 4+ placeholders → trailing `?;` chopped del WHERE.
3. Comentario en el código generado (de antes de esta sesión) admite una tercera instancia.

Las dos queries afectadas pasaron a raw SQL (`ListProfilesForOwner`, `RotateRefreshToken`) con comentario cruzado documentando la causa. **Recomendación P1**: bumpar a sqlc ≥1.32 cuando salga; migrar las queries de vuelta si el bug se cierra.

## Estado del worktree

- **11 commits** en la rama `claude/relaxed-lederberg-48e836`, todos pushados a origin.
- PR #238 mergeada a `main` (`afae026`); `origin/main` ya sincronizado.
- Follow-up post-merge: `59dbad0` *fix(livetv): make ?channel=<id> deep-link idempotent against re-render churn* (PR #239, merge `aa01f14`) cierra el freeze de 45s+ al pinchar canales desde el rail "En directo ahora" del Home. Causa: el useEffect de deep-link tenía `openPlayer` en deps, que se rebuildeaba en cada render porque `channels` también lo hacía; en concurrent rendering React 19 entraba en spiral. Fix con `handledChannelRef` para idempotencia per-channel-id + eliminado el strip-on-not-found que perdía deep-links silenciosamente.
- Container `hubplay` corriendo `ghcr.io/alexzafra13/hubplay_demo:latest` (recién built, healthy) con todos los fixes.
- Suite de tests Go verde: `go test ./internal/db/... ./internal/auth/... ./internal/api/handlers/...` (golang:1.25 docker, ~55s total).
- `pnpm exec tsc --noEmit` clean en frontend.

## Lo que NO se hizo (recomendaciones P1 para próxima sesión)

Del audit inicial, items P1 que siguen abiertos:

**Streaming**:
- HDR / 10-bit decision + tone-mapping (decision.go ignora `BitDepth`/`ColorTransfer`/`Profile`).
- Subtítulos burn-in (PGS / DVDSUB / ASS no se queman, browser sin soporte nativo).
- Audio multichannel passthrough (siempre baja a `aac stereo`).
- Refactor del seek-coalesce (3 capas defensivas tras 3 commits seguidos de fixes en el área).
- `-force_key_frames` GOP-aligned en transcode args.
- Pipeline VAAPI fully-on-GPU (hoy descarga frames a sysmem para `scale=...`).

**Frontend**:
- Polling 5s/30s residual donde debería ser SSE (system stats, dashboard).
- `manualChunks` en vite.config.ts no aprovechado (`hls.js` ~400KB no se separa).
- Páginas grandes sin tests (Home, LiveTV, Search, Movies, Series, Collections).
- `node-vibrant` client-side por cada hero (Plex hace esto server-side y cachea).

**Infra/seguridad**:
- `RateLimitConfig.GlobalRPM` dead code (declarado en config.example.yaml, sin readers).
- YAML config persistido a `0644` (jwt_secret legible en hosts multi-usuario).
- CSRF fail-open cuando no hay session cookie + localhost en `AllowedOrigins`.
- CI sin `govulncheck`. Trivy pinned a `@master` (supply-chain).
- No connection cap en SSE.

**UX adicionales que el usuario mencionó pero no atacamos**:
1. Quitar item de "Seguir viendo" (botón X en hover).
2. Marcar como visto desde la rail.
3. Hero del home auto-play (botón "Reproducir" del banner aún va al detail).
4. Skip-intro/credits visible en player (backend listo, falta scan de fingerprinting).
5. Next-up overlay automático al terminar episodio (`nextUpInfo` ya existe).
6. Aurora colors aplicado al detail (memoria S291-S299 dejó parte hecha).

## Para retomar

```bash
git -C C:\Users\alex\Desktop\hubp\HubPlay_demo log --oneline -12
docker ps --filter name=hubplay
```

PRs ya cerrados: #238 (audit + 11 commits) y #239 (livetv deep-link fix). Ambos en `main`.

Las credenciales del setup local: `admin` / `admin123`.
