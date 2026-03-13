# Wiring, Initialization & Lifecycle — Design Document

## Overview

Cómo se conectan los módulos entre sí, en qué orden se inicializan, y cómo se apagan limpiamente. Manual dependency injection en `main.go` — sin frameworks de DI.

---

## 1. Dependency Injection — Manual

Go idiomático: constructores reciben dependencias como parámetros. Sin magia, sin reflexión, sin contenedores.

```go
// Patrón: cada servicio recibe sus dependencias por constructor
func NewScanner(
    itemRepo    ItemRepository,
    libraryRepo LibraryRepository,
    analyzer    MediaAnalyzer,
    metadata    MetadataManager,
    eventBus    EventBus,
    clock       Clock,
    logger      *slog.Logger,
) *Scanner
```

**Ventajas:**
- Compile-time safe — si falta una dependencia, no compila
- Fácil de testear — inyectar mocks por constructor
- Fácil de entender — el grafo de dependencias es explícito en main.go
- Sin reflexión — zero overhead

---

## 2. Dependency Graph

```
                            ┌──────────┐
                            │  Config  │
                            └────┬─────┘
                                 │
                 ┌───────────────┼───────────────┐
                 ▼               ▼               ▼
           ┌──────────┐   ┌──────────┐    ┌──────────┐
           │  Logger   │   │ Database │    │  Clock   │
           │  (slog)   │   │ (sql.DB) │    │          │
           └─────┬─────┘   └────┬─────┘    └────┬─────┘
                 │               │               │
                 │          ┌────┴────┐          │
                 │          ▼         ▼          │
                 │   ┌───────────┐ ┌───────────┐│
                 │   │ sqlc      │ │ goose     ││
                 │   │ Queries   │ │ Migrations ││
                 │   └─────┬─────┘ └───────────┘│
                 │         │                     │
        ┌────────┼─────────┼─────────────────────┤
        ▼        ▼         ▼                     ▼
  ┌──────────────────────────────────────────────────┐
  │              Repository Layer                      │
  │  ItemRepo  LibraryRepo  UserRepo  ChannelRepo     │
  │  MetadataRepo  SessionRepo  ProgressRepo  ...     │
  └────────────────────┬──────────────────────────────┘
                       │
     ┌─────────────────┼─────────────────────────┐
     ▼                 ▼                         ▼
┌──────────┐    ┌──────────────┐          ┌──────────────┐
│ Event Bus │    │ Core Services │          │ Infrastructure│
│           │    │              │          │              │
│           │◄───│ AuthService  │          │ FFmpegBuilder│
│           │    │ UserService  │          │ HWAccel      │
│           │    │ Scanner      │          │ MediaAnalyzer│
│           │    │ MetadataMgr  │          │              │
│           │    │ ProgressSvc  │          └──────┬───────┘
│           │    │ FavoriteSvc  │                 │
│           │    │ ChannelMgr   │                 │
│           │    └──────┬───────┘                 │
│           │           │                         │
│           │    ┌──────┴─────────────────────────┤
│           │    ▼                                ▼
│           │  ┌──────────────────────────────────────┐
│           │  │         Composite Services            │
│           │  │  StreamingManager (uses FFmpeg + repos)│
│           │  │  FederationManager (uses HTTP client)  │
│           │  │  PluginManager (uses gRPC + process)   │
│           │  │  TrickplayGenerator (uses FFmpeg)      │
│           │  │  WebhookDispatcher (uses HTTP + tmpl)  │
│           │  └──────────────────┬────────────────────┘
│           │                     │
│           │              ┌──────┴──────┐
│           │              ▼             ▼
│           │        ┌──────────┐  ┌──────────────┐
└───────────┼───────►│ WebSocket│  │ Background   │
            │        │ Hub      │  │ Job Scheduler│
            │        └────┬─────┘  └──────┬───────┘
            │             │               │
            ▼             ▼               ▼
     ┌─────────────────────────────────────────┐
     │              HTTP Router (chi)            │
     │  Middleware: RealIP → RequestID → Logger  │
     │  → Recoverer → CORS → RateLimit → Auth   │
     │                                           │
     │  /api/v1/* → Handlers                     │
     │  /api/v1/ws → WebSocket Hub               │
     │  /* → SPA (embedded frontend)             │
     └──────────────────┬──────────────────────┘
                        │
                        ▼
                 ┌──────────────┐
                 │ http.Server  │
                 │ :8096        │
                 └──────────────┘
```

