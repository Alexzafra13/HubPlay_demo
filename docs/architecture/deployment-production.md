# Deployment & Production — Design Document

Guide for deploying HubPlay in real environments: Docker, native binary, systemd, NAS, reverse proxy, backups, and monitoring. Self-hosted — the admin controls everything.

---

## 1. Deployment Methods

### Docker — Plug and Play (Recommended)

**El objetivo**: `docker compose up -d` y listo. Sin instalar nada más. La imagen Docker incluye todo lo necesario:

- FFmpeg completo (todos los codecs: H.264, HEVC, VP9, AV1, AAC, AC3, Opus, FLAC…)
- Drivers VA-API (Intel/AMD) para transcoding por hardware
- libass + fuentes para burn-in de subtítulos ASS/SSA/PGS
- Certificados TLS para HTTPS saliente (TMDb, federación)
- Timezone data para programación correcta del scanner
- Frontend React embebido en el binario Go

```yaml
# docker-compose.yml — production (plug and play)
#
# Solo necesitas:
# 1. Copiar este archivo
# 2. Ajustar las rutas de /media a tus carpetas
# 3. docker compose up -d
# 4. Abrir http://localhost:8096

services:
  hubplay:
    image: hubplay/hubplay:latest
    container_name: hubplay
    ports:
      - "8096:8096"
    volumes:
      - hubplay-config:/config               # hubplay.yaml + SQLite DB (auto-creados)
      - hubplay-cache:/cache                  # Transcoding cache, thumbnails
      - /media/movies:/media/movies:ro        # ← CAMBIA a tu ruta de películas
      - /media/tv:/media/tv:ro               # ← CAMBIA a tu ruta de series
      - /media/music:/media/music:ro         # ← CAMBIA a tu ruta de música (opcional)
    environment:
      - TMDB_API_KEY=${TMDB_API_KEY:-}       # Opcional: metadata automática
      - FANART_API_KEY=${FANART_API_KEY:-}   # Opcional: artwork extra
      - TZ=${TZ:-Europe/Madrid}              # Tu zona horaria
    restart: unless-stopped
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"
    # Security hardening
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp:size=2G                          # FFmpeg temp files during transcode
    # Health check (ya incluido en la imagen, pero aquí por claridad)
    healthcheck:
      test: ["CMD", "hubplay", "health"]
      interval: 30s
      timeout: 3s
      start_period: 10s
      retries: 3

volumes:
  hubplay-config:    # Persiste config + DB entre actualizaciones
  hubplay-cache:     # Cache de transcode (se puede borrar sin perder datos)
```

### Primer arranque — qué pasa automáticamente

```
docker compose up -d
  │
  ├─ 1. Imagen descargada (~180MB, todo incluido)
  ├─ 2. Volúmenes creados (config + cache)
  ├─ 3. hubplay.yaml generado con defaults seguros
  │     └─ JWT secret auto-generado (crypto/rand)
  ├─ 4. SQLite DB creada + migrations ejecutadas
  ├─ 5. FFmpeg detectado + HW acceleration probada
  │     └─ Si hay GPU compatible → activada automáticamente
  ├─ 6. Scanner listo (esperando a que configures libraries)
  └─ 7. UI disponible en http://localhost:8096
         └─ Setup wizard: crear usuario admin
```

### ¿Qué NO necesitas hacer?

| Paso | ¿Necesario? | Por qué |
|------|-------------|---------|
| Instalar FFmpeg | No | Incluido en la imagen |
| Configurar codecs | No | FFmpeg viene con todos compilados |
| Instalar drivers GPU | No* | VA-API drivers incluidos para Intel/AMD |
| Crear hubplay.yaml | No | Se auto-genera en primer arranque |
| Crear la base de datos | No | SQLite se crea automáticamente |
| Instalar fuentes para subtítulos | No | Noto + Liberation incluidas |
| Configurar certificados TLS | No | CA certs incluidos para HTTPS saliente |

*\*NVIDIA: usar variante `hubplay/hubplay:latest-nvidia` que incluye CUDA runtime*

**Why named volumes over bind mounts?**
- Portable (work on any Docker host without path issues)
- Better performance on Docker Desktop (macOS/Windows)
- Easier backup: `docker volume inspect` → one path

**Media libraries stay as bind mounts** — admin needs direct control over the path, and `:ro` prevents accidental writes.

### Docker with Hardware Transcoding

