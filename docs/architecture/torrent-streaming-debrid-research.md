# Investigación: experiencia tipo Torrentio/Stremio sobre Prowlarr (+ Sonarr/Radarr + debrid)

> **Estado:** investigación / diseño previo. **No implementado.** Documento
> de trabajo para decidir el rumbo antes de tocar código.
> Fecha: 2026-06-16. Rama: `claude/dual-indexers-search-r9kv4a`.

---

## 0. TL;DR

- Lo que hoy falla (todos los títulos → "No se encontraron fuentes") tiene
  **dos causas concretas** en el código actual; ver §4.
- "Como Torrentio" tiene **dos mitades** que conviene no mezclar:
  1. **Resolución de fuentes** (qué torrents existen para *este* título) →
     hoy lo harías con **Prowlarr** (tus indexadores). Es el equivalente al
     scraping que hace Torrentio, pero con *tus* indexadores.
  2. **Reproducción instantánea** (poder darle a play sin esperar al
     torrent) → **esto NO lo da el indexador, lo da el debrid.** Es la pieza
     que falta en HubPlay y la que hace que Torrentio "se sienta" instantáneo.
- **Sonarr/Radarr + cliente de descargas son OTRA cosa**: automatización de
  *biblioteca permanente* (descargar y quedarte el fichero, con perfiles de
  calidad). Para el flujo "ver ya" no son necesarios; encajan en el camino
  "Descargar a la biblioteca" que HubPlay ya tiene. Ver §3 y §6.
- **Restricción dura nueva (2024-2026):** Real-Debrid **eliminó el endpoint
  `instantAvailability`** (nov-2024) y aplica filtros de contenido por
  palabra clave (may-2026). Ya no se puede marcar "cacheado" de forma fiable
  con RD. **TorBox** sí tiene `checkcached`. Esto condiciona el diseño. Ver §5.
- **Multi-IP:** los debrid banean cuentas usadas desde varias IP a la vez. La
  solución estándar es **proxear el stream a través del servidor** (el debrid
  solo ve la IP de HubPlay). HubPlay ya sirve streams por el backend, así que
  encaja bien. Ver §5.4.
- Recomendación: plan por fases (§9).

> **🟢 DECISIÓN (2026-06-16): se hace SIN debrid.** El resolver de
> reproducción es el **motor P2P propio** (`anacrolix/torrent`, ya integrado),
> no un servicio debrid. La sección debrid de abajo se conserva como
> referencia / enchufe opcional futuro. Lo que aplica al trabajo real es la
> **§8.bis (enfoque sin debrid)** y el **plan §9**.

---

## 1. Los dos modelos (y por qué se confunden)

| | **Torrentio + Stremio + debrid** | **Prowlarr + Sonarr/Radarr + cliente** |
|---|---|---|
| Objetivo | Ver *ahora*, efímero | Construir una biblioteca permanente |
| Resolución | Scraper propio indexado por IMDb | Indexadores Torznab (los tuyos) |
| Reproducción | Link directo del debrid (instantáneo si está cacheado) o P2P | Descarga completa a disco, luego se reproduce desde la biblioteca |
| Estado del fichero | No se guarda (vive en el debrid) | Se queda en tu almacenamiento |
| Decisión de calidad | El usuario elige en una lista ordenada | Perfiles de calidad automáticos (TRaSH) |
| Rol de HubPlay | = Stremio (reproductor) + addon | = el "media server" (Jellyfin/Plex) |

**Lo importante:** el usuario quiere la **experiencia de la columna izquierda
(instantánea, estilo Torrentio)** pero alimentada por **sus indexadores
(Prowlarr, columna derecha)**. Eso es perfectamente posible y es justo lo que
hacen agregadores modernos como **AIOStreams**, **Comet** o **MediaFusion**:
combinan fuentes (Prowlarr/Jackett/Zilean/Torrentio…) + un debrid + un proxy
de streaming, y exponen una lista filtrada/ordenada.

