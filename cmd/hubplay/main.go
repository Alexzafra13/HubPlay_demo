package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	hubplay "hubplay"
	"hubplay/internal/api"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/library"
	"hubplay/internal/logging"
	"hubplay/internal/iptv"
	"hubplay/internal/observability"
	"hubplay/internal/probe"
	"hubplay/internal/provider"
	"hubplay/internal/scanner"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
	"hubplay/internal/user"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "hubplay.yaml", "path to config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "hubplay: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ═══ Phase 1: Foundation ═══
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := logging.New(cfg.Logging)
	slog.SetDefault(logger)
	clk := clock.New()

	logger.Info("starting HubPlay", "version", version, "addr", cfg.Server.Addr())

	// Preflight: validate external binaries and filesystem permissions
	// before any service is built. Catching these here means "ffmpeg not
	// installed" shows up as a clear boot error instead of an opaque 500
	// during the first user's stream attempt.
	if err := cfg.Preflight(logger); err != nil {
		return fmt.Errorf("preflight checks failed:\n%w", err)
	}

	// ═══ Phase 2: Database ═══
	database, err := db.Open(cfg.Database.Driver, cfg.Database.Path, logger)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close() //nolint:errcheck

	if err := db.Migrate(database, hubplay.SQLiteMigrations, logger); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	repos := db.NewRepositories(database)

	// ═══ Phase 3: Infrastructure ═══
	eventBus := event.NewBus(logger)

	// Observability: one registry shared by every collector. A construction
	// error here is a programmer error (duplicate metric name) — fail fast
	// so it shows up in CI, not in a production scrape.
	metrics, err := observability.NewMetrics(version)
	if err != nil {
		return fmt.Errorf("initialising metrics: %w", err)
	}

	// ═══ Phase 4: Core Services ═══
	//
	// JWT signing keys live in the DB so they survive restarts and can be
	// rotated without editing config. On first boot (empty table) we seed
	// the keystore with the config secret so any tokens that were issued
	// before this upgrade keep validating; subsequent boots pick up existing
	// keys verbatim.
	if _, err := auth.Bootstrap(ctx, repos.SigningKeys, clk, cfg.Auth.JWTSecret); err != nil {
		return fmt.Errorf("bootstrapping signing keys: %w", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, repos.SigningKeys, clk)
	if err != nil {
		return fmt.Errorf("loading signing keys: %w", err)
	}
	logger.Info("signing keys loaded", "active", keyStore.ActiveCount(), "retired", keyStore.RetiredCount())

	// Expose live keystore counts to Prometheus via GaugeFunc so the metric
	// never drifts from the DB (see observability/auth.go for rationale).
	if err := observability.RegisterKeyStoreGauges(metrics, keyStore); err != nil {
		return fmt.Errorf("registering keystore gauges: %w", err)
	}

	authService := auth.NewService(repos.Users, repos.Sessions, keyStore, cfg.Auth, clk, logger, cfg.RateLimit)
	authService.SetEventBus(eventBus)
	authService.StartSessionCleaner(ctx)
	userService := user.NewService(repos.Users, logger)

	prober := probe.New()

	// ═══ Phase 4d: Providers (TMDb, Fanart.tv, OpenSubtitles) ═══
	providerManager := provider.NewManager(repos.Providers, logger)
	_ = providerManager.Register(ctx, provider.NewTMDbProvider())
	_ = providerManager.Register(ctx, provider.NewFanartProvider())
	_ = providerManager.Register(ctx, provider.NewOpenSubtitlesProvider())

	scnr := scanner.New(repos.Items, repos.MediaStreams, repos.Metadata, repos.ExternalIDs, repos.Images, providerManager, prober, eventBus, logger)
	libraryService := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, repos.Channels, scnr, logger)

	// ═══ Phase 4a: Library Scan Scheduler ═══
	scanScheduler := library.NewScheduler(libraryService, logger)
	scanScheduler.Start(ctx)

	// ═══ Phase 4b: Streaming ═══
	streamManager := stream.NewManager(repos.Items, repos.MediaStreams, cfg.Streaming, logger)
	streamManager.SetMetrics(observability.NewStreamSink(metrics))
	streamManager.SetEventBus(eventBus)

	// Detect hardware acceleration if enabled
	if cfg.Streaming.HWAccel.Enabled {
		hwResult := stream.DetectHWAccel(cfg.Streaming.HWAccel.Preferred, logger)
		logger.Info("hardware acceleration",
			"available", hwResult.Available,
			"selected", hwResult.Selected,
			"encoder", hwResult.Encoder,
		)
	}

	// ═══ Phase 4c: IPTV ═══
	iptvService := iptv.NewService(repos.Channels, repos.EPGPrograms, repos.Libraries, repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides, logger)
	iptvService.SetEventBus(eventBus)
	iptvProxy := iptv.NewStreamProxy(logger)
	// Wire health reporting now that both pieces exist. The proxy
	// records probe outcomes against the channel repo through the
	// service so dead upstreams drop out of the user view.
	iptvProxy.SetHealthReporter(iptvService)

	// ═══ Phase 4e: Setup Service ═══
	setupService := setup.NewService(cfg, configPath, logger)

	// ═══ Phase 5: HTTP Server ═══
	webFS, _ := fs.Sub(hubplay.WebAssets, "web/dist")

	router := api.NewRouter(api.Dependencies{
		Auth:          authService,
		Users:         userService,
		Libraries:     libraryService,
		StreamManager: streamManager,
		IPTV:          iptvService,
		IPTVProxy:     iptvProxy,
		Items:         repos.Items,
		MediaStreams:   repos.MediaStreams,
		Images:        repos.Images,
		Metadata:      repos.Metadata,
		UserData:        repos.UserData,
		UserPreferences: repos.UserPreferences,
		Providers:     providerManager,
		ExternalIDs:   repos.ExternalIDs,
		LibraryRepo:   repos.Libraries,
		ProviderRepo:  repos.Providers,
		SetupService:  setupService,
		EventBus:      eventBus,
		Database:      database,
		Version:       version,
		WebAssets:     webFS,
		Config:        cfg,
		Logger:        logger,
		Metrics:       metrics,
	})

	server := &http.Server{
		Addr:         cfg.Server.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Streaming endpoints need unlimited write time
		IdleTimeout:  60 * time.Second,
	}

	// ═══ Phase 6: Start ═══
	go func() {
		logger.Info("server started", "addr", cfg.Server.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	// ═══ Phase 7: Wait for shutdown ═══
	return waitForShutdown(ctx, cancel, server, streamManager, iptvService, iptvProxy, scanScheduler, libraryService, authService, database, logger)
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, server *http.Server, sm *stream.Manager, iptvSvc *iptv.Service, iptvProxy *iptv.StreamProxy, scheduler *library.Scheduler, librarySvc *library.Service, authSvc *auth.Service, database interface{ Close() error }, logger *slog.Logger) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig)
	case <-ctx.Done():
		logger.Info("context cancelled, shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.Info("starting graceful shutdown...")

	// Stop background services
	scheduler.Stop()
	logger.Info("scan scheduler stopped")
	authSvc.StopSessionCleaner()
	logger.Info("session cleaner stopped")

	// Stop HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Stop all streaming sessions
	sm.Shutdown()
	logger.Info("stream manager stopped")

	// Stop IPTV
	iptvProxy.Shutdown()
	iptvSvc.Shutdown()
	logger.Info("IPTV services stopped")

	// Drain in-flight auto-scan goroutines BEFORE closing the DB so they
	// don't race on "sql: database is closed".
	librarySvc.Shutdown()
	logger.Info("library service stopped")

	// Cancel root context
	cancel()

	// Close database
	if err := database.Close(); err != nil {
		logger.Error("database close error", "error", err)
	}
	logger.Info("database closed")

	logger.Info("shutdown complete")
	return nil
}
