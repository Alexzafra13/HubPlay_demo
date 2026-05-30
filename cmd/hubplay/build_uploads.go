package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/probe"
	"hubplay/internal/upload"
)

// buildUploads construye el handler tusd + GC si uploads están activos
// en config. Retorna nil si están desactivados.
func buildUploads(ctx context.Context, cfg config.UploadConfig, repos *db.Repositories, eventBus *event.Bus, prober probe.Prober, clk clock.Clock, logger *slog.Logger) (http.Handler, error) {
	if !cfg.Enabled {
		logger.Info("uploads disabled (config.upload.enabled=false)")
		return nil, nil
	}

	stagingDir, err := upload.NewStagingDir(cfg.StagingDir)
	if err != nil {
		return nil, fmt.Errorf("upload staging dir: %w", err)
	}

	upSvc := upload.NewService(
		upload.Config{
			MaxUploadBytes: cfg.MaxBytesPerUpload,
			MinDurationMs:  cfg.MinDurationMs,
		},
		stagingDir,
		repos.Users,
		uploadAuditStore{repos.UploadAudit},
		eventBus,
		upload.NewLibraryPicker(repos.Libraries),
		prober,
		clk,
		logger,
	)

	tusd, err := upload.NewTusdHandler(upSvc, "/api/v1/uploads/")
	if err != nil {
		return nil, fmt.Errorf("upload tusd handler: %w", err)
	}

	// http.StripPrefix requerido entre chi y tusd — chi.Mount no
	// modifica r.URL.Path, tusd espera "/" para POST de creación.
	handler := http.StripPrefix("/api/v1/uploads", tusd)

	logger.Info("uploads enabled",
		"staging_dir", stagingDir.Root(),
		"max_bytes", cfg.MaxBytesPerUpload)

	upload.NewGC(stagingDir, time.Hour, 24*time.Hour, clk, logger).Start(ctx)

	return handler, nil
}