`[Torrentio]` resuelve por su cuenta y "se siente" instantáneo porque la
mayoría de la gente lo usa **con un debrid**. Sin debrid, Torrentio cae a P2P
puro: arranque lento, dependes de seeders y expones tu IP al *swarm*.

---

## 2. Cómo funciona Torrentio + Stremio + debrid (mecánica real)

1. **Stremio** pide "streams" a un addon vía el protocolo de addons
   (`/stream/{type}/{id}.json`, donde `id` es `tt1234567` o
   `tt1234567:1:2` para serie temporada 1 episodio 2).
2. El **addon (Torrentio)** devuelve una lista de fuentes (magnets /
   infohashes) con metadatos parseados del nombre (resolución, códec,
   idiomas, seeders…).
3. Si hay **debrid configurado**, el addon (o un proxy) hace, por cada
   infohash:
   - `checkCached(hashes)` → marca cuáles están ya cacheados (`[RD+]`).
   - Para reproducir: `addMagnet` → `selectFiles` → esperar → `unrestrict` →
     **URL HTTPS directa** que Stremio reproduce como si fuera un MP4 normal.
4. Si el torrent **ya está cacheado** en el debrid, los pasos anteriores son
   casi instantáneos (el fichero ya está en la nube del debrid → CDN). Si
   **no** está cacheado, el debrid lo descarga primero (minutos u horas) y
   luego sirve el link.

> La "magia instantánea" = **cache del debrid**. El indexador solo aporta el
> infohash. El debrid aporta la reproducción rápida y sin exponer tu IP.

---

## 3. Cómo funciona Prowlarr + Sonarr/Radarr + cliente (el otro modelo)

- **Prowlarr**: agregador de indexadores. Expone una API Torznab estándar y
  además una **API nativa de búsqueda** (`/api/v1/search`). Sincroniza
  indexadores hacia las apps *arr.
- **Sonarr/Radarr**: PVR de series/películas. Mantienen una lista de
  "wanted", buscan en los indexadores (vía Prowlarr), eligen el release según
  **perfiles de calidad** (HD-1080p, TRaSH custom formats…) y se lo pasan a un
  **cliente de descargas**.
- **Cliente de descargas** (qBittorrent, Transmission, SABnzbd…): baja el
  fichero. Cuando termina, Sonarr/Radarr lo renombran y lo ordenan en la
  biblioteca.

**Conclusión para HubPlay:** este modelo es para *biblioteca permanente*, no
para "ver ya". HubPlay **ya es** el media server (escanea biblioteca, sirve,
transcodifica). Por tanto:

- Para el **streaming instantáneo**, Sonarr/Radarr **sobran**: usas Prowlarr
  (búsqueda) + debrid (reproducción) directamente.
- Sonarr/Radarr/cliente son una integración **opcional** para el camino
  "Descargar a la biblioteca" (que HubPlay ya tiene como botón, hoy resuelto
  con su propio motor torrent). Se podría delegar a qBittorrent o a las *arr
  si el usuario ya las tiene, pero es trabajo aparte y de menor prioridad.

> Recomendación: no acoplar el flujo instantáneo a Sonarr/Radarr. Mantenerlas
> como integración secundaria del camino "descargar y quedarse".

---

## 4. Por qué hoy "todos los títulos" dan "sin fuentes" (bug actual)

Confirmado leyendo el código. La pantalla de la captura es el modal de
`web/src/pages/Archive.tsx` (`SourcesModal`) → `useDiscoverSources` →
`GET /torrent/discover/sources` → backend resuelve el IMDb id y consulta los
indexadores **solo por `imdbid=`**.

1. **`buildTorznabURL` manda casi siempre solo `imdbid` + `cat`**
   (`internal/torrentstream/torznab.go:294-339`). La **mayoría de
   indexadores no soportan búsqueda por IMDb id** y Prowlarr devuelve un feed
   vacío para ellos → 0 resultados para casi cualquier título.
