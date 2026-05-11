package setup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"hubplay/internal/config"
)

// TestCompleteSetup_WritesYAMLWith0600 pins the file mode so a future
// "just match the surrounding 0644 pattern" refactor doesn't quietly
// downgrade secret-bearing files. JWT signing seed + provider API
// keys + the DB path all live in this file; world-readable on a
// multi-user host is a real leak vector.
func TestCompleteSetup_WritesYAMLWith0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix mode bits don't translate to Windows ACLs")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "hubplay.yaml")

	cfg := &config.Config{}
	svc := NewService(cfg, cfgPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.CompleteSetup(false); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	got := info.Mode().Perm()
	if got != 0o600 {
		t.Errorf("config file perms = %o, want 0600", got)
	}
}
