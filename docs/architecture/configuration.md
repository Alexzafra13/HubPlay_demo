# Configuration Reference

HubPlay se configura con un único archivo YAML + variables de entorno opcionales. El primer arranque genera un config por defecto con valores seguros. **No necesitas crear el archivo manualmente** — se auto-genera.

---

## 1. Config File Location

| Despliegue | Ruta | Cómo se crea |
|---|---|---|
| Docker | `/config/hubplay.yaml` | Auto-generado en primer arranque |
| Native binary | `--config /etc/hubplay/hubplay.yaml` | Copiar `hubplay.example.yaml` |
| Development | `./hubplay.yaml` | Copiar `hubplay.example.yaml` |

---

## 2. Full Schema

```yaml
# ═══════════════════════════════════════════
# SERVER
# ═══════════════════════════════════════════
server:
  bind: "0.0.0.0"                       # Listen address
                                         #   "0.0.0.0" → all interfaces (Docker default)
                                         #   "127.0.0.1" → localhost only (behind reverse proxy)
  port: 8096                             # HTTP port
  base_url: ""                           # Subpath prefix, e.g. "/hubplay" if served under subpath
                                         #   Empty = root (http://host:8096/)
  warn_no_tls: true                      # Log warning if accessed over HTTP from public IP
  trusted_proxies:                       # IPs allowed to set X-Forwarded-For / X-Real-IP
    - "127.0.0.1"                        #   Always trust localhost
    - "172.16.0.0/12"                    #   Docker default network
    - "10.0.0.0/8"                       #   Private networks

# ═══════════════════════════════════════════
# DATABASE
# ═══════════════════════════════════════════
database:
  driver: "sqlite"                       # "sqlite" or "postgres"
  # SQLite (default — zero config)
  path: "/config/hubplay.db"             # Database file location
  # PostgreSQL (uncomment for large deployments)
  # driver: "postgres"
  # dsn: "host=localhost port=5432 dbname=hubplay user=hubplay password=secret sslmode=disable"

# ═══════════════════════════════════════════
# AUTHENTICATION
# ═══════════════════════════════════════════
auth:
  jwt_secret: ""                         # Auto-generated on first run (32 bytes, crypto/rand)
                                         #   ⚠ NEVER change after setup — invalidates all sessions
  bcrypt_cost: 12                        # Password hash cost (10–14). Higher = slower but safer
  access_token_ttl: "15m"                # JWT access token lifetime
  refresh_token_ttl: "720h"              # Refresh token lifetime (30 days)
  max_sessions_per_user: 10              # Max concurrent devices per user (0 = unlimited)
  registration_enabled: false            # If true, anyone can register. Default: admin creates users

# ═══════════════════════════════════════════
# TRANSCODING
# ═══════════════════════════════════════════
transcoding:
  threads: 0                             # FFmpeg threads (0 = auto, use all cores)
  hw_accel: ""                           # Hardware acceleration
                                         #   "" → auto-detect at startup
                                         #   "vaapi" → Linux Intel/AMD
                                         #   "qsv" → Intel Quick Sync
                                         #   "nvenc" → NVIDIA
                                         #   "videotoolbox" → macOS
  vaapi_device: "/dev/dri/renderD128"    # VAAPI device path (only used if hw_accel=vaapi)
  cache_dir: "/cache/transcode"          # HLS segment cache
  cache_max_size_gb: 50                  # Auto-cleanup: delete oldest sessions when exceeded
  temp_dir: "/tmp"                       # FFmpeg temp files
  default_preset: "fast"                 # FFmpeg encoding preset
                                         #   "ultrafast" → lowest quality, fastest (weak hardware)
                                         #   "fast" → good balance (recommended)
                                         #   "medium" → better quality, slower (powerful hardware)
  max_sessions: 2                        # Max concurrent transcode sessions
                                         #   Depends on hardware:
                                         #   RPi/NAS: 1, 4-core: 2–3, GPU: 5–10+
  segment_duration: 6                    # HLS segment length in seconds (6 = standard)
  default_audio_bitrate: "192k"          # Audio bitrate for transcoded streams

# ═══════════════════════════════════════════
# TRICKPLAY (Timeline Preview Thumbnails)
# ═══════════════════════════════════════════
  trickplay:
    enabled: true                        # Generate timeline preview thumbnails
    interval: "10s"                      # One frame every N seconds
    width: 160                           # Thumbnail width in pixels
    columns: 10                          # Sprites per row in sprite sheet
    max_workers: 2                       # Concurrent trickplay generation jobs

# ═══════════════════════════════════════════
# SCANNER (Library scanning)
# ═══════════════════════════════════════════
scanner:
  schedule: "0 2 * * *"                  # Cron expression for scheduled scans
                                         #   "0 2 * * *" → daily at 2:00 AM
                                         #   "" → disabled (manual/watch only)
  watch_filesystem: true                 # Real-time detection via fsnotify (inotify on Linux)
  scan_on_startup: true                  # Full scan when HubPlay starts
  parallel_probes: 4                     # Concurrent FFprobe calls during scan
                                         #   Higher = faster scan, more I/O
  ignore_patterns:                       # Files to skip during scanning
    - "*.sample.*"
    - "*.txt"
    - "*.nfo"
    - ".DS_Store"
    - "Thumbs.db"

# ═══════════════════════════════════════════
# METADATA
# ═══════════════════════════════════════════
metadata:
  tmdb_api_key: ""                       # Can also use TMDB_API_KEY env var
                                         #   Get free key at https://www.themoviedb.org/settings/api
  fanart_api_key: ""                     # Can also use FANART_API_KEY env var
                                         #   Get free key at https://fanart.tv/get-an-api-key/
  language: "es"                         # Preferred metadata language (ISO 639-1)
  country: "ES"                          # Region for ratings/release dates (ISO 3166-1)

# ═══════════════════════════════════════════
# IPTV / LIVE TV
# ═══════════════════════════════════════════
iptv:
  proxy_streams: true                    # Proxy streams through HubPlay (recommended)
                                         #   true → unified auth, CORS fix, reconnection
                                         #   false → client connects directly to provider URL
  epg_refresh_interval: "6h"             # How often to re-download EPG (XMLTV)
  m3u_refresh_interval: "24h"            # How often to re-download M3U playlist
  epg_window_past: "24h"                 # Keep EPG data from the past N hours
  epg_window_future: "72h"              # Keep EPG data for the next N hours
  health_check_interval: "30m"           # Channel health check frequency
  health_check_concurrency: 20           # Parallel health checks
  stream_reconnect_max_retries: 4        # Max retries on source stream drop
  fan_out_grace_period: "30s"            # Keep source alive after last viewer disconnects

# ═══════════════════════════════════════════
# FEDERATION (Server-to-server)
# ═══════════════════════════════════════════
federation:
  enabled: false                         # Enable federation features
  display_name: "My HubPlay Server"      # Name visible to peers
  max_bandwidth_mbps: 50                 # Max total outbound bandwidth for federation
  max_concurrent_streams: 3              # Max simultaneous streams served to peers
  catalog_sync_interval: "1h"            # How often to sync remote catalogs

# ═══════════════════════════════════════════
# PLUGINS
# ═══════════════════════════════════════════
plugins:
  directory: "/config/plugins"           # Plugin install directory
  auto_update: false                     # Check for updates but don't auto-install

# ═══════════════════════════════════════════
# LOGGING
# ═══════════════════════════════════════════
logging:
  level: "info"                          # "debug" | "info" | "warn" | "error"
  format: "json"                         # "json" → structured (Docker, log aggregators)
                                         # "text" → human-readable (development)
  log_ips: true                          # Log client IPs (for security audit)
  file: ""                               # Log file path. Empty = stdout (Docker-friendly)
                                         #   "/var/log/hubplay/hubplay.log" for systemd

# ═══════════════════════════════════════════
# RATE LIMITING
# ═══════════════════════════════════════════
rate_limit:
  enabled: true                          # Enable rate limiting
  login_attempts: 5                      # Max login attempts per 15 min (per IP+username)
  global_rpm: 100                        # Requests per minute per authenticated user

# ═══════════════════════════════════════════
# CORS
# ═══════════════════════════════════════════
cors:
  allowed_origins: []                    # Empty = allow all origins (safe for LAN)
                                         #   ["https://hubplay.example.com"] for public servers
```