2. **El backend ya tiene título y año pero los descarta.** En
   `DiscoverSources` (`internal/api/handlers/torrent/torrent.go:299-312`) se
   hace `FetchMetadata` (hay `meta.Title`, `meta.Year`), pero solo se pasa el
   `imdbid` a `Sources()`. La búsqueda por texto **solo se usa cuando NO hay
   imdb id**, que casi nunca ocurre.
3. **Series: nunca se manda `season`/`ep`.** `t=tvsearch` va sin temporada ni
   episodio, así que incluso los indexadores que soportan tvsearch responden
   mal.

Esto es independiente de añadir debrid: aunque pongas debrid, si la
resolución de fuentes devuelve vacío no hay nada que reproducir. Por eso la
**Fase 0** (arreglar la búsqueda) es prerrequisito.

---

## 5. La pieza que falta: el debrid

### 5.1. El cambio de 2024-2026 (crítico para el diseño)

- **Real-Debrid eliminó `/torrents/instantAvailability` en nov-2024** (por
  presión antipiratería). Era *el* endpoint que usaban todos los addons para
  saber si un hash estaba cacheado. Ya no existe.
- Antes (jul-2024) ya había cambiado el formato para hashes no cacheados.
- Desde **may-2026**, RD aplica un **filtro por palabra clave** que bloquea
  ficheros cacheados cuyo nombre contiene tags de release comunes → errores
  "File was removed due to copyright infringement".
- RD también **bajó los descargas activas simultáneas** (de 42 a ~6).

**Implicación de diseño:** no podemos depender de un "check cached" universal.
Estrategia realista:
- **TorBox** sí ofrece `/torrents/checkcached` → con TorBox sí podemos pintar
  badges "cacheado/instantáneo".
- Para RD/AllDebrid, dos opciones: (a) no prometer "cacheado", simplemente
  intentar resolver y mostrar progreso si toca descargar; (b) usar una fuente
  externa de hashes cacheados conocida, p.ej. **Zilean** (indexa las
  *hashlists* públicas de Debrid Media Manager). Zilean es además un indexador
  más para Prowlarr.

### 5.2. Panorama de servicios (jun-2026)

| Servicio | Cache check | Notas |
|---|---|---|
| **Real-Debrid** | ❌ (deprecado nov-2024) | Mayor cache histórica, pero filtros antipiratería desde may-2026; límite de descargas activas bajo |
| **TorBox** | ✅ `checkcached` | API moderna y dev-friendly, sin logs, selección de CDN; cache ya iguala a RD en mainstream |
| **AllDebrid** | parcial | 70+ hosts, barato |
| **Premiumize** | ✅ | Cloud + VPN, más caro |
| EasyDebrid, Debrid-Link, put.io, Offcloud, Seedr, PikPak | varía | soportados por AIOStreams |

> Recomendación de proveedor para **empezar**: **TorBox** (porque
> `checkcached` sigue vivo → mejor UX de "instantáneo") o **Real-Debrid**
> (mayor cache, pero sin badge fiable de cacheado). Diseñar tras una
> **interfaz** para no acoplarse a ninguno (§6.2).

### 5.3. Flujo de endpoints (ejemplo Real-Debrid)

```
1. POST /torrents/addMagnet     (magnet)        -> torrent_id
2. GET  /torrents/info/{id}                       -> ficheros + estado
   estados: magnet_conversion -> waiting_files_selection -> queued
3. POST /torrents/selectFiles/{id}  (fileIDs)     -> inicia descarga
4. (poll) GET /torrents/info/{id}                 -> downloading -> downloaded -> links[]
5. POST /unrestrict/link        (link)            -> URL HTTPS directa
```

TorBox es análogo: `checkcached` → `createtorrent` → `requestdownloadlink`
(el link se abre 3h). El patrón general es **el mismo**: del magnet/infohash a
una URL HTTPS directa, con un paso de "selección de fichero" (elegir el de
vídeo más grande / el episodio correcto).

### 5.4. Multi-IP y proxy de stream (importante)

