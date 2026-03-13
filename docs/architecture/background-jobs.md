# Background Jobs & Scheduling — Design Document

## Overview

HubPlay tiene muchas tareas periódicas y background tasks. En lugar de que cada módulo gestione sus propios timers, un scheduler centralizado coordina todas las tareas con un API uniforme.

Sin dependencias externas — un scheduler simple basado en `time.Ticker` + goroutines es suficiente para un monolito.

---

## 1. Tareas que necesitan scheduling

| Tarea | Trigger | Intervalo Default | Módulo |
|-------|---------|-------------------|--------|
| Library scan (scheduled mode) | Cron/interval | Configurable por library (6h) | Scanner |
| EPG refresh | Interval | 12h | IPTV |
| M3U playlist refresh | Interval | 24h | IPTV |
| Federation catalog sync | Interval | 6h | Federation |
| Federation peer health check | Interval | 5min | Federation |
| Session cleanup (expired) | Interval | 1h | Auth |
| Transcode temp cleanup | Interval | 15min | Streaming |
| Trickplay generation | Event (`item.added`) | — (on-demand queue) | Trickplay |
| Webhook retry queue | Interval | 1min | Webhooks |
| Plugin health check | Interval | 30s | Plugins |
| Activity log pruning | Interval | 24h | System |

---

## 2. Scheduler Design

### Dos tipos de background work:

1. **Periodic Jobs** — se ejecutan en un intervalo fijo (EPG refresh cada 12h)
2. **Work Queues** — procesan items de una cola (trickplay generation, webhook retries)

```go
// internal/jobs/scheduler.go
type Scheduler struct {
    jobs    []registeredJob
    queues  []registeredQueue
    logger  *slog.Logger
    wg      sync.WaitGroup
    cancel  context.CancelFunc
}

type registeredJob struct {
    name     string
    interval time.Duration
    fn       func(ctx context.Context) error
    running  atomic.Bool
}

func NewScheduler(logger *slog.Logger) *Scheduler {
    return &Scheduler{logger: logger.With("module", "scheduler")}
}
```

### Register Jobs

```go
// Periodic job — se ejecuta cada `interval`
func (s *Scheduler) Register(name string, interval time.Duration, fn func(ctx context.Context) error) {
    s.jobs = append(s.jobs, registeredJob{
        name:     name,
        interval: interval,
        fn:       fn,
    })
}

// Ejemplo de registro en main.go:
func registerJobs(s *jobs.Scheduler, cfg *config.Config, ...) {
    // EPG refresh
    s.Register("epg-refresh", cfg.IPTV.EPGRefreshInterval, func(ctx context.Context) error {
        return channelMgr.RefreshAllEPG(ctx)
    })

    // M3U playlist refresh
    s.Register("playlist-refresh", cfg.IPTV.PlaylistRefreshInterval, func(ctx context.Context) error {
        return channelMgr.RefreshAllPlaylists(ctx)
    })

    // Federation catalog sync
    if cfg.Federation.Enabled {
        s.Register("federation-catalog-sync", cfg.Federation.CatalogSyncInterval, func(ctx context.Context) error {
            return federationMgr.SyncAllCatalogs(ctx)
        })

        s.Register("federation-peer-ping", 5*time.Minute, func(ctx context.Context) error {
            federationMgr.PingAllPeers(ctx)
            return nil
        })
    }

    // Session cleanup
    s.Register("session-cleanup", 1*time.Hour, func(ctx context.Context) error {
        cleaned, err := sessionRepo.DeleteExpired(ctx)
        if err != nil {
            return err
        }
        if cleaned > 0 {
            logger.Info("cleaned expired sessions", "count", cleaned)
        }
        return nil
    })

    // Transcode temp cleanup
    s.Register("transcode-cleanup", 15*time.Minute, func(ctx context.Context) error {
        return streamingMgr.CleanupIdle(5 * time.Minute)
    })

    // Activity log pruning (keep last 90 days)
    s.Register("activity-prune", 24*time.Hour, func(ctx context.Context) error {
        cutoff := clock.Now().Add(-90 * 24 * time.Hour)
        return activityRepo.DeleteBefore(ctx, cutoff)
    })

    // Plugin health
    s.Register("plugin-health", 30*time.Second, func(ctx context.Context) error {
        return pluginMgr.HealthCheckAll(ctx)
    })

    // Library scans (scheduled mode)
    for _, lib := range cfg.Libraries {
        if lib.ScanMode == "scheduled" {
            libID := lib.ID
            interval, _ := time.ParseDuration(lib.ScanInterval)
            s.Register("scan-"+lib.Name, interval, func(ctx context.Context) error {
                _, err := scanner.ScanLibrary(ctx, libID)
                return err
            })
        }
    }
}
```

### Start & Stop

```go
func (s *Scheduler) Start(ctx context.Context) {
    ctx, s.cancel = context.WithCancel(ctx)

    for i := range s.jobs {
        job := &s.jobs[i]
        s.wg.Add(1)
        go s.runJob(ctx, job)
    }

    s.logger.Info("scheduler started", "jobs", len(s.jobs))
}

func (s *Scheduler) runJob(ctx context.Context, job *registeredJob) {
    defer s.wg.Done()

    ticker := time.NewTicker(job.interval)
    defer ticker.Stop()

    // Ejecutar inmediatamente al inicio (no esperar el primer tick)
    s.executeJob(ctx, job)

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.executeJob(ctx, job)
        }
    }
}

func (s *Scheduler) executeJob(ctx context.Context, job *registeredJob) {
    // Evitar ejecuciones concurrentes del mismo job
    if !job.running.CompareAndSwap(false, true) {
        s.logger.Debug("job still running, skipping", "job", job.name)
        return
    }
    defer job.running.Store(false)

    start := time.Now()
    s.logger.Debug("job started", "job", job.name)

    if err := job.fn(ctx); err != nil {
        if ctx.Err() != nil {
            return // Shutdown — no loguear como error
        }
        s.logger.Error("job failed", "job", job.name, "error", err, "duration", time.Since(start))
        return
    }

    s.logger.Debug("job completed", "job", job.name, "duration", time.Since(start))
}

func (s *Scheduler) Stop(ctx context.Context) {
    s.cancel()

    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        s.logger.Info("scheduler stopped gracefully")
    case <-ctx.Done():
        s.logger.Warn("scheduler stop timed out")
    }
}
```

