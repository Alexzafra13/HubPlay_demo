package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ExtractSubtitleVTT extrae una pista de subtítulos y la convierte a WebVTT.
func ExtractSubtitleVTT(ctx context.Context, inputPath string, trackIndex int) (io.Reader, error) {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", inputPath,
		"-map", fmt.Sprintf("0:%d", trackIndex),
		"-c:s", "webvtt",
		"-f", "webvtt",
		"pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("extract subtitle track %d: %s: %w", trackIndex, stderr.String(), err)
	}

	return &stdout, nil
}

// ExtractSubtitlesVTT extrae varias pistas de subtítulos de texto en UNA
// sola pasada de ffmpeg y las escribe como WebVTT en las rutas de
// `outputs` (índice absoluto del stream → fichero destino). Leer un MKV
// grande cuesta decenas de segundos en frío; con una pasada por fichero
// el resto de pistas salen gratis. Escribe en `.part` y renombra al
// acabar para que nadie sirva un VTT a medias; si ffmpeg falla no deja
// nada. Las pistas de imagen (PGS/DVD) no convierten a WebVTT: el caller
// debe filtrarlas con IsImageSubtitleCodec.
func ExtractSubtitlesVTT(ctx context.Context, inputPath string, outputs map[int]string) error {
	if len(outputs) == 0 {
		return nil
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", inputPath}
	parts := make(map[string]string, len(outputs))
	for idx, out := range outputs {
		part := out + ".part"
		parts[part] = out
		args = append(args, "-map", fmt.Sprintf("0:%d", idx), "-c:s", "webvtt", "-f", "webvtt", part)
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		for part := range parts {
			_ = os.Remove(part)
		}
		return fmt.Errorf("extract subtitle tracks: %s: %w", stderr.String(), err)
	}
	for part, out := range parts {
		if err := os.Rename(part, out); err != nil {
			return fmt.Errorf("extract subtitle tracks: %w", err)
		}
	}
	return nil
}

// ConvertSubtitleToVTT pasa bytes de subtítulos (SRT, ASS, etc.) por
// ffmpeg para producir WebVTT. ffmpeg auto-detecta el container de
// entrada. Usado para el endpoint de subtítulos externos.
func ConvertSubtitleToVTT(ctx context.Context, data []byte) ([]byte, error) {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-c:s", "webvtt",
		"-f", "webvtt",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("convert subtitle to vtt: %s: %w", stderr.String(), err)
	}
	return stdout.Bytes(), nil
}