Los debrid **banean cuentas usadas desde varias IP simultáneamente**. Si
HubPlay resuelve la URL del debrid y el **navegador** la descarga directamente,
el debrid ve la IP del cliente (distinta de la del servidor) → riesgo de ban,
y además rompe con varios dispositivos a la vez.

**Solución estándar (MediaFlow Proxy / StremThru / AIOStreams):** **proxear el
stream a través del servidor**. HubPlay pide la URL del debrid y **reenvía los
bytes** al cliente (con soporte de `Range` para seeking). Así el debrid solo
ve **una IP** (la de HubPlay) y el control de `Content-Type`/SSRF queda en el
backend.

Buena noticia: **HubPlay ya hace esto** para otros casos (sirve el stream
torrent por el handler, tiene transmux-proxy IPTV con passthrough Range y
`imaging.SafeGet` SSRF-safe). El proxy de debrid reutiliza ese patrón.

---

## 6. Encaje en la arquitectura de HubPlay

### 6.1. Qué ya tienes (buena base)

- `internal/torrentstream/`:
  - `SearchResult` ya es un **modelo unificado** con `InfoHash`, `MagnetURI`,
    `Seeders`, `Quality`, `Resolution`, `Codec`, `Languages`, `QualityScore`,
    `IMDbID`. Sirve igual para Prowlarr y para debrid.
  - `TorznabClient` (búsqueda), `SourceService` (caché + curación),
    `Curate`/`filter_engine.go` (**filtrado/orden/dedupe** — equivalente al
    pipeline de AIOStreams), `metadata_parser.go` (parseo de títulos).
  - `Manager`: **motor P2P secuencial** propio (el camino "sin debrid").
- `internal/api/handlers/torrent/`: `/torrent/sources/*`, `/discover`,
  `/stream` (con `ServeContent` → Range), gate admin para "arrancar descarga".
- Config: `TorrentConfig`/`TorznabConfig` ya soporta Prowlarr (`base_url`) y
  Jackett (`url`), categorías por tipo, OFF por defecto, override por env.
- Frontend: `Archive.tsx` (grid de carátulas → modal de fuentes),
  `SourceList.tsx`, hooks `useMediaSources`/`useDiscoverSources`.

### 6.2. Qué hay que añadir

```
                       ┌─────────────────────────────────────────┐
                       │                HubPlay                   │
   IMDb id / título    │                                          │
  ───────────────────► │  SourceResolver                          │
                       │   ├─ Prowlarr (API nativa /api/v1/search)│──► tus indexadores
                       │   ├─ Torznab feeds (compat actual)       │
                       │   └─ (opc.) Zilean (hashes cacheados)    │
                       │            │ []SearchResult (infohash…)  │
                       │            ▼                              │
                       │  Curate (filtro/orden/dedupe) [ya existe]│
                       │            │                             │
                       │            ▼                             │
                       │  Debrid (interfaz)         [NUEVO]       │
                       │   ├─ CheckCached(hashes)                 │──► TorBox / RD / AD…
                       │   └─ Resolve(infohash) -> URL directa    │
                       │            │                             │
                       │            ▼                             │
                       │  StreamProxy (Range, SSRF-safe) [NUEVO/  │──► navegador
                       │            reusa patrón IPTV transmux]   │
                       │                                          │
                       │  (opcional) P2P Manager  [ya existe]     │  fallback sin debrid
                       └─────────────────────────────────────────┘
```

Piezas nuevas concretas:

1. **`Debrid` interface** (paquete `internal/debrid` o dentro de
   `torrentstream`):
   ```go
   type Provider interface {
       Name() string
       // CheckCached marca qué infohashes están listos para play instantáneo.
       // Puede devolver "desconocido" si el proveedor no lo soporta (RD).
       CheckCached(ctx, hashes []string) (map[string]bool, error)
       // Resolve lleva de un infohash/magnet a una URL HTTPS directa,
       // eligiendo el fichero correcto (peli / SxxEyy).
       Resolve(ctx, magnetOrHash string, pick FileSelector) (DirectLink, error)
   }
   ```
   Implementaciones: `realdebrid`, `torbox`, `alldebrid`, `premiumize`.
   Config nueva: `torrent.debrid.{provider, api_key}` (y, fase 2, por usuario).