---

## 3. Environment Variable Overrides

Every config value can be overridden via environment variable with the `HUBPLAY_` prefix. Nested keys use `_` as separator.

```bash
# Server
HUBPLAY_SERVER_PORT=9090
HUBPLAY_SERVER_BIND=127.0.0.1
HUBPLAY_SERVER_BASE_URL=/hubplay

# Database
HUBPLAY_DATABASE_DRIVER=postgres
HUBPLAY_DATABASE_DSN="host=db dbname=hubplay user=hubplay password=secret sslmode=disable"
HUBPLAY_DATABASE_PATH=/data/hubplay.db

# Auth
HUBPLAY_AUTH_JWT_SECRET=my-secret-key
HUBPLAY_AUTH_BCRYPT_COST=12
HUBPLAY_AUTH_ACCESS_TOKEN_TTL=15m
HUBPLAY_AUTH_REGISTRATION_ENABLED=false

# Transcoding
HUBPLAY_TRANSCODING_HW_ACCEL=vaapi
HUBPLAY_TRANSCODING_VAAPI_DEVICE=/dev/dri/renderD128
HUBPLAY_TRANSCODING_MAX_SESSIONS=3
HUBPLAY_TRANSCODING_DEFAULT_PRESET=ultrafast
HUBPLAY_TRANSCODING_CACHE_MAX_SIZE_GB=100

# Scanner
HUBPLAY_SCANNER_PARALLEL_PROBES=8
HUBPLAY_SCANNER_WATCH_FILESYSTEM=true

# Metadata (special case: no HUBPLAY_ prefix)
TMDB_API_KEY=abc123
FANART_API_KEY=def456

# IPTV
HUBPLAY_IPTV_PROXY_STREAMS=true
HUBPLAY_IPTV_EPG_REFRESH_INTERVAL=12h

# Federation
HUBPLAY_FEDERATION_ENABLED=true
HUBPLAY_FEDERATION_DISPLAY_NAME="Alex's Server"

# Logging
HUBPLAY_LOGGING_LEVEL=debug
HUBPLAY_LOGGING_FORMAT=text

# Timezone (Docker)
TZ=Europe/Madrid
```

