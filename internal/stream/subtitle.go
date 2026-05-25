package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
