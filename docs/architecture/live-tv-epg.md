# Live TV, IPTV & EPG — Design Document

## Overview

Soporte nativo de IPTV para ver canales de TV en directo dentro de HubPlay. El usuario proporciona una URL M3U (de proveedores IPTV legales o canales públicos TDT) y opcionalmente una URL EPG (guía de programación en formato XMLTV).

**Plug-and-play**: pegar la URL, esperar 10 segundos, empezar a ver tele.

---

## 1. Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                      Channel Manager                          │
│  (Orchestrates playlists, EPG, proxying, health)             │
├────────────┬────────────┬──────────────┬─────────────────────┤
│ M3U Parser │ EPG Parser │ Stream Proxy │ Health Checker      │
│ (playlist) │ (XMLTV)    │ (relay)      │ (periodic ping)     │
├────────────┴────────────┴──────────────┴─────────────────────┤
│                    Repository Layer                            │
│          channels  •  epg_programs  •  user_data              │
├──────────────────────────────────────────────────────────────┤
│                    SQLite / PostgreSQL                         │
└──────────────────────────────────────────────────────────────┘
```

---

## 2. M3U Playlist Parsing

### Formato soportado

```m3u
#EXTM3U url-tvg="https://provider.com/epg.xml" x-tvg-url="https://..."
#EXTINF:-1 tvg-id="la1.es" tvg-name="La 1" tvg-logo="https://logo.png" group-title="Nacionales" tvg-language="Spanish" tvg-country="ES",La 1 HD
http://stream-url/la1.m3u8
#EXTINF:-1 tvg-id="a3.es" tvg-name="Antena 3" tvg-logo="https://logo2.png" group-title="Nacionales",Antena 3 HD
http://stream-url/antena3.m3u8
```

### Atributos extraídos

| Atributo M3U | Campo DB | Uso |
|---|---|---|
| `tvg-id` | `channels.tvg_id` | Matching con EPG (clave principal de enlace) |
| `tvg-name` | `channels.name` | Nombre del canal |
| `tvg-logo` | `channels.logo_url` | Logo en la UI |
| `group-title` | `channels.group_name` | Categoría: "Nacionales", "Deportes", etc. |
| `tvg-language` | `channels.language` | Idioma del canal |
| `tvg-country` | `channels.country` | País |
| URL de stream | `channels.stream_url` | URL fuente del stream |
| `url-tvg` (cabecera) | `libraries.epg_url` | Auto-detect EPG URL si no se proporcionó |

### Parser

```go
type M3UParser interface {
    // Parse an M3U playlist from a reader (file or HTTP response body)
    Parse(ctx context.Context, r io.Reader) (*Playlist, error)
}

type Playlist struct {
    EPGUrl   string     // from #EXTM3U header (url-tvg or x-tvg-url)
    Channels []M3UChannel
}

type M3UChannel struct {
    Name      string
    TvgID     string
    TvgName   string
    LogoURL   string
    Group     string
    Language  string
    Country   string
    StreamURL string
    // Extended attributes (catch-all for non-standard attrs)
    Extras    map[string]string
}
```

### Edge cases

| Caso | Manejo |
|---|---|
| URL M3U devuelve error HTTP | Retry 3x con backoff (2s, 4s, 8s), luego marcar error en UI |
| Canales duplicados (mismo `tvg-id`) | Último gana (overwrite), log warning |
| Sin `tvg-id` | Generar ID desde nombre normalizado (`slugify(name)`) |
| Stream URL con token expirable | Auto-refresh playlist (configurable, default 24h) |
| M3U muy grande (10K+ canales) | Parse en streaming (no cargar todo en memoria), insertar en batches de 500 |
| Encoding no UTF-8 | Detectar charset, convertir a UTF-8 |

### Refresh automático

```
Playlist refresh timer (default: 24h)
    │
    ├─ 1. Descargar nueva versión del M3U
    ├─ 2. Diff con canales existentes en DB
    │     ├─ Nuevos → INSERT + emit channel.added
    │     ├─ Eliminados → marcar is_active=false + emit channel.removed
    │     └─ Modificados (URL, logo) → UPDATE
    ├─ 3. Emit playlist.refreshed event
    └─ 4. Log resultado: "Playlist refreshed: +12 new, -3 removed, 5 updated"
