package stream

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// HWAccelType representa un método de aceleración por hardware.
type HWAccelType string

const (
	HWAccelNone         HWAccelType = "none"
	HWAccelVAAPI        HWAccelType = "vaapi"
	HWAccelQSV          HWAccelType = "qsv"
	HWAccelNVENC        HWAccelType = "nvenc"
	HWAccelVideoToolbox HWAccelType = "videotoolbox"
)

// DefaultVAAPIDevice es el render node DRM por defecto en Linux con
// una sola GPU. Configurable vía `hardware_acceleration.device` para
// hosts multi-GPU.
const DefaultVAAPIDevice = "/dev/dri/renderD128"

// HWAccelResult contiene las capacidades de aceleración detectadas.
type HWAccelResult struct {
	Available []HWAccelType
	Selected  HWAccelType
	Encoder   string // ej. "h264_vaapi", "h264_nvenc"
	// Device es el render node usado por VAAPI/QSV (vacío para el resto).
	Device string
	// FallbackReason explica por qué se cayó a software cuando un
	// acelerador estaba disponible pero su verificación falló. Vacío si
	// no hubo fallback. El panel admin lo muestra para que "compré el
	// target hwaccel y transcodea por CPU" deje de ser silencioso. PB-5.
	FallbackReason string
}

// DetectHWAccel sondea el sistema buscando aceleradores disponibles.
// `device` es el render node para VAAPI/QSV; vacío = DefaultVAAPIDevice.
func DetectHWAccel(preferred, device string, logger *slog.Logger) HWAccelResult {
	result := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}
	if device == "" {
		device = DefaultVAAPIDevice
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-hwaccels").Output()
	if err != nil {
		logger.Warn("failed to detect hwaccels", "error", err)
		result.FallbackReason = "ffmpeg -hwaccels failed: " + err.Error()
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

	// Verificar que el encoder realmente funciona, ejercitando el MISMO
	// pipeline que BuildFFmpegArgs va a emitir (device + hwupload para
	// VAAPI). Antes el test era un encode pelado sin device: h264_vaapi
	// fallaba SIEMPRE y todo host VAAPI caía a libx264 en silencio. PB-5.
	encoder := accelToEncoder(selected)
	if reason := verifyEncoder(encoder, device, logger); reason == "" {
		result.Selected = selected
		result.Encoder = encoder
		if selected == HWAccelVAAPI || selected == HWAccelQSV {
			result.Device = device
		}
		logger.Info("hardware acceleration enabled", "type", selected, "encoder", encoder, "device", result.Device)
	} else {
		result.FallbackReason = reason
		logger.Warn("hw encoder verification failed, falling back to software",
			"encoder", encoder, "device", device, "reason", reason)
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

	// Auto: prioridad por rendimiento típico
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

// HWAccelInputArgs devuelve los flags ffmpeg de entrada para un tipo de
// aceleración. Van antes de `-i`. `device` solo aplica a VAAPI/QSV
// (vacío = DefaultVAAPIDevice).
//
// VAAPI necesita el device declarado explícitamente: sin
// `-init_hw_device` el encoder h264_vaapi no tiene contexto y muere al
// arrancar (PB-5). No se pone `-hwaccel_output_format vaapi` a
// propósito: los frames decodificados bajan a memoria de sistema para
// que la cadena de filtros software (scale/pad/tonemap/subburn) siga
// funcionando; `format=nv12,hwupload` al final de la cadena los sube
// de vuelta para el encoder (ver HWUploadVideoFilter).
//
// QSV: solo init del device para el encoder — el decode queda en
// software. `-hwaccel qsv` requiere forzar decoders *_qsv por codec y
// frames en memoria GPU, incompatible con la cadena de filtros actual.
func HWAccelInputArgs(accel HWAccelType, device string) []string {
	switch accel {
	case HWAccelVAAPI:
		if device == "" {
			device = DefaultVAAPIDevice
		}
		return []string{
			"-init_hw_device", "vaapi=hw:" + device,
			"-hwaccel", "vaapi",
			"-hwaccel_device", "hw",
			"-filter_hw_device", "hw",
		}
	case HWAccelQSV:
		return []string{
			"-init_hw_device", "qsv=hw",
			"-filter_hw_device", "hw",
		}
	case HWAccelNVENC:
		// Con `-hwaccel cuda` ffmpeg puede usar NVDEC para lectura,
		// reduciendo uso de CPU a la mitad durante transcode.
		return []string{"-hwaccel", "cuda"}
	case HWAccelVideoToolbox:
		// VT solo provee el encoder; no necesita flag de entrada.
		return nil
	default:
		return nil
	}
}

// HWUploadVideoFilter devuelve el sufijo de video filter que sube los
// frames a memoria GPU para encoders que SOLO aceptan hw frames.
// h264_vaapi rechaza frames de sistema; el resto (nvenc, qsv, vt,
// libx264) acepta nv12/yuv420p en memoria de sistema y negocia el
// formato vía el filtergraph. Exportado porque el transmux de IPTV
// construye sus propios args con el mismo encoder.
func HWUploadVideoFilter(encoder string) string {
	if encoder == "h264_vaapi" {
		return "format=nv12,hwupload"
	}
	return ""
}

// verifyEncoder arranca un encode real de 0.1s con los mismos flags de
// device/upload que producirá BuildFFmpegArgs. Devuelve "" si funciona
// o una razón diagnóstica (con el tail de stderr) si no.
func verifyEncoder(encoder, device string, logger *slog.Logger) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error"}
	switch encoder {
	case "h264_vaapi":
		args = append(args, "-init_hw_device", "vaapi=hw:"+device, "-filter_hw_device", "hw")
	case "h264_qsv":
		args = append(args, "-init_hw_device", "qsv=hw", "-filter_hw_device", "hw")
	}
	args = append(args, "-f", "lavfi", "-i", "nullsrc=s=64x64:d=0.1")
	if vf := HWUploadVideoFilter(encoder); vf != "" {
		args = append(args, "-vf", vf)
	}
	args = append(args, "-c:v", encoder, "-f", "null", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 512 {
			detail = detail[len(detail)-512:]
		}
		logger.Debug("encoder test failed", "encoder", encoder, "error", err, "stderr", detail)
		reason := encoder + " verification failed: " + err.Error()
		if detail != "" {
			reason += ": " + detail
		}
		return reason
	}
	return ""
}