```yaml
# GPU passthrough for transcoding
services:
  hubplay:
    image: hubplay/hubplay:latest
    # ... same as above, plus:
    devices:
      - /dev/dri:/dev/dri          # Intel QSV / VAAPI
    # OR for NVIDIA:
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
    environment:
      - HUBPLAY_TRANSCODE_HW=vaapi     # vaapi | qsv | nvenc | videotoolbox
      - HUBPLAY_VAAPI_DEVICE=/dev/dri/renderD128
```

### Native Binary

```bash
# Download latest release
curl -L https://github.com/hubplay/hubplay/releases/latest/download/hubplay-linux-amd64 -o /usr/local/bin/hubplay
chmod +x /usr/local/bin/hubplay

# Verify
hubplay --version

# Prerequisites
# FFmpeg must be installed
apt install ffmpeg    # Debian/Ubuntu
dnf install ffmpeg    # Fedora
pacman -S ffmpeg      # Arch
```

### Systemd Service (native binary)

```ini
# /etc/systemd/system/hubplay.service
[Unit]
Description=HubPlay Media Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=hubplay
Group=hubplay
ExecStart=/usr/local/bin/hubplay --config /etc/hubplay/hubplay.yaml

# Directories
WorkingDirectory=/var/lib/hubplay
ConfigurationDirectory=hubplay
StateDirectory=hubplay
CacheDirectory=hubplay

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/hubplay /var/cache/hubplay
# Allow read access to media libraries (add your paths)
ReadOnlyPaths=/media/movies /media/tv /media/music

# Resource limits
LimitNOFILE=65536
MemoryMax=4G

# Restart policy
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# Setup
useradd -r -s /sbin/nologin hubplay
mkdir -p /etc/hubplay /var/lib/hubplay /var/cache/hubplay
chown hubplay:hubplay /var/lib/hubplay /var/cache/hubplay

# Enable and start
systemctl daemon-reload
systemctl enable --now hubplay

# Logs
journalctl -u hubplay -f
```

---

## 2. Configuration

### Full Config Reference

```yaml
# /etc/hubplay/hubplay.yaml (or /config/hubplay.yaml in Docker)

server:
  bind: "0.0.0.0"                     # 127.0.0.1 if behind reverse proxy
  port: 8096
  base_url: ""                         # "/hubplay" if served under subpath
  warn_no_tls: true
  trusted_proxies:
    - "127.0.0.1"
    - "172.16.0.0/12"

database:
  driver: "sqlite"                     # sqlite | postgres
  # SQLite (default)
  path: "/config/hubplay.db"
  # PostgreSQL (optional, for large deployments)
  # driver: "postgres"
  # dsn: "host=localhost dbname=hubplay user=hubplay password=secret sslmode=disable"

auth:
  jwt_secret: ""                       # Auto-generated on first run
  bcrypt_cost: 12
  access_token_ttl: "15m"
  refresh_token_ttl: "720h"            # 30 days
  max_sessions_per_user: 10
  registration_enabled: false          # Admin creates users manually

transcoding:
  threads: 0                           # 0 = auto (use all cores)
  hw_accel: ""                         # vaapi | qsv | nvenc | videotoolbox | ""
  vaapi_device: "/dev/dri/renderD128"
  cache_dir: "/cache/transcode"
  cache_max_size_gb: 50                # Auto-cleanup oldest segments
  temp_dir: "/tmp"
  default_preset: "fast"               # ultrafast | fast | medium (quality vs speed)

scanner:
  schedule: "0 2 * * *"               # Cron: scan at 2 AM daily
  watch_filesystem: true               # inotify for real-time detection
  scan_on_startup: true
  parallel_probes: 4                   # Concurrent FFprobe calls

metadata:
  tmdb_api_key: ""                     # Can also use TMDB_API_KEY env var
  fanart_api_key: ""
  language: "es"                       # Metadata language (ISO 639-1)
  country: "ES"                        # For region-specific ratings/release dates

iptv:
  proxy_streams: true                  # Proxy through HubPlay for unified auth
  epg_refresh_interval: "6h"
  m3u_refresh_interval: "24h"

federation:
  enabled: false
  display_name: "My HubPlay Server"
  max_bandwidth_mbps: 50
  max_concurrent_streams: 3

plugins:
  directory: "/config/plugins"
  auto_update: false                   # Check for plugin updates but don't install

logging:
  level: "info"                        # debug | info | warn | error
  format: "json"                       # json | text (text for development)
  log_ips: true
  file: ""                             # Empty = stdout (Docker-friendly)
  # file: "/var/log/hubplay/hubplay.log"  # For systemd setups

rate_limit:
  enabled: true
  login_attempts: 5
  global_rpm: 100

cors:
  allowed_origins: []                  # Empty = allow all (LAN). Set for public
```

