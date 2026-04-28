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
	"path/filepath"
	"syscall"
	"time"

	hubplay "hubplay"
	"hubplay/internal/api"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/imaging/pathmap"
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

	// Image storage shared with the HTTP image handler/refresher: the
	// scanner downloads every poster/backdrop here so re-scans and
	// admin refreshes write to the same root. Path-mapping is plain
	// filesystem state, so it's safe to instantiate independent
	// `pathmap.Store`s pointing at the same directory.
	imageDir := filepath.Join(filepath.Dir(cfg.Database.Path), "images")
	scannerPathmap := pathmap.New(imageDir)

	scnr := scanner.New(repos.Items, repos.MediaStreams, repos.Metadata, repos.ExternalIDs, repos.Images, repos.Chapters, providerManager, prober, eventBus, imageDir, scannerPathmap, logger)
	libraryService := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, repos.Channels, scnr, logger)

	// ═══ Phase 4a: Library Scan Scheduler ═══
	scanScheduler := library.NewScheduler(libraryService, logger)
	scanScheduler.Start(ctx)

	// ═══ Phase 4b: Streaming ═══
	streamManager := stream.NewManager(repos.Items, repos.MediaStreams, cfg.Streaming, logger)
	streamManager.SetMetrics(observability.NewStreamSink(metrics))
	streamManager.SetEventBus(eventBus)

	// Hardware acceleration detection happens inside `stream.NewManager`
	// when `cfg.Streaming.HWAccel.Enabled` is true — the result both
	// gets logged there and threaded into the transcoder. Detecting
	// twice (here + there) was the prior shape; the result was logged
	// here and silently discarded, leaving every transcode on libx264.

	// ═══ Phase 4c: IPTV ═══
	iptvService := iptv.NewService(repos.Channels, repos.EPGPrograms, repos.Libraries, repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides, repos.ChannelWatchHistory, logger)
	iptvService.SetEventBus(eventBus)
	iptvProxy := iptv.NewStreamProxy(logger)
	// Wire health reporting now that both pieces exist. The proxy
	// records probe outcomes against the channel repo through the
	// service so dead upstreams drop out of the user view.
	iptvProxy.SetHealthReporter(iptvService)

	// Live MPEG-TS → HLS transmux. Required for Xtream Codes /
	// raw-TS providers (most non-public IPTV today). Optional: if
	// disabled in config, the channel-stream handler falls back to
	// the raw passthrough proxy and only HLS providers play in the
	// browser. The work dir is rooted under the streaming cache dir
	// so a single volume mount covers both VOD transcoding and live
	// transmux output.
	var iptvTransmux *iptv.TransmuxManager
	if cfg.IPTV.Transmux.Enabled {
		transmuxCacheDir := filepath.Join(cfg.Streaming.EffectiveCacheDir(), "iptv-hls")
		// Share the proxy's circuit breaker with the transmux manager
		// so failures on either plane (HLS proxy or MPEG-TS transmux)
		// trip the same per-channel cooldown. Without this, a dead
		// Xtream upstream produced a fork-bomb of failed ffmpeg spawns
		// every time the player retried the manifest.
		iptvTransmux = iptv.NewTransmuxManager(iptv.TransmuxManagerConfig{
			CacheDir:     transmuxCacheDir,
			MaxSessions:  cfg.IPTV.Transmux.MaxSessions,
			IdleTimeout:  cfg.IPTV.Transmux.IdleTimeout,
			ReadyTimeout: cfg.IPTV.Transmux.ReadyTimeout,
			Gate:         iptvProxy.Breaker(),
			Reporter:     iptvService,
			Metrics:      observability.NewIPTVTransmuxSink(metrics),
		}, logger)
		if err := observability.RegisterIPTVTransmuxGauges(metrics, iptvTransmux); err != nil {
			return fmt.Errorf("register iptv transmux gauges: %w", err)
		}
		logger.Info("iptv transmux enabled",
			"cache_dir", transmuxCacheDir,
			"max_sessions", cfg.IPTV.Transmux.MaxSessions)
	}

	// ═══ Phase 4d: IPTV Scheduler ═══
	// Runs periodic M3U + EPG refreshes per configured schedule so the
	// product no longer requires an admin to click "Refrescar" every
	// morning. Sequential — see scheduler.go for why.
	iptvScheduler := iptv.NewScheduler(repos.IPTVSchedules, iptvService, logger)
	iptvScheduler.Start(ctx)

	// Active stream prober: walks every livetv library every few hours
	// and records a probe outcome against each channel via the same
	// ChannelHealthReporter the proxy uses. Catches dead upstreams on
	// channels nobody happens to be watching, so the user-facing list
	// auto-hides them before a viewer clicks a dead tile.
	iptvProber := iptv.NewProber(nil, iptvService)
	iptvProberWorker := iptv.NewProberWorker(iptvProber, repos.Libraries, repos.Channels, logger)
	iptvProberWorker.Start(ctx)
	iptvService.SetProberWorker(iptvProberWorker)

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
		IPTVTransmux:  iptvTransmux,
		IPTVScheduler: iptvScheduler,
		IPTVSchedules: repos.IPTVSchedules,
		Items:         repos.Items,
		MediaStreams:   repos.MediaStreams,
		Images:        repos.Images,
		Metadata:      repos.Metadata,
		UserData:        repos.UserData,
		Chapters:        repos.Chapters,
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
	return waitForShutdown(ctx, cancel, server, streamManager, iptvService, iptvProxy, iptvTransmux, iptvScheduler, iptvProberWorker, scanScheduler, libraryService, authService, database, logger)
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, server *http.Server, sm *stream.Manager, iptvSvc *iptv.Service, iptvProxy *iptv.StreamProxy, iptvTransmux *iptv.TransmuxManager, iptvSched *iptv.Scheduler, iptvProber *iptv.ProberWorker, scheduler *library.Scheduler, librarySvc *library.Service, authSvc *auth.Service, database interface{ Close() error }, logger *slog.Logger) error {
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

	// Stop background services. Stop the IPTV scheduler before the
	// IPTV service itself so an in-flight refresh has a chance to
	// finish recording its outcome against an open DB handle. The
	// shutdownCtx bounds the wait — if a run is stuck past the
	// supervisor deadline the scheduler cancels its in-flight ctx
	// rather than block the whole shutdown.
	iptvSched.Stop(shutdownCtx)
	logger.Info("iptv scheduler stopped")
	if err := iptvProber.Stop(shutdownCtx); err != nil {
		logger.Warn("iptv prober stop", "error", err)
	}
	logger.Info("iptv prober stopped")
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
	if iptvTransmux != nil {
		iptvTransmux.Shutdown()
	}
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
