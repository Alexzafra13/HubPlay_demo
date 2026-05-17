package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Save: persiste cfg en path atómico (write-temp + rename) con modo 0600.
// El rename atómico evita que un crash a mitad de write deje un YAML medio
// escrito que el próximo boot no parsearía.
//
// Modo 0600 = convención de Plex/Jellyfin para configs con secretos.
//
// Callers deben validar antes — Save es persistencia, no validación.
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
	// Limpieza del temp en cualquier rama de error. En éxito el rename ya
	// lo movió, y os.Remove de un path inexistente devuelve ErrNotExist
	// (lo ignoramos).
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