---

## 3. Work Queue (Trickplay, Webhook Retries)

Para tareas que se encolan por eventos (no periódicas):

```go
// internal/jobs/queue.go
type WorkQueue[T any] struct {
    name       string
    ch         chan T
    maxWorkers int
    handler    func(ctx context.Context, item T) error
    logger     *slog.Logger
    wg         sync.WaitGroup
}

func NewWorkQueue[T any](name string, maxWorkers, bufferSize int, handler func(ctx context.Context, item T) error, logger *slog.Logger) *WorkQueue[T] {
    return &WorkQueue[T]{
        name:       name,
        ch:         make(chan T, bufferSize),
        maxWorkers: maxWorkers,
        handler:    handler,
        logger:     logger.With("queue", name),
    }
}

func (q *WorkQueue[T]) Start(ctx context.Context) {
    for i := 0; i < q.maxWorkers; i++ {
        q.wg.Add(1)
        go q.worker(ctx, i)
    }
    q.logger.Info("queue started", "workers", q.maxWorkers)
}

func (q *WorkQueue[T]) Enqueue(item T) {
    select {
    case q.ch <- item:
    default:
        q.logger.Warn("queue full, dropping item")
    }
}

func (q *WorkQueue[T]) worker(ctx context.Context, id int) {
    defer q.wg.Done()
    for {
        select {
        case <-ctx.Done():
            return
        case item := <-q.ch:
            if err := q.handler(ctx, item); err != nil {
                if ctx.Err() != nil {
                    return
                }
                q.logger.Error("worker error", "worker", id, "error", err)
            }
        }
    }
}

func (q *WorkQueue[T]) Stop(ctx context.Context) {
    done := make(chan struct{})
    go func() {
        q.wg.Wait()
        close(done)
    }()
    select {
    case <-done:
    case <-ctx.Done():
    }
}
```

### Uso: Trickplay Queue

```go
// En main.go
trickplayQueue := jobs.NewWorkQueue[uuid.UUID](
    "trickplay",
    cfg.Streaming.Trickplay.MaxWorkers, // e.g. 2
    100,                                 // buffer
    func(ctx context.Context, itemID uuid.UUID) error {
        return trickplayGen.Generate(ctx, itemID)
    },
    logger,
)

// Suscribirse al evento
eventBus.Subscribe(event.ItemAdded, func(e event.Event) {
    trickplayQueue.Enqueue(e.ItemID)
})
```

### Uso: Webhook Retry Queue

```go
webhookRetryQueue := jobs.NewWorkQueue[webhook.PendingDelivery](
    "webhook-retry",
    3,   // workers
    200, // buffer
    func(ctx context.Context, delivery webhook.PendingDelivery) error {
        return webhookDispatcher.Deliver(ctx, delivery)
    },
    logger,
)
```

---

## 4. Event-Driven vs Scheduled

| Patrón | Cuándo usar | Ejemplo |
|--------|------------|---------|
| **Scheduled** (periodic) | Tarea recurrente, sin trigger externo | EPG refresh, session cleanup |
| **Event → Queue** | Reacción a un cambio, puede acumular trabajo | Trickplay (on `item.added`), webhook delivery |
| **Event → Direct** | Reacción inmediata, ligera, sin backpressure | WebSocket notify, search index update |

```
Event Bus ──┬── WebSocket Hub (direct, instant notify)
            ├── Search Indexer (direct, update FTS)
            ├── Trickplay Queue (queued, heavy work)
            └── Webhook Queue (queued, external HTTP)

Scheduler ──┬── EPG Refresh (periodic)
            ├── Session Cleanup (periodic)
            ├── Federation Sync (periodic)
            └── Library Scan (periodic, if scheduled mode)
```

---

## 5. Resilience

### Job Failure Handling
- **Periodic jobs**: error se loguea, job se reintenta en el siguiente intervalo
- **Queue items**: error se loguea, item se descarta (para webhooks: se reencola con backoff)
- **Ningún job falla silenciosamente** — siempre hay un log

### Concurrency Safety
- `running.CompareAndSwap` previene ejecuciones concurrentes del mismo periodic job
- WorkQueue tiene workers fijos — backpressure natural via buffer size
- Todos los jobs reciben `context.Context` — se cancelan en shutdown

### Monitoring
- Cada job loguea inicio, fin, duración y errores
- Admin API endpoint para ver estado de jobs:

```
GET /api/v1/system/jobs
→ [{
    "name": "epg-refresh",
    "type": "periodic",
    "interval": "12h",
    "last_run": "2026-03-13T08:00:00Z",
    "last_duration_ms": 3200,
    "last_error": null,
    "running": false
}]
```

---

## 6. Directory Structure

```
internal/
├── jobs/
│   ├── scheduler.go    # Periodic job scheduler
│   ├── queue.go        # Generic work queue
│   └── scheduler_test.go
```