```

---

## 3. EPG — Electronic Program Guide

### Formato: XMLTV

Estándar de la industria para guías de programación TV. XML con canales y programas.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<tv generator-info-name="EPG Provider">
  <channel id="la1.es">
    <display-name lang="es">La 1</display-name>
    <icon src="https://logo.png"/>
  </channel>
  <programme start="20260313200000 +0100" stop="20260313210000 +0100" channel="la1.es">
    <title lang="es">Telediario</title>
    <desc lang="es">Noticias del día con los corresponsales...</desc>
    <category lang="es">Noticias</category>
    <icon src="https://thumb.png"/>
    <episode-num system="onscreen">S1E234</episode-num>
    <sub-title lang="es">Edición de las 20:00</sub-title>
    <date>20260313</date>
    <rating system="MPAA">
      <value>TV-G</value>
    </rating>
  </programme>
</tv>
```

### Parser

```go
type EPGParser interface {
    // Parse XMLTV data from a reader
    Parse(ctx context.Context, r io.Reader) (*EPGData, error)
}

type EPGData struct {
    Channels []EPGChannel
    Programs []EPGProgram
}

type EPGChannel struct {
    ID          string   // matches tvg-id from M3U
    DisplayName string
    IconURL     string
}

type EPGProgram struct {
    ChannelID   string   // maps to channel via tvg-id
    Title       string
    Subtitle    string   // episode title
    Description string
    Category    string
    IconURL     string
    Start       time.Time
    End         time.Time
    EpisodeNum  string   // "S1E234" or "onscreen" format
    Rating      string
}
```

### Channel ↔ EPG Matching

El enlace entre canales M3U y programas EPG se hace por `tvg-id`:

```
M3U: tvg-id="la1.es"  ←──→  XMLTV: <programme channel="la1.es">
```

Si no hay match exacto:
1. Intentar match case-insensitive
2. Intentar match fuzzy por nombre del canal (Levenshtein distance < 3)
3. Si no hay match → canal sin EPG (funciona, pero sin guía de programación)

El admin puede hacer matching manual desde la UI:

```
/admin/iptv/channel-mapping
┌─────────────────┬─────────────────────────┐
│ Canal M3U       │ Canal EPG asignado       │
├─────────────────┼─────────────────────────┤
│ La 1 HD         │ [la1.es ▼] ✓ Auto       │
│ Antena 3 HD     │ [a3.es ▼] ✓ Auto        │
│ Canal+ Deportes │ [? ▼] ⚠ Sin match       │
└─────────────────┴─────────────────────────┘
```

### EPG Data Management

```go
type EPGManager interface {
    // Load/refresh EPG data from URL
    LoadEPG(ctx context.Context, libraryID uuid.UUID, epgURL string) error

    // Get current program for a channel
    GetNow(ctx context.Context, channelID uuid.UUID) (*EPGProgram, error)

    // Get upcoming program
    GetNext(ctx context.Context, channelID uuid.UUID) (*EPGProgram, error)

    // Get schedule for a time range (EPG grid view)
    GetSchedule(ctx context.Context, channelID uuid.UUID, from, to time.Time) ([]EPGProgram, error)

    // Get schedule for ALL channels in a library (batch, for EPG grid)
    GetBulkSchedule(ctx context.Context, libraryID uuid.UUID, from, to time.Time) (map[uuid.UUID][]EPGProgram, error)

    // Search programs by title
    SearchPrograms(ctx context.Context, query string, from, to time.Time) ([]EPGProgram, error)
}
```

### Almacenamiento y limpieza

