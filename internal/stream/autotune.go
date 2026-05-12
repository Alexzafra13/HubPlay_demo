package stream

import "hubplay/internal/config"

// AutoTuneStreaming fills zero / empty values in cfg with
// hardware-aware recommendations so a fresh install is sensible
// out of the box. Values already set explicitly (via hubplay.yaml
// or an app_settings override) pass through unchanged — auto-tune
// only touches sentinel zeros.
//
// The contract with the caller is:
//
//   - MaxTranscodeSessions == 0        → auto
//   - MaxTranscodeSessionsPerUser == 0 → auto
//   - TranscodePreset == ""            → auto
//
// Any non-zero value is treated as deliberate operator intent and
// preserved, even if it looks wrong (e.g. 12 sessions on a 2-core
// box). The admin owns that decision; this helper just makes the
// default reasonable.
//
// `hwAccel` is the detected accelerator kind (HWAccelNone for hosts
// without GPU acceleration). `cpuCount` is runtime.NumCPU() at the
// call site — passed in so tests stay deterministic.
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

// RecommendMaxSessions returns the auto-tuned global cap on
// concurrent transcoding sessions for a given hardware mix.
//
// Numbers are intentionally conservative — under-promising and
// over-delivering is the right failure mode for a home server.
// The admin can bump them from the panel once they've measured
// their host under real load.
//
//   - NVENC (NVIDIA): 3 — the documented hardware cap on every
//     consumer GeForce since Pascal. Datacenter GPUs (A2/A10/T4)
//     accept 8+ but we don't auto-detect that; the admin bumps
//     it manually after reading nvidia-smi.
//   - QSV / VAAPI (Intel iGPU, AMD): 6 — modern integrated GPUs
//     handle 6+ 1080p sessions comfortably, capped by memory
//     bandwidth rather than compute. Older silicon (pre-Skylake)
//     will choke before reaching the cap; the operator can lower.
//   - VideoToolbox (Apple Silicon): 4 — documented limit on
//     M1 Mac-Mini-class hardware. M2 Pro / M3 Pro accept more
//     but the auto-tuner doesn't introspect the SoC tier.
//   - Software (libx264): cpuCount / 2, minimum 1. libx264
//     veryfast hits roughly 2-3× realtime per core on 1080p →
//     720p; halving the core count leaves headroom for everything
//     else the server has to do (scanning, image processing,
//     the API itself).
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

// RecommendPerUserCap returns the per-user cap given a global cap.
// Half the pool (rounded up) means one user can't soak the whole
// server while still leaving room for the same user to seek-restart
// on a second device. Minimum 1 — a single-session global cap still
// allows that one user to use it.
func RecommendPerUserCap(globalCap int) int {
	n := (globalCap + 1) / 2
	if n < 1 {
		n = 1
	}
	return n
}

// RecommendPreset returns the libx264 preset name appropriate for a
// host. HW encoders ignore -preset but we still emit a value so the
// admin UI shows a coherent default and the same string round-trips
// through the settings API.
//
// For software (libx264) hosts the preset scales with available
// cores because preset affects per-frame CPU cost:
//
//   - ≥ 12 cores: "fast" — can afford the quality bump at the
//     2-3× cost over veryfast.
//   - 6-11 cores: "veryfast" — the historical project default,
//     stays good across most mid-range desktops.
//   - 4-5 cores:  "superfast" — keeps the system responsive on
//     quad-core NASes.
//   - < 4 cores:  "ultrafast" — low-end N100 / RPi-class hosts
//     where every cycle counts.
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

// ValidLibx264Preset reports whether s is a known libx264 -preset
// value. Used by the settings validator to reject typos before they
// reach ffmpeg (which would just error mid-transcode on the first
// session after the override).
func ValidLibx264Preset(s string) bool {
	switch s {
	case "ultrafast", "superfast", "veryfast", "faster", "fast",
		"medium", "slow", "slower", "veryslow":
		return true
	default:
		return false
	}
}