### Precedence

```
Environment variable  >  Config file  >  Default value
```

---

## 4. Example Configurations

### Home NAS (Synology, Unraid, TrueNAS)

Minimal config — almost everything at defaults.

```yaml
# hubplay.yaml — NAS doméstico
database:
  path: "/config/hubplay.db"

transcoding:
  hw_accel: ""              # auto-detect (Intel iGPU likely)
  default_preset: "fast"
  max_sessions: 1           # NAS CPUs are weak

scanner:
  schedule: "0 3 * * *"    # Scan at 3 AM
  parallel_probes: 2        # Don't hammer the NAS disks

metadata:
  tmdb_api_key: "${TMDB_API_KEY}"
  language: "es"
  country: "ES"
```

### Dedicated Server (Public, with HTTPS)

Behind Caddy reverse proxy, PostgreSQL, NVIDIA GPU.

```yaml
# hubplay.yaml — servidor dedicado
server:
  bind: "127.0.0.1"        # Only accept from reverse proxy
  port: 8096
  trusted_proxies:
    - "127.0.0.1"

database:
  driver: "postgres"
  dsn: "host=localhost dbname=hubplay user=hubplay password=${DB_PASSWORD} sslmode=disable"

auth:
  bcrypt_cost: 12
  max_sessions_per_user: 5

transcoding:
  hw_accel: "nvenc"
  default_preset: "medium"  # GPU can handle better quality
  max_sessions: 8           # NVIDIA can handle many streams
  cache_max_size_gb: 200

scanner:
  parallel_probes: 8

metadata:
  tmdb_api_key: "${TMDB_API_KEY}"
  fanart_api_key: "${FANART_API_KEY}"
  language: "es"
  country: "ES"

federation:
  enabled: true
  display_name: "Alex's Media Server"
  max_bandwidth_mbps: 100
  max_concurrent_streams: 5

logging:
  level: "info"
  format: "json"

cors:
  allowed_origins:
    - "https://hubplay.example.com"

rate_limit:
  enabled: true
  login_attempts: 3
  global_rpm: 60
```

