package stream

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// HWAccelType represents a hardware acceleration method.
type HWAccelType string

const (
	HWAccelNone         HWAccelType = "none"
	HWAccelVAAPI        HWAccelType = "vaapi"
	HWAccelQSV          HWAccelType = "qsv"
	HWAccelNVENC        HWAccelType = "nvenc"
	HWAccelVideoToolbox HWAccelType = "videotoolbox"
)

// HWAccelResult contains detected hardware acceleration capabilities.
type HWAccelResult struct {
	Available []HWAccelType
	Selected  HWAccelType
	Encoder   string // e.g. "h264_vaapi", "h264_nvenc"
}

// DetectHWAccel probes the system for available hardware accelerators.
func DetectHWAccel(preferred string, logger *slog.Logger) HWAccelResult {
	result := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query ffmpeg for available hwaccels
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-hwaccels").Output()
	if err != nil {
		logger.Warn("failed to detect hwaccels", "error", err)
		return result
	}

	available := parseHWAccels(string(out))
	result.Available = available

	if len(available) == 0 {
		return result
	}

	// Select based on preference
	selected := selectAccel(available, preferred)
	if selected == HWAccelNone {
		return result
	}

	// Verify the encoder actually works
	encoder := accelToEncoder(selected)
	if verifyEncoder(encoder, logger) {
		result.Selected = selected
		result.Encoder = encoder
		logger.Info("hardware acceleration enabled", "type", selected, "encoder", encoder)
	} else {
		logger.Warn("hw encoder verification failed, falling back to software", "encoder", encoder)
	}

	return result
}

func parseHWAccels(output string) []HWAccelType {
	var accels []HWAccelType
	lines := strings.Split(output, "\n")
	started := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "Hardware acceleration methods:" {
			started = true
			continue
		}
		if !started || line == "" {
			continue
		}

		switch line {
		case "vaapi":
			accels = append(accels, HWAccelVAAPI)
		case "qsv":
			accels = append(accels, HWAccelQSV)
		case "cuda":
			accels = append(accels, HWAccelNVENC)
		case "videotoolbox":
			accels = append(accels, HWAccelVideoToolbox)
		}
	}

	return accels
}

func selectAccel(available []HWAccelType, preferred string) HWAccelType {
	if preferred != "" && preferred != "auto" {
		want := HWAccelType(preferred)
		for _, a := range available {
			if a == want {
				return a
			}
		}
	}

	// Auto: prefer in order of typical performance
	priority := []HWAccelType{HWAccelNVENC, HWAccelQSV, HWAccelVAAPI, HWAccelVideoToolbox}
	for _, p := range priority {
		for _, a := range available {
			if a == p {
				return a
			}
		}
	}

	return HWAccelNone
}

func accelToEncoder(accel HWAccelType) string {
	switch accel {
	case HWAccelVAAPI:
		return "h264_vaapi"
	case HWAccelQSV:
		return "h264_qsv"
	case HWAccelNVENC:
		return "h264_nvenc"
	case HWAccelVideoToolbox:
		return "h264_videotoolbox"
	default:
		return "libx264"
	}
}

// HWAccelInputArgs returns the ffmpeg input-side flags for a given
// acceleration kind. These go before `-i` and tell ffmpeg to use
// the GPU/iGPU decoder. Empty slice means software decode (no flags).
//
// We intentionally don't set `-hwaccel_output_format` so frames stay
// downloadable to system memory — that lets the existing software
// filter chain (`scale=...,pad=...`) keep working unchanged. A fully
// on-device pipeline would need scale_vaapi / scale_cuda / scale_qsv
// and a per-format `-vf hwupload`, which is the next iteration.
//
// Exported because the iptv transmux re-encode fallback (used when
// `-c copy` can't repackage the upstream codec) needs the same
// decode-side flags as the VOD transcoder. Owning the canonical
// mapping in one place keeps NVDEC / VAAPI quirks from drifting.
func HWAccelInputArgs(accel HWAccelType) []string {
	switch accel {
	case HWAccelVAAPI:
		return []string{"-hwaccel", "vaapi"}
	case HWAccelQSV:
		return []string{"-hwaccel", "qsv"}
	case HWAccelNVENC:
		// NVENC's encoder works without `-hwaccel cuda`, but adding
		// it lets ffmpeg pick the NVDEC decoder for the read side
		// when the input codec is supported (h264, hevc, …),
		// halving CPU usage during transcode.
		return []string{"-hwaccel", "cuda"}
	case HWAccelVideoToolbox:
		// VT only provides the encoder; no input flag needed.
		return nil
	default:
		return nil
	}
}

func verifyEncoder(encoder string, logger *slog.Logger) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to encode a tiny test frame
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=0.1",
		"-c:v", encoder,
		"-f", "null", "-",
	)

	if err := cmd.Run(); err != nil {
		logger.Debug("encoder test failed", "encoder", encoder, "error", err)
		return false
	}
	return true
}
