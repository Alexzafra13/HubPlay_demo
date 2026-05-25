package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// ExtractSubtitleVTT extrae un track de subtitulos y lo convierte a WebVTT.
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

// ConvertSubtitleToVTT convierte bytes de subtitulos (SRT, ASS, etc.)
// a WebVTT via ffmpeg. Usado para subtitulos externos donde no hay
// fichero en disco del que extraer.
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