### Docker Compose con IPTV + PostgreSQL

```yaml
# docker-compose.yml
services:
  hubplay:
    image: hubplay/hubplay:latest
    container_name: hubplay
    ports:
      - "8096:8096"
    volumes:
      - hubplay-config:/config
      - hubplay-cache:/cache
      - /media/movies:/media/movies:ro
      - /media/tv:/media/tv:ro
    environment:
      - HUBPLAY_DATABASE_DRIVER=postgres
      - HUBPLAY_DATABASE_DSN=host=db dbname=hubplay user=hubplay password=secret sslmode=disable
      - TMDB_API_KEY=${TMDB_API_KEY}
      - TZ=Europe/Madrid
    depends_on:
      db:
        condition: service_healthy
    devices:
      - /dev/dri:/dev/dri    # Intel/AMD GPU
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: hubplay
      POSTGRES_USER: hubplay
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U hubplay"]
      interval: 5s
      timeout: 3s
      retries: 5

volumes:
  hubplay-config:
  hubplay-cache:
  pgdata:
```

### Development (Local)

```yaml
# hubplay.yaml — desarrollo local
server:
  bind: "127.0.0.1"
  port: 8096

database:
  driver: "sqlite"
  path: "./dev.db"

transcoding:
  hw_accel: ""
  default_preset: "ultrafast"
  max_sessions: 1

scanner:
  watch_filesystem: true
  scan_on_startup: false     # Don't scan on every restart during dev
  parallel_probes: 2

metadata:
  tmdb_api_key: "${TMDB_API_KEY}"
  language: "es"

logging:
  level: "debug"
  format: "text"             # Human-readable for terminal

rate_limit:
  enabled: false             # No rate limiting in dev
```

---

## 5. Validation

Config is validated at startup before creating any services. Invalid config = fail fast with clear error message.

```
$ hubplay --config /etc/hubplay/hubplay.yaml
Error: invalid config:
  - server.port must be 1-65535, got 0
  - auth.bcrypt_cost must be 10-14, got 5
  - libraries[0].paths: "/nonexistent" directory does not exist
```

### Validation rules

| Field | Rule |
|---|---|
| `server.port` | 1–65535 |
| `server.bind` | Valid IP address |
| `auth.bcrypt_cost` | 10–14 |
| `auth.access_token_ttl` | ≥ 1m |
| `auth.refresh_token_ttl` | ≥ 1h |
| `database.driver` | "sqlite" or "postgres" |
| `database.path` (sqlite) | Directory must exist and be writable |
| `database.dsn` (postgres) | Must not be empty |
| `transcoding.hw_accel` | "", "vaapi", "qsv", "nvenc", "videotoolbox" |
| `transcoding.default_preset` | "ultrafast", "superfast", "veryfast", "faster", "fast", "medium" |
| `transcoding.max_sessions` | ≥ 1 |
| `scanner.schedule` | Valid cron expression (or empty to disable) |
| `logging.level` | "debug", "info", "warn", "error" |
| `logging.format` | "json", "text" |

---

## 6. First-Run Auto-Generation

On first startup with no config file present:

```
1. Generate hubplay.yaml with secure defaults
2. Generate JWT secret (32 bytes, crypto/rand, hex-encoded)
3. Create SQLite database + run all migrations
4. Detect FFmpeg + hardware acceleration
5. Start server → show setup wizard URL
```

The generated config is immediately usable — zero manual editing required for basic setups.

---

## 7. Config Hot-Reload

Most config values require a restart to take effect. **Exceptions** that support hot-reload via `SIGHUP`:

| Config | Hot-reload? |
|---|---|
| `logging.level` | Yes |
| `rate_limit.*` | Yes |
| `cors.allowed_origins` | Yes |
| `transcoding.max_sessions` | Yes |
| `scanner.schedule` | Yes |
| Everything else | No (restart required) |

```bash
# Trigger hot-reload
kill -HUP $(pidof hubplay)

# Or via API (admin only)
POST /api/v1/system/reload-config
```