2. **Proxy de stream debrid**: handler que recibe la fuente elegida, llama a
   `Resolve`, y **reenvía** la URL directa con `Range`. Reutiliza
   `imaging.SafeGet`/el patrón de transmux IPTV. Sustituye/añade junto a
   `/torrent/stream` un `/torrent/stream/debrid`.

3. **Resolución de fuentes mejorada** (Fase 0, ver §4): imdbid **+** título+año
   (pelis) / título+`SxxEyy` (series), fusionado y deduplicado. Idealmente
   migrar a la **API nativa de Prowlarr** (`/api/v1/search?query=&type=&
   categories=&indexerIds=`), que agrega indexadores y devuelve JSON con
   infohash/seeders/size — más limpio que enumerar feeds Torznab por
   indexador. Mantener Torznab como compat.

4. **UI**: badges "⚡ instantáneo (cacheado)" vs "⬇ requiere descarga", filtros
   por resolución/idioma/códec (ya tienes la metadata), y selección de
   episodio para series.

### 6.3. Detección de cacheados (resumen de decisión)

- TorBox → `checkcached` directo (badge fiable).
- RD/AllDebrid → sin check fiable: o no prometer cacheado, o cruzar con
  **Zilean** (hashlists DMM) como señal aproximada.

---

## 7. Decisiones de diseño y trade-offs

1. **Debrid como interfaz, no acoplado a un proveedor.** RD puede cambiar
   reglas en cualquier momento (ya lo hizo); TorBox hoy es más dev-friendly.
   Una interfaz permite cambiar/añadir sin tocar el resto.
2. **Proxy de stream por el servidor (recomendado).** Evita baneos multi-IP y
   centraliza SSRF/Content-Type. Coste: ancho de banda del servidor (igual que
   el transmux IPTV que ya haces).
3. **P2P propio como fallback "sin debrid".** Ya existe. Mantenerlo para
   quien no tenga cuenta debrid, dejando claro en la UI que es más lento.
4. **Sonarr/Radarr fuera del flujo instantáneo.** Integración secundaria solo
   para "descargar a biblioteca"; no bloquear la Fase 1 con esto.
5. **Claves debrid por usuario vs globales.** Empezar global (operador). En
   fase 2, por usuario (cada quien su cuenta) para no compartir cuota/IP.
6. **Caché de resoluciones.** Ya existe TTL en `SourceService`; el
   `CheckCached` conviene cachearlo aparte (TTL corto) porque cambia más.

---

## 8. Postura legal / del proyecto (a decidir explícitamente)

El código actual es **deliberadamente conservador**: el buscador integrado
solo consulta Internet Archive (catálogo legal) y los comentarios subrayan
"no incluye scrapers de terceros" y "lo que el operador apunte en Torznab es
su responsabilidad". Añadir **resolución por Prowlarr + debrid** mueve el
proyecto hacia terreno **dual-use**: el operador apunta a *sus* indexadores y
usa *su* cuenta debrid, igual que el stack *arr o cualquier addon de Stremio.

Recomendación para mantener la postura coherente:

- Mantener todo **opt-in y OFF por defecto** (como `torrent.enabled` hoy).
- El operador aporta indexadores y clave debrid (HubPlay no trae ninguno).
- Conservar el framing de "responsabilidad del operador" en config y docs.
- No empaquetar listas de indexadores ni scrapers de terceros en el binario.

Es una decisión de producto del dueño del proyecto; este documento solo la
deja explícita.

---

## 8.bis. Enfoque ELEGIDO: sin debrid (motor P2P como resolver)

La experiencia tipo Torrentio se construye **sobre el motor P2P propio** que
HubPlay ya tiene (`internal/torrentstream`, `anacrolix/torrent v1.61.0`). No
se integra ningún debrid. Esto es más simple, sin dependencias externas, sin
baneos multi-IP, sin la volatilidad de RD, y mantiene la postura legal más
limpia (fuentes propias del operador).

