package main

import (
	"context"
	"strconv"

	"hubplay/internal/config"
)

// settingsReader es el contrato mínimo para leer app_settings.
type settingsReader interface {
	Get(ctx context.Context, key string) (string, error)
}

// applyStreamingOverrides lee overrides de runtime desde app_settings
// y los aplica sobre la config de streaming del YAML. Cadena de
// autoridad: YAML default → DB override → config efectiva. Extraída
// de run() porque era lógica de negocio (parseo + validación) en
// medio del composition root (sub-olor QQ-a del audit macro).
func applyStreamingOverrides(ctx context.Context, base config.StreamingConfig, settings settingsReader) config.StreamingConfig {
	cfg := base
	if v, err := settings.Get(ctx, "hardware_acceleration.enabled"); err == nil {
		cfg.HWAccel.Enabled = v == "true"
	}
	if v, err := settings.Get(ctx, "hardware_acceleration.preferred"); err == nil && v != "" {
		cfg.HWAccel.Preferred = v
	}
	if v, err := settings.Get(ctx, "streaming.max_transcode_sessions"); err == nil {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			cfg.MaxTranscodeSessions = n
		}
	}
	if v, err := settings.Get(ctx, "streaming.max_transcode_sessions_per_user"); err == nil {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			cfg.MaxTranscodeSessionsPerUser = n
		}
	}
	if v, err := settings.Get(ctx, "streaming.transcode_preset"); err == nil && v != "" {
		cfg.TranscodePreset = v
	}
	return cfg
}
