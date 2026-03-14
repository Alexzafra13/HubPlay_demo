package main

import (
	"context"
	"flag"
	"fmt"
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
	"hubplay/internal/probe"
	"hubplay/internal/scanner"
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
	clk := clock.New()

	logger.Info("starting HubPlay", "version", version, "addr", cfg.Server.Addr())

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

	// ═══ Phase 4: Core Services ═══
	authService := auth.NewService(repos.Users, repos.Sessions, cfg.Auth, clk, logger)
	userService := user.NewService(repos.Users, logger)

	prober := probe.New()
	scnr := scanner.New(repos.Items, repos.MediaStreams, prober, eventBus, logger)
	libraryService := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, scnr, logger)

	// ═══ Phase 4b: Streaming ═══
	streamManager := stream.NewManager(repos.Items, repos.MediaStreams, cfg.Streaming, logger)

	// Detect hardware acceleration if enabled
	if cfg.Streaming.HWAccel.Enabled {
		hwResult := stream.DetectHWAccel(cfg.Streaming.HWAccel.Preferred, logger)
		logger.Info("hardware acceleration",
			"available", hwResult.Available,
			"selected", hwResult.Selected,
			"encoder", hwResult.Encoder,
		)
	}

	// ═══ Phase 5: HTTP Server ═══
	router := api.NewRouter(api.Dependencies{
		Auth:          authService,
		Users:         userService,
		Libraries:     libraryService,
		StreamManager: streamManager,
		Items:         repos.Items,
		MediaStreams:   repos.MediaStreams,
		Config:        cfg,
		Logger:        logger,
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
	return waitForShutdown(ctx, cancel, server, streamManager, database, logger)
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, server *http.Server, sm *stream.Manager, database interface{ Close() error }, logger *slog.Logger) error {
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

	// Stop HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Stop all streaming sessions
	sm.Shutdown()
	logger.Info("stream manager stopped")

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