### Environment Variable Overrides

Every config value can be overridden via environment variable with `HUBPLAY_` prefix:

```bash
HUBPLAY_SERVER_PORT=9090
HUBPLAY_DATABASE_DRIVER=postgres
HUBPLAY_DATABASE_DSN="host=db dbname=hubplay ..."
HUBPLAY_AUTH_JWT_SECRET=my-secret
HUBPLAY_TRANSCODING_HW_ACCEL=vaapi
HUBPLAY_LOGGING_LEVEL=debug
TMDB_API_KEY=abc123             # Special case: no prefix
FANART_API_KEY=def456           # Special case: no prefix
```

Priority: env var > config file > default value.

---

## 3. Reverse Proxy

### Caddy (Recommended — auto HTTPS)

```caddyfile
hubplay.example.com {
    reverse_proxy localhost:8096

    # Optional: compress responses
    encode gzip zstd
}
```

That's it. Caddy automatically:
- Obtains Let's Encrypt certificate
- Redirects HTTP → HTTPS
- Renews certificates
- Enables HTTP/2

### nginx

```nginx
# /etc/nginx/sites-available/hubplay
upstream hubplay {
    server 127.0.0.1:8096;
    keepalive 32;
}

server {
    listen 80;
    server_name hubplay.example.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name hubplay.example.com;

    ssl_certificate     /etc/letsencrypt/live/hubplay.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hubplay.example.com/privkey.pem;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header X-XSS-Protection "0" always;  # Disable: CSP is better

    # Streaming — no buffering, large timeout
    location /api/v1/stream/ {
        proxy_pass http://hubplay;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;  # 1 hour for long streams
        proxy_send_timeout 3600s;
    }

    # WebSocket
    location /api/v1/ws {
        proxy_pass http://hubplay;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400s;
    }

    # API and frontend
    location / {
        proxy_pass http://hubplay;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Large media uploads (if ever needed)
        client_max_body_size 0;
    }
}
```

### Traefik (Docker label-based)

```yaml
# docker-compose.yml with Traefik
services:
  hubplay:
    image: hubplay/hubplay:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.hubplay.rule=Host(`hubplay.example.com`)"
      - "traefik.http.routers.hubplay.tls.certresolver=letsencrypt"
      - "traefik.http.services.hubplay.loadbalancer.server.port=8096"
    # ... volumes, environment, etc.
```

---

## 4. Database in Production

### SQLite (default, ≤10 concurrent users)

Good for: home server, personal use, small families.

```yaml
database:
  driver: "sqlite"
  path: "/config/hubplay.db"
```

SQLite configuration (set automatically):
- **WAL mode** — concurrent reads during writes
- **busy_timeout=5000** — wait 5s before failing on lock
- **journal_size_limit=67108864** — WAL file max 64MB
- **synchronous=NORMAL** — good durability/performance balance
- **cache_size=-64000** — 64MB page cache

### PostgreSQL (optional, >10 concurrent users)

For: shared servers, multiple concurrent streamers, many federation peers.

```yaml
database:
  driver: "postgres"
  dsn: "host=localhost port=5432 dbname=hubplay user=hubplay password=secret sslmode=disable"
```

Docker Compose with PostgreSQL:

```yaml
services:
  hubplay:
    image: hubplay/hubplay:latest
    environment:
      - HUBPLAY_DATABASE_DRIVER=postgres
      - HUBPLAY_DATABASE_DSN=host=db dbname=hubplay user=hubplay password=secret sslmode=disable
    depends_on:
      db:
        condition: service_healthy
    # ... rest of config

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
  pgdata:
```

### Migrations

Migrations run automatically on startup. No manual intervention needed.

```bash
# Manual migration (if needed)
hubplay migrate up --config /etc/hubplay/hubplay.yaml

# Check migration status
hubplay migrate status

# Rollback last migration (emergency)
hubplay migrate down --steps 1
```

---

## 5. Backup & Restore

### SQLite Backup