---

## 3. Initialization Order (`main.go`)

El orden importa: cada paso depende del anterior.

```go
func main() {
    // ═══════════════════════════════════════════
    // Phase 1: Foundation (no dependencies)
    // ═══════════════════════════════════════════
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 1.1 Config
    cfg, err := config.Load(configPath)
    exitOnError(err, "loading config")

    // 1.2 Logger
    logger := logging.New(cfg.Logging)

    // 1.3 Clock
    clock := clock.New() // RealClock in prod, MockClock in tests

    // ═══════════════════════════════════════════
    // Phase 2: Database
    // ═══════════════════════════════════════════
    // 2.1 Open database
    database, err := db.Open(cfg.Database, logger)
    exitOnError(err, "opening database")
    defer database.Close()

    // 2.2 Run migrations
    err = db.Migrate(database, cfg.Database.Driver)
    exitOnError(err, "running migrations")

    // 2.3 Create repositories (sqlc-generated queries)
    repos := db.NewRepositories(database)

    // ═══════════════════════════════════════════
    // Phase 3: Infrastructure
    // ═══════════════════════════════════════════
    // 3.1 Event bus
    eventBus := event.NewBus(logger)

    // 3.2 FFmpeg detection
    ffmpegPath, err := ffmpeg.Detect()
    if err != nil {
        logger.Warn("FFmpeg not found — transcoding disabled", "error", err)
    }
    hwCaps, _ := ffmpeg.DetectHardwareAccel(ctx, ffmpegPath)
    ffBuilder := ffmpeg.NewBuilder(ffmpegPath, hwCaps)
    analyzer := ffmpeg.NewProbe(ffmpegPath, cfg.Scanner.ProbeWorkers)

    // ═══════════════════════════════════════════
    // Phase 4: Core Services
    // ═══════════════════════════════════════════
    authService := auth.NewService(repos.Users, repos.Sessions, cfg.Auth, clock, logger)
    userService := user.NewService(repos.Users, logger)
    progressService := progress.NewService(repos.Progress, repos.Items, eventBus, logger)
    favoriteService := progress.NewFavoriteService(repos.Favorites, logger)

    metadataMgr := metadata.NewManager(
        repos.Metadata, repos.Images, repos.ExternalIDs, repos.People,
        metadata.NewTMDbProvider(cfg.Metadata.TMDb.APIKey),
        metadata.NewFanartProvider(cfg.Metadata.Fanart.APIKey),
        logger,
    )

    scanner := library.NewScanner(
        repos.Items, repos.Libraries, analyzer, metadataMgr, eventBus, clock, logger,
    )

    watcher := library.NewWatcher(scanner, repos.Libraries, eventBus, logger)

    channelMgr := iptv.NewManager(
        repos.Channels, repos.EPG, eventBus, logger,
    )

    // ═══════════════════════════════════════════
    // Phase 5: Composite Services
    // ═══════════════════════════════════════════
    streamingMgr := streaming.NewManager(
        repos.Items, repos.Streams, ffBuilder, cfg.Streaming, logger,
    )

    trickplayGen := trickplay.NewGenerator(
        repos.Trickplay, ffBuilder, cfg.Streaming.Trickplay, logger,
    )

    webhookDispatcher := webhook.NewDispatcher(
        repos.Webhooks, repos.WebhookLog, eventBus, logger,
    )

    pluginMgr := plugin.NewManager(cfg.Plugins.Dir, eventBus, logger)

    federationMgr := federation.NewManager(
        repos.Federation, repos.Identity, repos.Libraries, repos.Items,
        streamingMgr, cfg.Federation, clock, logger,
    )

    // ═══════════════════════════════════════════
    // Phase 6: Event Subscriptions
    // ═══════════════════════════════════════════
    subscribeEvents(eventBus, trickplayGen, webhookDispatcher, logger)

    // ═══════════════════════════════════════════
    // Phase 7: Background Jobs
    // ═══════════════════════════════════════════
    scheduler := jobs.NewScheduler(logger)
    registerJobs(scheduler, cfg, channelMgr, federationMgr, repos.Sessions, clock, logger)

    // ═══════════════════════════════════════════
    // Phase 8: HTTP Server
    // ═══════════════════════════════════════════
    wsHub := ws.NewHub(eventBus, logger)

    router := api.NewRouter(api.Dependencies{
        Auth:          authService,
        Users:         userService,
        Scanner:       scanner,
        Items:         repos.Items,
        Libraries:     repos.Libraries,
        Metadata:      metadataMgr,
        Streaming:     streamingMgr,
        Channels:      channelMgr,
        Progress:      progressService,
        Favorites:     favoriteService,
        Plugins:       pluginMgr,
        Federation:    federationMgr,
        Webhooks:      webhookDispatcher,
        WSHub:         wsHub,
        Config:        cfg,
        Logger:        logger,
    })

    server := &http.Server{
        Addr:         cfg.Server.Addr(),
        Handler:      router,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 0,  // Streaming endpoints need unlimited write time
        IdleTimeout:  60 * time.Second,
    }

    // ═══════════════════════════════════════════
    // Phase 9: Start Everything
    // ═══════════════════════════════════════════
    // Start in dependency order
    pluginMgr.LoadAll(ctx)
    watcher.StartAll(ctx)
    scheduler.Start(ctx)
    wsHub.Start(ctx)

    // Start HTTP server (blocks in goroutine)
    go func() {
        logger.Info("server started", "addr", cfg.Server.Addr())
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("server error", "error", err)
            cancel()
        }
    }()

    // ═══════════════════════════════════════════
    // Phase 10: Wait for shutdown signal
    // ═══════════════════════════════════════════
    waitForShutdown(ctx, cancel, server, scheduler, watcher, streamingMgr,
        pluginMgr, wsHub, database, logger)
}
```

