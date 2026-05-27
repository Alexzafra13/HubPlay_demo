package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/logging"
)

// foundation agrupa los componentes de arranque que el resto de fases
// consume. Extraída de run() para reducir la función de 630 → ~300 LoC
// (cierre parcial de QQ).
type foundation struct {
	Config    *config.Config
	Logger    *slog.Logger
	LogBuffer *logging.Buffer
	Clock     clock.Clock
}

// buildFoundation carga config, crea logger + clock, prependa el
// directorio del ejecutable al PATH (bundled ffmpeg) y valida
// prerequisitos (preflight). Errores aquí son fatales para el boot.
func buildFoundation(configPath string) (*foundation, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	logger, logBuffer := logging.NewWithBuffer(cfg.Logging)
	slog.SetDefault(logger)
	clk := clock.New()

	logger.Info("starting HubPlay", "version", version, "commit", commit, "addr", cfg.Server.Addr())

	prependBundledBinaries()

	if err := cfg.Preflight(logger); err != nil {
		return nil, fmt.Errorf("preflight checks failed:\n%w", err)
	}

	return &foundation{
		Config:    cfg,
		Logger:    logger,
		LogBuffer: logBuffer,
		Clock:     clk,
	}, nil
}

// prependBundledBinaries añade el directorio del ejecutable al inicio
// del PATH para que ffmpeg/ffprobe bundleados tengan prioridad.
func prependBundledBinaries() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exe)
	curPath := os.Getenv("PATH")
	sep := string(os.PathListSeparator)
	if curPath == "" {
		_ = os.Setenv("PATH", exeDir)
	} else if !strings.Contains(sep+curPath+sep, sep+exeDir+sep) {
		_ = os.Setenv("PATH", exeDir+sep+curPath)
	}
}