### Lo que ya existe (≈70% hecho)
- **Streaming secuencial mientras descarga**: `Session.Reader()` →
  `NewReader()` + `SetReadahead(16MiB)` + `SetResponsive()`; sirve por HTTP
  con `Range`/seeking (`session.go`). Es el patrón correcto.
- **Sesión compartida entre viewers** (una descarga sirve a todos):
  `GetActive`/`bySrc`/`holders` (`manager.go`). Es la "cache" casera.
- SSRF-guard, magnet + `.torrent`, reaper por idle, cap de sesiones,
  limpieza de scratch, jobs de "descargar a biblioteca".
- **Privacidad**: el peer del swarm es el **servidor**, no el navegador → la
  IP del usuario final nunca toca el torrent. (Contrapartida: la IP del
  operador sí — más exposición legal para quien lo despliega.)

### El techo honesto (física del P2P)
Sin una cache que no controlas (la del debrid), el arranque está limitado:
- bien sembrado + ya caliente en el servidor → arranca en segundos;
- frío / pocos seeders / 4K HEVC a transcodificar → buffer perceptible + CPU.
  No hay forma de evitarlo sin cache. Hay que **gestionar la expectativa**.

### Los 4 gaps reales (el plumbing del torrent NO es el problema)
1. **🔴 Transcode/transmux.** Hoy `guessContentType` tiene el TODO: MKV/AVI/TS
   "querrían un remux (futuro, reusando el path ffmpeg de IPTV)". La mayoría
   de torrents son MKV/HEVC/AC3 → **hoy se ven en negro / sin audio** en el
   navegador. Hay que meter `Session.Reader()` por el `internal/stream.Decide`
   existente (direct-play / transcode), con ffmpeg leyendo del propio
   `/torrent/stream` (que bloquea hasta que llegan las piezas), igual que el
   transmux IPTV. **Sin esto, "salen fuentes" pero no se reproducen.**
2. **🔴 Selección consciente del códec = el "¿cacheado?" de este mundo.** La
   señal nº1 de pick deja de ser "cached" y pasa a ser **"direct-play +
   seeders"**: un 1080p x264 MP4 con 500 seeders gana a un 4K HEVC con 3
   seeders, aunque el `QualityScore` diga lo contrario. Hay que **reordenar
   `Curate` para modo P2P** (seeders/direct-play por encima de resolución).
   La metadata ya se parsea; solo falta usarla para elegir.
3. **🟠 Latencia de arranque.** (a) magnet→metadata por DHT (hasta 60s):
   **preferir el `.torrent` que Prowlarr sirve** sobre el magnet. (b) buffer
   inicial: **pre-warm** (arrancar el top source al abrir la ficha).
4. **🟠 Cache caliente.** Hoy el reaper borra el scratch al quedar idle →
   re-ver = re-descargar. Una **LRU en disco** (con cuota) deja lo reciente
   caliente → re-play instantáneo y lo popular caliente para todos.

> La interfaz `Debrid` (§6.2) queda como **enchufe opcional futuro**, no se
> construye ahora.

---

## 9. Plan por fases (enfoque SIN debrid)

**P0 — Arreglar la resolución de fuentes (sin deps nuevas). ← EN CURSO**
- Búsqueda híbrida: `imdbid` (preciso, para indexadores que lo soportan) **+**
  texto `título+año` (pelis) / `título SxxEyy` (series), fusionado + dedupe.
- **Verificación de match** (título/año/episodio) sobre los resultados del
  pase de texto, para matar falsos positivos (remakes, títulos parecidos).
- Pasar título/año/temporada/episodio desde el handler hasta la query.
- Resultado: las fuentes **aparecen y son las correctas** (resuelve la
  captura). Independiente del resolver de playback.

