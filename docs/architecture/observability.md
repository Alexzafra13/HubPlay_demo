# Observability — Design Document

## Overview

Health checks, métricas internas, y logging estructurado para saber qué pasa en el servidor sin adivinar. Sin dependencias externas obligatorias — funciona standalone, pero compatible con Prometheus/Grafana si el admin lo quiere.

---

## 1. Health Check

### Endpoints

```
GET /health          → Quick check (para Docker HEALTHCHECK, load balancers)
GET /api/v1/system/info   → Detallado (para admin dashboard)
```

### Quick Health

```go
// internal/api/handlers/system.go
func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
    checks := map[string]string{
        "database": h.checkDB(),
        "ffmpeg":   h.checkFFmpeg(),
    }

    healthy := true
    for _, status := range checks {
        if status != "ok" {
            healthy = false
        }
    }

    status := http.StatusOK
    if !healthy {
        status = http.StatusServiceUnavailable
    }

    respondJSON(w, status, map[string]any{
        "status": boolToStatus(healthy),
        "checks": checks,
    })
}

func (h *SystemHandler) checkDB() string {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := h.db.PingContext(ctx); err != nil {
        return "error: " + err.Error()
    }
    return "ok"
}

func (h *SystemHandler) checkFFmpeg() string {
    if h.ffmpegPath == "" {
        return "not found"
    }
    return "ok"
}
```

**Response:**
```json
{
  "status": "healthy",
  "checks": {
    "database": "ok",
    "ffmpeg": "ok"
  }
}
```

### System Info (Admin)

```go
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]any{
        "version":   version,
        "commit":    commit,
        "go_version": runtime.Version(),
        "os":        runtime.GOOS,
        "arch":      runtime.GOARCH,
        "uptime_seconds": time.Since(h.startedAt).Seconds(),
        "database": map[string]any{
            "driver": h.cfg.Database.Driver,
            "size_bytes": h.getDBSize(),
        },
        "ffmpeg": map[string]any{
            "path":     h.ffmpegPath,
            "hw_accel": h.hwCaps,
        },
        "libraries": h.getLibraryStats(),
        "streaming": map[string]any{
            "active_sessions":  h.streaming.ActiveSessions(),
            "max_sessions":     h.cfg.Streaming.MaxTranscodeSessions,
        },
        "plugins": h.pluginMgr.List(),
        "federation": map[string]any{
            "enabled":     h.cfg.Federation.Enabled,
            "peers_online": h.countOnlinePeers(),
        },
    })
}
```

---

## 2. Internal Metrics

Métricas recopiladas en memoria, expuestas via API. Sin Prometheus obligatorio.

```go
// internal/metrics/metrics.go
type Metrics struct {
    mu sync.RWMutex

    // Streaming
    ActiveTranscodes   int
    TotalStreamsServed  int64
    DirectPlayCount    int64
    TranscodeCount     int64

    // Library
    TotalItems         int
    LastScanDuration   time.Duration
    LastScanAt         time.Time
    ScanErrors         int64

    // IPTV
    ActiveChannelStreams int
    TotalChannels       int

    // Auth
    ActiveSessions     int
    FailedLogins24h    int64

    // Federation
    PeersOnline        int
    PeersTotal         int

    // System
    StartedAt          time.Time
    RequestsTotal      int64
    RequestErrors5xx   int64
}
```

### API Endpoint

```
GET /api/v1/system/metrics    → (admin only)
```

```json
{
  "uptime_seconds": 86400,
  "streaming": {
    "active_transcodes": 1,
    "total_served": 342,
    "direct_play_pct": 78.5
  },
  "library": {
    "total_items": 1250,
    "last_scan": "2026-03-13T08:00:00Z",
    "last_scan_duration_ms": 7200
  },
  "iptv": {
    "active_streams": 2,
    "total_channels": 450
  },
  "federation": {
    "peers_online": 2,
    "peers_total": 3
  },
  "system": {
    "requests_total": 15420,
    "errors_5xx": 3,
    "memory_mb": 128,
    "goroutines": 42
  }
}
```

### Prometheus Export (Opcional)

Si el admin quiere Prometheus, un middleware expone `/metrics`:

```go
// Solo si config.observability.prometheus = true
// Usa la librería estándar prometheus/client_golang
```

No es una dependencia obligatoria — la librería solo se importa si se habilita en config.

---

## 3. Request Metrics Middleware

```go
// internal/api/middleware.go
func MetricsMiddleware(m *metrics.Metrics) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
            next.ServeHTTP(ww, r)

            atomic.AddInt64(&m.RequestsTotal, 1)
            if ww.Status() >= 500 {
                atomic.AddInt64(&m.RequestErrors5xx, 1)
            }
        })
    }
}
```

---

## 4. Admin Dashboard Data

El frontend admin consume estos endpoints:

```
/admin/system  page:
├── Server info (version, uptime, OS, Go version)
├── Database (driver, size, table counts)
├── FFmpeg (path, HW acceleration)
├── Storage (cache size, transcode temp, trickplay)
├── Active streams (who's watching what, direct play vs transcode)
├── Recent activity (last 50 events)
├── Background jobs (scheduler status, last run, errors)
├── Plugins (name, status, memory usage)
└── Federation (peer status, last sync)
```

---

## 5. Activity Log

Eventos importantes se guardan en `activity_log` para auditoría:

```go
// internal/activity/logger.go
type ActivityLogger struct {
    repo   ActivityRepository
    logger *slog.Logger
}

func (a *ActivityLogger) Log(ctx context.Context, entry ActivityEntry) {
    if err := a.repo.Create(ctx, entry); err != nil {
        a.logger.Error("failed to log activity", "error", err)
    }
}

type ActivityEntry struct {
    UserID   *uuid.UUID
    Type     string    // "login", "playback_start", "scan_complete", etc.
    ItemID   *uuid.UUID
    Severity string    // "info", "warning", "error"
    Message  string
    Data     map[string]any
}
```

### Eventos que se registran

| Evento | Severity | Quién lo genera |
|--------|----------|----------------|
| User login/logout | info | Auth middleware |
| Failed login attempt | warning | Auth service |
| Playback started | info | Stream handler |
| Library scan complete | info | Scanner |
| Scan errors | warning | Scanner |
| Plugin crash | error | Plugin manager |
| Federation peer linked/unlinked | info | Federation manager |
| Federation peer went offline | warning | Health check job |
| User created/deleted | info | User service |
| Config changed | info | Admin handler |

### Admin Activity API

```
GET /api/v1/admin/activity?type=login&severity=warning&limit=50&offset=0
```

Filtrable por tipo, severity, usuario, fecha. Paginado.

---

## 6. Structured Logging (resumen)

Detallado en [error-handling.md](./error-handling.md). Aquí el resumen operativo:

```yaml
# hubplay.yaml
logging:
  level: "info"        # debug para troubleshooting
  format: "json"       # json para producción, text para dev
  log_ips: true
```

Logs van a stdout — el admin los redirige a archivo, journald, o log aggregator según su setup.

```bash
# Ejemplo con journald (systemd)
journalctl -u hubplay -f

# Ejemplo con Docker
docker logs -f hubplay

# Ejemplo redirigir a archivo
./hubplay 2>&1 | tee /var/log/hubplay.log
```

---

## 7. Configuration

```yaml
# hubplay.yaml
observability:
  health_check_timeout: 2s     # Timeout para health checks internos
  metrics_enabled: true        # Exponer /api/v1/system/metrics
  prometheus: false            # Exponer /metrics en formato Prometheus
  activity_retention_days: 90  # Cuántos días guardar activity log
```
