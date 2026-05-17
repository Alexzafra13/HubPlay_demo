package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Preflight: checks runtime que Validate (schema) no cubre — binarios en
// PATH, permisos de FS, etc.
//
// Se llama tras Load y antes de construir services, así un problema de
// config sale como error de boot claro y no como 500 opaco en el primer
// request.
//
// Cada check corre aunque otros fallen — el operador no debería arreglar
// problemas boot a boot. errors.Join junta todos los mensajes.
func (c *Config) Preflight(logger *slog.Logger) error {
	var errs []error

	// Binarios externos. Streaming es la feature core; sin ffmpeg es fatal,
	// no warning.
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			errs = append(errs, fmt.Errorf("%s not found in PATH (required for streaming)", bin))
			continue
		}
		if logger != nil {
			logger.Debug("preflight: binary found", "name", bin)
		}
	}

	// Dir de DB: Validate() ya chequea existencia. Aquí va el check real de
	// write — lo que importa en Docker con bind-mount RO o volumen del uid
	// equivocado.
	if c.Database.Driver == "sqlite" && c.Database.Path != "" {
		dir := filepath.Dir(c.Database.Path)
		if err := checkWritableDir(dir); err != nil {
			errs = append(errs, fmt.Errorf("database.path %q: %w", c.Database.Path, err))
		}
	}

	// Dir de cache de transcode. Si el operador lo override-ó, debe estar
	// ya usable; si no, fallback al default bajo $HOME y lo creamos ya para
	// que el primer transcode no falle en MkdirAll.
	cacheDir := c.Streaming.EffectiveCacheDir()
	if cacheDir == "" {
		errs = append(errs, errors.New("streaming.cache_dir: cannot resolve default (no home directory)"))
	} else if err := checkWritableDir(cacheDir); err != nil {
		errs = append(errs, fmt.Errorf("streaming.cache_dir %q: %w", cacheDir, err))
	}

	return errors.Join(errs...)
}

// EffectiveCacheDir: CacheDir si está, si no el default bajo $HOME.
// Compartido entre preflight y stream.Manager para que coincidan.
// "" significa que no se pudo resolver el default (sin home) — fatal.
func (s StreamingConfig) EffectiveCacheDir() string {
	if s.CacheDir != "" {
		return s.CacheDir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".hubplay", "cache", "transcode")
}

// checkWritableDir: asegura que existe (lo crea si falta) y que podemos
// escribir. Un write probe real es la única señal portable — los mode bits
// no cubren ACLs, flags immutable, bind mounts, sesiones SMB stale, etc.
func checkWritableDir(dir string) error {
	if dir == "" {
		return errors.New("empty path")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".hubplay-preflight-*")
	if err != nil {
		return fmt.Errorf("directory not writable: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}