```bash
#!/bin/bash
# /usr/local/bin/hubplay-backup.sh
BACKUP_DIR="/backups/hubplay"
DATE=$(date +%Y-%m-%d_%H%M)
CONFIG_DIR="/config"  # or /var/lib/hubplay for systemd

mkdir -p "$BACKUP_DIR"

# Hot backup (safe while HubPlay is running — SQLite .backup API)
sqlite3 "$CONFIG_DIR/hubplay.db" ".backup '$BACKUP_DIR/hubplay_$DATE.db'"

# Config
cp "$CONFIG_DIR/hubplay.yaml" "$BACKUP_DIR/hubplay_$DATE.yaml"

# Compress
tar czf "$BACKUP_DIR/hubplay_$DATE.tar.gz" \
    -C "$BACKUP_DIR" \
    "hubplay_$DATE.db" \
    "hubplay_$DATE.yaml"

# Cleanup individual files
rm "$BACKUP_DIR/hubplay_$DATE.db" "$BACKUP_DIR/hubplay_$DATE.yaml"

# Retention: keep last 30 days
find "$BACKUP_DIR" -name "hubplay_*.tar.gz" -mtime +30 -delete

echo "Backup complete: hubplay_$DATE.tar.gz"
```

```bash
# Cron: daily backup at 3 AM
echo "0 3 * * * root /usr/local/bin/hubplay-backup.sh" > /etc/cron.d/hubplay-backup
```

### PostgreSQL Backup

```bash
# Hot backup
pg_dump -U hubplay -Fc hubplay > "/backups/hubplay/hubplay_$(date +%Y-%m-%d).dump"

# Restore
pg_restore -U hubplay -d hubplay --clean "/backups/hubplay/hubplay_2026-03-13.dump"
```

### Docker Volume Backup

```bash
# Stop for consistent backup (or use sqlite3 .backup first)
docker compose exec hubplay sqlite3 /config/hubplay.db ".backup /config/hubplay_backup.db"

# Copy from volume
docker cp hubplay:/config/hubplay_backup.db ./hubplay_backup.db
docker cp hubplay:/config/hubplay.yaml ./hubplay_backup.yaml
```

### Restore

```bash
# 1. Stop HubPlay
systemctl stop hubplay  # or docker compose down

# 2. Replace database and config
cp hubplay_backup.db /config/hubplay.db
cp hubplay_backup.yaml /config/hubplay.yaml

# 3. Start HubPlay (auto-runs pending migrations if needed)
systemctl start hubplay  # or docker compose up -d
```

### What To Backup

| File | Location | Why |
|------|----------|-----|
| `hubplay.db` | `/config/` | All user data, watch progress, library metadata, sessions |
| `hubplay.yaml` | `/config/` | Server config, JWT secret, API keys |
| `plugins/` | `/config/plugins/` | Installed plugins + their config |

**NOT backed up** (regenerable):
- `/cache/` — transcoding cache, thumbnails (auto-regenerated on demand)
- Media files — admin's responsibility (separate backup strategy)

---

## 6. Monitoring & Health

### Health Endpoint

```
GET /api/v1/health          # No auth required
```

```json
{
    "status": "healthy",
    "version": "1.2.0",
    "uptime": "72h15m",
    "database": "ok",
    "ffmpeg": "ok",
    "active_streams": 3,
    "active_transcodes": 1,
    "disk": {
        "config_path": "/config",
        "config_free_gb": 45.2,
        "cache_path": "/cache",
        "cache_free_gb": 120.8,
        "cache_used_gb": 12.3
    }
}
```

Use with any monitoring tool (Uptime Kuma, Healthchecks.io, etc.):

```bash
# Simple health check
curl -sf http://localhost:8096/api/v1/health | jq .status
```

### Structured Logs

```json
{"level":"info","ts":"2026-03-13T10:15:00Z","msg":"stream_started","user":"alex","item":"tt1234567","profile":"1080p-hevc","hw_accel":"vaapi"}
{"level":"warn","ts":"2026-03-13T10:16:30Z","msg":"transcode_slow","item":"tt1234567","fps":18,"target_fps":24}
{"level":"info","ts":"2026-03-13T10:45:00Z","msg":"stream_ended","user":"alex","duration":"30m","bytes_sent":3221225472}
```

Docker logs:
```bash
docker compose logs hubplay -f --since 1h
```

### Key Metrics to Watch

| Metric | Source | Alert If |
|--------|--------|----------|
| Active transcodes | `/health` | > CPU core count |
| Disk free (cache) | `/health` | < 5 GB |
| Disk free (config) | `/health` | < 1 GB |
| FFmpeg status | `/health` | `"ffmpeg": "missing"` |
| Failed logins | Logs | Spike in `auth_failed` events |
| Memory usage | OS / Docker | > 80% of MemoryMax |

---

## 7. NAS-Specific Deployment

### Synology (Docker via Container Manager)

