package main

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/event"
	"hubplay/internal/federation"
	federationstorage "hubplay/internal/federation/storage"
	"hubplay/internal/notification"
	"hubplay/internal/observability"
)

// federationResult agrupa los outputs de initFederation para que
// run() los cablee sin desempaquetar 5 variables sueltas.
type federationResult struct {
	Manager *federation.Manager
	Repo    *federationstorage.Repository
}

// initFederation carga o crea la identidad Ed25519 del servidor e
// inicializa el manager. Fail-soft: si falla, retorna Manager nil
// y el router salta las rutas federation. Registra los lifecycle
// hooks pertinentes en lc.
func initFederation(
	ctx context.Context,
	lc *lifecycle,
	cfg federationInitConfig,
	database *sql.DB,
	clk clock.Clock,
	eventBus *event.Bus,
	metrics *observability.Metrics,
	notifService *notification.Service,
	logger *slog.Logger,
) federationResult {
	federationRepo := federationstorage.NewRepository(cfg.DBDriver, database)
	federationCfg := federation.DefaultConfig()
	federationCfg.AdvertisedURL = cfg.BaseURL
	federationCfg.Version = version
	federationCfg.AvatarsDir = cfg.AvatarsDir

	if _, err := federation.LoadOrCreate(ctx, federationRepo, clk, "HubPlay Server"); err != nil {
		logger.Error("federation: identity load/create failed; federation disabled", "err", err)
	}

	mgr, err := federation.NewManager(ctx, federationCfg, federationRepo, clk, logger, eventBus)
	if err != nil {
		logger.Error("federation: manager init failed; federation disabled", "err", err)
		return federationResult{Repo: federationRepo}
	}

	if cfg.Settings != nil {
		mgr.SetSettings(cfg.Settings)
	}
	logger.Info("federation: manager initialised",
		"server_uuid", mgr.PublicServerInfo().ServerUUID,
		"fingerprint", mgr.PublicServerInfo().PubkeyFingerprint)

	mgr.SetMetricsSink(observability.NewFederationSink(metrics))
	if err := observability.RegisterFederationGauges(metrics, mgr); err != nil {
		logger.Error("federation: register gauges failed", "err", err)
	}

	lc.AddWorker("federation close", func(context.Context) error {
		mgr.Close()
		return nil
	})

	registerFederationNotifications(ctx, eventBus, notifService, logger)

	stopPendingSweeper := federation.StartPendingRequestSweeper(ctx, mgr, logger, cfg.SweeperInterval)
	lc.AddWorker("pending request sweeper", func(context.Context) error {
		stopPendingSweeper()
		return nil
	})

	return federationResult{Manager: mgr, Repo: federationRepo}
}

// federationInitConfig agrupa los parámetros de configuración para
// initFederation sin arrastrar *config.Config entero.
type federationInitConfig struct {
	DBDriver        string
	BaseURL         string
	AvatarsDir      string
	Settings        federation.SettingsReader
	SweeperInterval time.Duration
}