| Aspecto | Estrategia |
|---|---|
| Ventana de datos | Almacenar EPG de las últimas 24h + próximas 72h (configurable) |
| Datos antiguos | Limpiar programas con `end_time < now - 24h` cada 6h |
| Cache comprimido | Guardar XML parseado en gob/gzip en disco para evitar re-descarga en restart |
| Tamaño típico | ~5-20MB XML para 500 canales × 7 días = ~50K programas |
| Refresh | Cada 6h (configurable). XMLTV suele actualizarse cada 6-12h |

### EPG Refresh Flow

```
EPG refresh timer (default: 6h)
    │
    ├─ 1. HTTP GET epg_url (con If-Modified-Since si el servidor soporta)
    │     └─ Si 304 Not Modified → skip, next refresh en 6h
    ├─ 2. Parse XML (streaming SAX parser para archivos grandes)
    ├─ 3. Filtrar: solo programas dentro de ventana [-24h, +72h]
    ├─ 4. Upsert en DB (REPLACE INTO por canal+start_time)
    ├─ 5. Limpiar programas expirados
    ├─ 6. Emit epg.updated event → frontend recarga guía
    └─ 7. Cache XML comprimido en disco
```

---

## 4. Stream Proxy

HubPlay actúa como proxy entre el proveedor IPTV y el cliente. El stream nunca va directo del proveedor al navegador.

### ¿Por qué proxy?

| Razón | Detalle |
|---|---|
| **Auth unificada** | El usuario se autentica con HubPlay, no necesita credenciales del proveedor |
| **CORS** | Streams IPTV no tienen cabeceras CORS, el navegador los bloquearía |
| **Monitorización** | Detectar streams caídos, latencia, bitrate |
| **Reconexión** | Si el stream fuente se corta, HubPlay reintenta sin que el cliente lo note |
| **Abstracción** | El frontend siempre usa el mismo formato de URL (`/api/v1/channels/{id}/stream`) |

### Endpoint

```
GET /api/v1/channels/{channelId}/stream
Authorization: Bearer <token>

Response: chunked transfer-encoding, Content-Type: video/mp2t (o application/x-mpegURL)
```

### Proxy Architecture

```go
type StreamProxy interface {
    // Start proxying a channel's stream to the writer
    ProxyStream(ctx context.Context, channelID uuid.UUID, w http.ResponseWriter) error

    // Get proxy stats
    Stats(channelID uuid.UUID) *ProxyStats
}

type ProxyStats struct {
    ActiveViewers  int
    BytesRelayed   int64
    Uptime         time.Duration
    Reconnections  int
    SourceBitrate  int64   // estimated from bytes/sec
    SourceCodec    string  // detected from stream headers
}
```

### Reconexión automática

```
Source stream drops
    │
    ├─ Retry 1 → wait 1s → reconnect
    ├─ Retry 2 → wait 2s → reconnect
    ├─ Retry 3 → wait 4s → reconnect
    ├─ Retry 4 → wait 8s → reconnect
    └─ Max retries → mark channel is_active=false
                     emit channel.unavailable event
                     return error to client
```

### Shared relay (fan-out)

Si múltiples usuarios ven el mismo canal simultáneamente, no abrimos múltiples conexiones al proveedor:

```
Provider stream ──→ Relay goroutine ──┬──→ User A (writer)
                                      ├──→ User B (writer)
                                      └──→ User C (writer)
```

- Una sola conexión HTTP al proveedor por canal activo
- Fan-out a todos los viewers mediante `io.MultiWriter` pattern
- Cuando el último viewer se desconecta → cerrar conexión al proveedor (con grace period de 30s)

---

## 5. Channel Health Monitoring

Background job que verifica periódicamente la salud de los canales.

```go
type HealthChecker interface {
    // Check a single channel
    CheckChannel(ctx context.Context, channelID uuid.UUID) (*HealthResult, error)

    // Check all channels in a library (batch)
    CheckAll(ctx context.Context, libraryID uuid.UUID) ([]HealthResult, error)
}

type HealthResult struct {
    ChannelID   uuid.UUID
    IsReachable bool
    ResponseTime time.Duration
    HTTPStatus   int
    Error        string
    CheckedAt    time.Time
}
```

