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