---

## 4. Dependencies Struct

Agrupa todas las dependencias del router en un struct explícito. Evita pasar 20 parámetros.

```go
// internal/api/deps.go
type Dependencies struct {
    Auth       auth.Service
    Users      user.Service
    Scanner    library.Scanner
    Items      db.ItemRepository
    Libraries  db.LibraryRepository
    Metadata   metadata.Manager
    Streaming  streaming.Manager
    Channels   iptv.ChannelManager
    Progress   progress.Service
    Favorites  progress.FavoriteService
    Plugins    plugin.Manager
    Federation federation.Manager
    Webhooks   webhook.Dispatcher
    WSHub      *ws.Hub
    Config     *config.Config
    Logger     *slog.Logger
}
```

---

## 5. Graceful Shutdown

Apagar en orden inverso a la inicialización. Dar tiempo a cada componente.

```go
func waitForShutdown(
    ctx context.Context,
    cancel context.CancelFunc,
    server *http.Server,
    scheduler *jobs.Scheduler,
    watcher *library.Watcher,
    streaming streaming.Manager,
    plugins plugin.Manager,
    wsHub *ws.Hub,
    database *sql.DB,
    logger *slog.Logger,
) {
    // Esperar señal del OS (SIGINT, SIGTERM)
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    select {
    case sig := <-sigCh:
        logger.Info("shutdown signal received", "signal", sig)
    case <-ctx.Done():
        logger.Info("context cancelled, shutting down")
    }

    // Timeout total para shutdown
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer shutdownCancel()

    logger.Info("starting graceful shutdown...")

    // ─── Phase 1: Stop accepting new work ───
    // Stop HTTP server (finish in-flight requests, reject new ones)
    if err := server.Shutdown(shutdownCtx); err != nil {
        logger.Error("HTTP server shutdown error", "error", err)
    }
    logger.Info("HTTP server stopped")

    // ─── Phase 2: Stop background processes ───
    // Stop scheduler (wait for running jobs to finish)
    scheduler.Stop(shutdownCtx)
    logger.Info("scheduler stopped")

    // Stop file watcher
    watcher.StopAll()
    logger.Info("file watcher stopped")

    // Close WebSocket connections (notify clients)
    wsHub.Shutdown()
    logger.Info("WebSocket hub closed")

    // ─── Phase 3: Stop active sessions ───
    // Kill transcoding sessions (FFmpeg processes)
    streaming.StopAll()
    logger.Info("transcoding sessions terminated")

    // Stop plugins (gRPC child processes)
    plugins.StopAll(shutdownCtx)
    logger.Info("plugins stopped")

    // ─── Phase 4: Close data layer ───
    // Cancel root context (stops any remaining goroutines)
    cancel()

    // Close database (flushes WAL)
    if err := database.Close(); err != nil {
        logger.Error("database close error", "error", err)
    }
    logger.Info("database closed")

    logger.Info("shutdown complete")
}
```

### Shutdown Order (y por qué)