### Estrategia

| Aspecto | Valor |
|---|---|
| Frecuencia | Cada 30 minutos (configurable) |
| Método | HTTP HEAD al stream URL, timeout 10s |
| Canales marcados `is_active=false` | Recheck cada 6h (backoff) |
| Resultado | Actualizar `channels.is_active`, log health status |
| Concurrencia | Max 20 checks en paralelo (semaphore) |
| No molestar | No check si hay viewers activos en el canal |

---

## 6. User Features

### Favoritos

```go
// Los favoritos de canales usan la misma tabla user_data que movies/series
// user_data.item_id → channel UUID, is_favorite = true
```

UI: el usuario marca canales como favoritos con un corazón. Aparecen en el tab "Favoritos" del Live TV.

### Último canal visto

Al abrir Live TV, auto-tune al último canal que el usuario estaba viendo:

```go
// Guardar en user preferences
type UserPreferences struct {
    LastChannelID  *uuid.UUID  // último canal visto
    // ...
}
```

### Número de canal

El usuario puede escribir un número (ej: "001") con el teclado/mando para saltar directamente a un canal, como en una TV real:

```
User types "1" → wait 1.5s for more digits → tune to channel #1
User types "1" "2" "3" → tune to channel #123
```

---

## 7. API Endpoints

```
# Channels
GET    /api/v1/channels                      → List channels (with filters: group, active, search)
GET    /api/v1/channels/{id}                 → Channel detail
GET    /api/v1/channels/{id}/stream          → Proxy stream
POST   /api/v1/channels/{id}/favorite        → Toggle favorite

# EPG
GET    /api/v1/channels/{id}/epg             → Schedule for one channel (?from=&to=)
GET    /api/v1/channels/epg                  → Bulk schedule for all channels (?from=&to=&group=)
GET    /api/v1/channels/now                  → What's on now (all channels, lightweight)
GET    /api/v1/epg/search                    → Search programs by title (?q=&from=&to=)

# Admin
POST   /api/v1/admin/iptv/refresh-playlist   → Force M3U refresh
POST   /api/v1/admin/iptv/refresh-epg        → Force EPG refresh
GET    /api/v1/admin/iptv/health             → Channel health report
PUT    /api/v1/admin/iptv/channel-mapping    → Manual EPG ↔ channel mapping
```

### Response examples

**GET /api/v1/channels/now**
```json
{
  "channels": [
    {
      "id": "uuid-la1",
      "name": "La 1",
      "number": 1,
      "group": "Nacionales",
      "logo_url": "https://...",
      "is_active": true,
      "now": {
        "title": "Telediario",
        "start": "2026-03-13T20:00:00+01:00",
        "end": "2026-03-13T21:00:00+01:00",
        "progress": 0.67,
        "category": "Noticias"
      },
      "next": {
        "title": "El Tiempo",
        "start": "2026-03-13T21:00:00+01:00"
      }
    }
  ]
}
```

**GET /api/v1/channels/epg?from=2026-03-13T20:00:00Z&to=2026-03-14T02:00:00Z**
```json
{
  "schedule": {
    "uuid-la1": [
      {
        "title": "Telediario",
        "description": "Noticias del día...",
        "category": "Noticias",
        "start": "2026-03-13T20:00:00+01:00",
        "end": "2026-03-13T21:00:00+01:00",
        "icon_url": "https://..."
      },
      {
        "title": "El Tiempo",
        "start": "2026-03-13T21:00:00+01:00",
        "end": "2026-03-13T21:15:00+01:00"
      }
    ]
  }
}
```

---

## 8. Configuration

