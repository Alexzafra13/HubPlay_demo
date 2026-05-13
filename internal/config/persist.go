package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Save persists cfg to path atomically (write-temp + rename) with mode
// 0600. The atomic rename means a crash mid-write can never leave a
// half-written YAML the next boot would fail to parse — either the old
// file survives or the new one is fully in place.
//
// Mode 0600 matches the convention every other secret-bearing config
// file in self-hosted media servers uses (Plex Preferences.xml,
// Jellyfin server.json) and lines up with what setup.CompleteSetup
// used before it was extracted here.
//
// Callers must validate cfg before calling — Save is the persistence
// layer, not the validation layer. The admin DB editor and the setup
// wizard both validate first, then call Save, then ask the operator
// to restart so the new values take effect.
func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hubplay.yaml.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Clean up the temp on every error path; the rename below removes
	// it from the FS on success so the deferred Remove turns into a
	// no-op (os.Remove on a missing path returns ErrNotExist, which
	// we ignore).
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