| Paso | Componente | Timeout | Por qué este orden |
|------|-----------|---------|-------------------|
| 1 | HTTP Server | 10s | Dejar de aceptar requests, terminar in-flight |
| 2 | Scheduler | 5s | Esperar jobs actuales (EPG refresh, catalog sync) |
| 3 | File Watcher | instant | Solo cierra channels de fsnotify |
| 4 | WebSocket Hub | instant | Envía "server shutting down" a clientes |
| 5 | Streaming Manager | 5s | Kill FFmpeg processes, limpiar temp dirs |
| 6 | Plugin Manager | 5s | SIGTERM a plugins, esperar salida limpia |
| 7 | Context cancel | instant | Señal a todas las goroutines restantes |
| 8 | Database | instant | Flush WAL, cerrar conexiones |

---

## 6. Service Interfaces

Todas las dependencias entre módulos se definen por interfaces. El módulo que consume define la interfaz (no el que la implementa).

```go
// El scanner define qué necesita del analyzer:
// internal/library/scanner.go
type mediaAnalyzer interface {
    Analyze(ctx context.Context, path string) (*media.AnalysisResult, error)
}

// El handler define qué necesita del service:
// internal/api/handlers/items.go
type itemLister interface {
    GetByLibrary(ctx context.Context, libID uuid.UUID, opts db.ListOptions) ([]media.Item, int, error)
    GetByID(ctx context.Context, id uuid.UUID) (*media.Item, error)
}
```

**Principio**: interfaces pequeñas donde se usan, no interfaces grandes donde se implementan. Esto facilita el testing y reduce acoplamiento.

---

## 7. Configuration Validation

El config se valida al cargar, antes de crear cualquier servicio.

```go
// internal/config/config.go
func Load(path string) (*Config, error) {
    cfg := &Config{}

    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading config: %w", err)
    }

    // Expand environment variables (${TMDB_API_KEY})
    data = []byte(os.ExpandEnv(string(data)))

    if err := yaml.Unmarshal(data, cfg); err != nil {
        return nil, fmt.Errorf("parsing config: %w", err)
    }

    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }

    return cfg, nil
}

func (c *Config) Validate() error {
    var errs []error

    if c.Server.Port < 1 || c.Server.Port > 65535 {
        errs = append(errs, fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port))
    }
    if c.Auth.BCryptCost < 10 || c.Auth.BCryptCost > 14 {
        errs = append(errs, fmt.Errorf("auth.bcrypt_cost must be 10-14, got %d", c.Auth.BCryptCost))
    }
    // Validar paths de libraries existen
    for i, lib := range c.Libraries {
        for _, p := range lib.Paths {
            if !filepath.IsAbs(p) {
                errs = append(errs, fmt.Errorf("libraries[%d].paths: %q must be absolute", i, p))
            }
        }
    }

    return errors.Join(errs...)
}
```

---

## 8. Testability

La estructura de wiring hace que cada capa sea independientemente testeable:

| Test | Qué se inyecta | Qué se mockea |
|------|----------------|---------------|
| Repo tests | `*sql.DB` (SQLite `:memory:`) | Nada — DB real |
| Service tests | Repo interface | MockRepo, MockAnalyzer |
| Handler tests | Service interface | MockService |
| E2E tests | Todo real | Solo APIs externas (TMDb) |

```go
// Para tests de integración completos:
func NewTestApp(t *testing.T) *TestApp {
    cfg := config.TestConfig()
    db := testutil.NewTestDB(t)
    repos := db.NewRepositories(db)
    eventBus := event.NewBus(slog.Default())
    clock := &testutil.MockClock{Now: time.Now()}

    // Construir servicios con mocks de infraestructura
    app := &TestApp{
        DB:       db,
        Repos:    repos,
        EventBus: eventBus,
        Clock:    clock,
        Auth:     auth.NewService(repos.Users, repos.Sessions, cfg.Auth, clock, slog.Default()),
        // ... etc
    }
    app.Router = api.NewRouter(app.toDependencies())
    return app
}
```

---

## 9. Directory Structure (Wiring)

```
cmd/
└── hubplay/
    └── main.go              # Wiring: create deps, start server, handle shutdown
internal/
├── config/
│   ├── config.go            # Config struct, Load(), Validate()
│   └── config_test.go       # Validation tests
├── clock/
│   └── clock.go             # Clock interface + RealClock
├── api/
│   ├── deps.go              # Dependencies struct
│   ├── router.go            # NewRouter(deps) — route registration
│   └── middleware.go        # Middleware stack
└── ...
```