```yaml
# hubplay.yaml — IPTV section
iptv:
  proxy_streams: true               # Proxy through HubPlay (recommended)
  epg_refresh_interval: "6h"        # How often to re-download EPG
  m3u_refresh_interval: "24h"       # How often to re-download playlist
  epg_window_past: "24h"            # Keep EPG data from last 24h
  epg_window_future: "72h"          # Keep EPG data for next 72h
  health_check_interval: "30m"      # Channel health check frequency
  health_check_concurrency: 20      # Parallel health checks
  stream_reconnect_max_retries: 4   # Max retries on source drop
  stream_reconnect_backoff: "1s"    # Initial backoff (doubles each retry)
  fan_out_grace_period: "30s"       # Keep source connection alive after last viewer
  channel_switch_timeout: "5s"      # Max time to establish stream on channel switch
```

### Environment variable overrides

```bash
HUBPLAY_IPTV_PROXY_STREAMS=true
HUBPLAY_IPTV_EPG_REFRESH_INTERVAL=6h
HUBPLAY_IPTV_M3U_REFRESH_INTERVAL=24h
```

---

## 9. Frontend Integration

### Live TV View (`/live-tv`)

```
Player (top half)
    ↕ Channel info bar (now playing, progress, next)
    ↕ Category tabs [All] [Favoritos] [Nacionales] [Deportes] ...
    ↕ Channel grid (scrollable, virtual rendering)
```

### EPG Grid (`/live-tv/guide`)

Powered by **Planby** library:
- Virtual scrolling horizontal (time) + vertical (channels)
- Red "NOW" line (vertical marker at current time)
- Click program → info popup with description
- Click channel → tune to it
- Keyboard navigation (arrow keys, Enter)

### Data flow

```
1. Page loads → TanStack Query fetches /channels/now
2. Channels render with current program info
3. EPG grid: batch fetch /channels/epg?from=now-1h&to=now+6h
4. WebSocket: listen for epg.updated events → invalidate + refetch
5. Channel click → player switches to /channels/{id}/stream
6. Every 60s → re-fetch /channels/now for progress bar updates
```

### Mini-player

Al navegar fuera de Live TV, el stream continúa en un mini-player flotante (bottom-right). Implementado con un `<video>` element que se "desacopla" del player principal y se reposiciona con CSS.

---

## 10. Future: Timeshift & Catch-up TV

No en v1, pero el diseño lo contempla.

### Timeshift (pause/rewind live)

```
Concepto:
  - Buffer circular de los últimos N minutos en disco
  - El usuario puede pausar y rebobinar live TV
  - Al "poner al día" → vuelve a live

Implementación:
  - HLS: el proxy guarda los últimos 30min de segmentos .ts en disco
  - Servir segmentos guardados cuando el usuario hace seek al pasado
  - Cuando el buffer llega al límite → eliminar segmentos más antiguos
```

### Catch-up TV (ver programas pasados)

```
Concepto:
  - Algunos proveedores ofrecen URLs de catch-up (programa ya emitido)
  - El usuario ve la EPG pasada y puede hacer click en un programa para verlo

Requisitos:
  - El proveedor IPTV debe soportar catch-up (no todos lo hacen)
  - La URL de catch-up suele incluir timestamp: stream-url?start=YYYYMMDDHHMMSS
  - Detectar soporte via atributos M3U: catchup="default" catchup-days="7"
```

---

## 11. Events

```go
const (
    EventChannelAdded       EventType = "channel.added"
    EventChannelRemoved     EventType = "channel.removed"
    EventChannelUnavailable EventType = "channel.unavailable"
    EventChannelRestored    EventType = "channel.restored"
    EventEPGUpdated         EventType = "epg.updated"
    EventPlaylistRefreshed  EventType = "playlist.refreshed"
    EventStreamStarted      EventType = "stream.live.started"
    EventStreamEnded        EventType = "stream.live.ended"
)
```

---

## 12. Directory Structure

```
internal/iptv/
├── manager.go          # ChannelManager — orchestrates everything
├── m3u.go              # M3U parser (streaming, handles large files)
├── epg.go              # XMLTV EPG parser + EPGManager
├── proxy.go            # StreamProxy with fan-out + reconnection
├── health.go           # HealthChecker background job
├── channel.go          # Channel domain type
└── mapping.go          # EPG ↔ channel matching logic (auto + manual)
```
