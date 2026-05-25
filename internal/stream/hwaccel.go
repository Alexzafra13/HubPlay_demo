package stream

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// HWAccelType representa un metodo de aceleracion por hardware.
type HWAccelType string

const (
	HWAccelNone         HWAccelType = "none"
	HWAccelVAAPI        HWAccelType = "vaapi"
	HWAccelQSV          HWAccelType = "qsv"
	HWAccelNVENC        HWAccelType = "nvenc"
	HWAccelVideoToolbox HWAccelType = "videotoolbox"
)

// HWAccelResult contiene las capacidades de aceleracion detectadas.
type HWAccelResult struct {
	Available []HWAccelType
	Selected  HWAccelType
	Encoder   string // ej. "h264_vaapi", "h264_nvenc"
}

// DetectHWAccel sondea el sistema buscando aceleradores disponibles.
func DetectHWAccel(preferred string, logger *slog.Logger) HWAccelResult {
	result := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

	selected := selectAccel(available, preferred)
	if selected == HWAccelNone {
		return result
	}

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

	// Auto: prioridad por rendimiento tipico.
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

// HWAccelInputArgs devuelve los flags ffmpeg de decode-side para un
// acelerador dado. Van antes de -i. Slice vacio = software decode.
//
// No seteamos -hwaccel_output_format para que los frames queden en
// memoria de sistema y el filter chain software (scale, pad) funcione
// sin cambios. Pipeline full-device seria la siguiente iteracion.
//
// Exportado porque el fallback de re-encode de iptv transmux usa los
// mismos flags de decode.
func HWAccelInputArgs(accel HWAccelType) []string {
	switch accel {
	case HWAccelVAAPI:
		return []string{"-hwaccel", "vaapi"}
	case HWAccelQSV:
		return []string{"-hwaccel", "qsv"}
	case HWAccelNVENC:
		// -hwaccel cuda activa NVDEC para el decode, reduciendo uso de CPU.
		return []string{"-hwaccel", "cuda"}
	case HWAccelVideoToolbox:
		return nil
	default:
		return nil
	}
}

func verifyEncoder(encoder string, logger *slog.Logger) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
