package stream

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

// TestNewTranscoder_BaseDirIsAbsolute: con un cache_dir relativo los args de
// ffmpeg (ruta completa de manifest/segmentos) se resolvían relativos a
// cmd.Dir = outputDir y el header nunca se escribía. El transcoder debe
// absolutizar el base dir al construirse.
func TestNewTranscoder_BaseDirIsAbsolute(t *testing.T) {
	tr := NewTranscoder(TranscoderConfig{
		BaseDir: "./relative-cache",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if !filepath.IsAbs(tr.baseDir) {
		t.Fatalf("baseDir should be absolute, got %q", tr.baseDir)
	}
	if filepath.Base(tr.baseDir) != "relative-cache" {
		t.Fatalf("baseDir should keep its last element, got %q", tr.baseDir)
	}
}
