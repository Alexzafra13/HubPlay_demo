package stream

import "hubplay/internal/config"

// AutoTuneStreaming rellena valores cero/vacíos en cfg con recomendaciones
// basadas en hardware. Valores ya configurados (vía YAML u override)
// pasan intactos — solo toca los sentinel zeros.
func AutoTuneStreaming(cfg config.StreamingConfig, hwAccel HWAccelType, cpuCount int) config.StreamingConfig {
	if cfg.MaxTranscodeSessions <= 0 {
		cfg.MaxTranscodeSessions = RecommendMaxSessions(hwAccel, cpuCount)
	}
	if cfg.MaxTranscodeSessionsPerUser <= 0 {
		cfg.MaxTranscodeSessionsPerUser = RecommendPerUserCap(cfg.MaxTranscodeSessions)
	}
	if cfg.TranscodePreset == "" {
		cfg.TranscodePreset = RecommendPreset(hwAccel, cpuCount)
	}
	return cfg
}

// RecommendMaxSessions devuelve el cap global auto-tuneado de sesiones
// concurrentes según el hardware detectado. Conservador por defecto —
// el admin puede subir desde el panel tras medir carga real.
func RecommendMaxSessions(hw HWAccelType, cpuCount int) int {
	switch hw {
	case HWAccelNVENC:
		return 3
	case HWAccelQSV, HWAccelVAAPI:
		return 6
	case HWAccelVideoToolbox:
		return 4
	default:
		n := cpuCount / 2
		if n < 1 {
			n = 1
		}
		return n
	}
}

// RecommendPerUserCap devuelve el cap por usuario dado un cap global.
// Mitad del pool (redondeado arriba), mínimo 1.
func RecommendPerUserCap(globalCap int) int {
	n := (globalCap + 1) / 2
	if n < 1 {
		n = 1
	}
	return n
}

// RecommendPreset devuelve el preset libx264 apropiado para el host.
// HW encoders ignoran -preset pero emitimos un valor coherente para
// la UI de admin y el round-trip por la API de settings.
func RecommendPreset(hw HWAccelType, cpuCount int) string {
	if hw != HWAccelNone {
		return "veryfast"
	}
	switch {
	case cpuCount >= 12:
		return "fast"
	case cpuCount >= 6:
		return "veryfast"
	case cpuCount >= 4:
		return "superfast"
	default:
		return "ultrafast"
	}
}

// ValidLibx264Preset reporta si s es un valor válido de libx264 -preset.
func ValidLibx264Preset(s string) bool {
	switch s {
	case "ultrafast", "superfast", "veryfast", "faster", "fast",
		"medium", "slow", "slower", "veryslow":
		return true
	default:
		return false
	}
}
