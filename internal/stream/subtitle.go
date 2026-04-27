package stream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// ExtractSubtitleVTT extracts a subtitle track from a media file and converts it to WebVTT.
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

// ConvertSubtitleToVTT pipes arbitrary subtitle bytes (typically SRT
// or ASS as served by OpenSubtitles) through ffmpeg to produce WebVTT.
// ffmpeg auto-detects the input container, so the caller doesn't
// have to declare the source format. Used for the external subtitles
// endpoint where we don't have a file on disk to extract from.
//
// Errors include the ffmpeg stderr, which usually pinpoints malformed
// timestamps or unsupported codecs in less than a line.
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