1. Install **Container Manager** from Package Center
2. Create shared folder: `/docker/hubplay/config`
3. Import `docker-compose.yml` via Container Manager UI
4. Map volumes:
   - `/docker/hubplay/config` → `/config`
   - `/volume1/media/movies` → `/media/movies` (read-only)
   - `/volume1/media/tv` → `/media/tv` (read-only)
5. Set resource limits: 2 CPU / 2GB RAM (minimum for transcoding)

### Unraid

```
Container template:
- Repository: hubplay/hubplay:latest
- Network: bridge
- Port: 8096 → 8096
- Path: /mnt/user/appdata/hubplay → /config
- Path: /mnt/user/media/movies → /media/movies (read-only)
- Path: /mnt/user/media/tv → /media/tv (read-only)
- Extra: --device=/dev/dri (for Intel HW transcoding)
```

### TrueNAS SCALE

Use TrueCharts or manual Docker Compose:
- Dataset: `tank/apps/hubplay` → `/config`
- Media datasets: mount as read-only
- GPU passthrough: Intel iGPU via device passthrough in TrueNAS UI

---

## 8. Common Operations

### Update HubPlay

```bash
# Docker
docker compose pull
docker compose up -d

# Native binary
systemctl stop hubplay
curl -L https://github.com/hubplay/hubplay/releases/latest/download/hubplay-linux-amd64 -o /usr/local/bin/hubplay
chmod +x /usr/local/bin/hubplay
systemctl start hubplay
# Migrations run automatically on startup
```

### Move Database from SQLite to PostgreSQL

```bash
# 1. Export from SQLite
hubplay db export --format sql --output hubplay_export.sql

# 2. Update config
# database.driver: postgres
# database.dsn: "host=..."

# 3. Start HubPlay (creates schema via migrations)
systemctl start hubplay

# 4. Import data
hubplay db import --input hubplay_export.sql

# 5. Verify
hubplay db status
```

### Reset Admin Password

```bash
# Interactive
hubplay user reset-password --username admin

# Non-interactive (useful for Docker)
docker compose exec hubplay hubplay user reset-password --username admin --password new-password
```

### Transcode Cache Cleanup

```bash
# Auto-cleanup: configured via transcoding.cache_max_size_gb
# Manual cleanup:
hubplay cache clear --type transcode

# Docker
docker compose exec hubplay hubplay cache clear --type transcode
```

---

## 9. Performance Tuning

### Transcoding

| Setting | Low-power (RPi/NAS) | Mid-range (4-core) | High-end (8+ cores / GPU) |
|---------|---------------------|---------------------|--------------------------|
| `threads` | 2 | 0 (auto) | 0 (auto) |
| `default_preset` | `ultrafast` | `fast` | `medium` |
| `hw_accel` | _(none)_ | `vaapi` / `qsv` | `nvenc` / `qsv` |
| Concurrent streams | 1 | 2-3 | 5-10+ |

### SQLite Tuning (large libraries >50K items)

```yaml
database:
  driver: "sqlite"
  path: "/config/hubplay.db"
  # These are set automatically, shown for reference:
  # pragma_cache_size: -128000    # 128MB (increase for large libraries)
  # pragma_mmap_size: 268435456   # 256MB memory-mapped I/O
```

### File Descriptor Limits

For servers with many concurrent streams:

```bash
# /etc/security/limits.d/hubplay.conf
hubplay soft nofile 65536
hubplay hard nofile 65536
```

Or in systemd: `LimitNOFILE=65536` (already in the service file above).

---

## 10. Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| "FFmpeg not found" at startup | FFmpeg not in PATH | Install FFmpeg or use Docker image (includes it) |
| "database is locked" | Too many concurrent writes (SQLite) | Switch to PostgreSQL, or reduce scanner parallelism |
| Transcoding very slow | Software decoding on weak CPU | Enable HW acceleration, use `ultrafast` preset |
| Out of disk on /cache | Transcode cache grew | Set `cache_max_size_gb`, or run `hubplay cache clear` |
| "permission denied" on media files | HubPlay user can't read media | `chmod o+r` on media files, or add `hubplay` user to media group |
| Stream buffering on remote | Bandwidth too low for bitrate | Lower max bitrate in streaming profile, or enable transcoding |
| Health check fails | Port not exposed, or HubPlay crashed | Check `docker compose logs`, verify port mapping |
| CORS errors in browser | Origins not configured | Set `cors.allowed_origins` in config |
| "too many open files" | File descriptor limit hit | Increase `LimitNOFILE` in systemd or `ulimit -n` |