**P1 — Transcode wiring + selección por códec (que de verdad se reproduzca).**
- ✅ **P1a**: `Curate` reordenado para modo P2P (web-playable + seeders).
- ✅ **P1b-1**: gestor VOD propio (`VODTransmux`) — ffprobe del fichero por un
  loopback interno → decisión `DecidePlayMode` (direct / remux / reencode) →
  **remux `-c copy` a HLS** para H.264-en-MKV (audio a AAC si hace falta);
  endpoints `/torrent/play` + `/torrent/hls/*`; player con hls.js.
- ✅ **P1b-2**: reencode HEVC/AC3/AV1/XviD → H.264+AAC a HLS, con el mismo
  encoder hardware que detecta `stream.DetectHWAccel` (VAAPI/NVENC/QSV/
  VideoToolbox; libx264 si no hay). Opt-out `torrent.disable_reencode` para
  hosts flojos. `/torrent/play` ya devuelve `hls` también para reencode.

**P2 — Latencia.**
- Preferir `.torrent` sobre magnet; pre-warm del top source al abrir ficha.

**P3 — Cache LRU en disco.**
- Retener lo reciente (con cuota) en vez de borrar siempre el scratch →
  re-play instantáneo; lo popular sigue caliente para todos.

**P4 — (opcional, futuro) Debrid como enchufe.**
- Solo si en el futuro se quiere reproducción instantánea para contenido no
  cacheado/poco sembrado. Interfaz `Debrid` + proxy de stream (§5–§6).

---

## 10. Riesgos y preguntas abiertas

- **Volatilidad del debrid (RD).** Cambios de API/reglas frecuentes. Mitiga:
  interfaz + empezar por TorBox.
- **Detección de cacheados no universal.** Decidir si prometemos "instantáneo"
  o solo "intentar y mostrar progreso".
- **Ancho de banda del servidor** si se proxea todo el stream. Igual que el
  transmux IPTV actual; medir.
- **Selección de fichero en series** (elegir el episodio correcto dentro de un
  pack). Necesita lógica de matching `SxxEyy`.
- **Decisión de postura legal/producto** (§8) — del dueño del proyecto.

**Preguntas para el dueño antes de Fase 1:**
1. ¿Proveedor debrid inicial: TorBox (mejor "cacheado") o Real-Debrid (mayor
   cache)?
2. ¿Claves debrid globales (operador) o por usuario desde el principio?
3. ¿Proxear el stream por el servidor (recomendado) o redirigir el navegador?
4. ¿Migramos la búsqueda a la API nativa de Prowlarr o mantenemos Torznab?

---

## 11. Fuentes

- Torrentio + Stremio + debrid (cómo funciona): arnav.au, StreamStack, troypoint.
- Real-Debrid API (addMagnet/selectFiles/info/unrestrict): api.real-debrid.com,
  pkg.go.dev/github.com/deflix-tv/go-debrid/realdebrid.
- Deprecación `instantAvailability` (nov-2024) + filtros (2026): rdt-client
  issue #545, TorrentFreak, ElfHosted blog "Stremio after RealDebrid".
- Comparativa debrid 2026 (RD/TorBox/AllDebrid/Premiumize): factually.co,
  github.com/fynks/debrid-services-comparison, troypoint.
- TorBox API (checkcached/createtorrent/requestdownloadlink): api-docs.torbox.app,
  github.com/TorBox-App/torbox-sdk-py.
- AIOStreams / StremThru / MediaFlow (agregación + proxy + multi-IP):
  github.com/Viren070/AIOStreams, docs.elfhosted.com.
- Prowlarr API nativa de búsqueda `/api/v1/search`: wiki.servarr.com/prowlarr/search,
  Prowlarr issue #2440.
- Stremio addon protocol (stream/manifest): github.com/Stremio/stremio-addon-sdk.
- Zilean / DMM hashlists (cacheados): guides.viren070.me, github (stremio-stack).
- Rol *arr (Sonarr/Radarr/Prowlarr): wiki.servarr.com, bytesized-hosting,
  homelabstarter.
</content>
</invoke>
