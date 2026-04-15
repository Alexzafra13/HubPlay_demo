package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Preflight runs the runtime checks that Validate (schema) cannot cover:
// external binaries present on PATH, filesystem permissions, etc.
//
// Called at startup right after Load and before any service is
// constructed, so configuration problems surface as a clear boot error
// instead of as an opaque 500 during the first user request.
//
// Every check runs regardless of previous failures — operators shouldn't
// have to fix problems one boot at a time. Errors are combined with
// errors.Join so the message prints all of them at once.
func (c *Config) Preflight(logger *slog.Logger) error {
	var errs []error

	// External binaries. Streaming is the core feature; missing ffmpeg is
	// not a warning, it's a fatal.
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			errs = append(errs, fmt.Errorf("%s not found in PATH (required for streaming)", bin))
			continue
		}
		if logger != nil {
			logger.Debug("preflight: binary found", "name", bin)
		}
	}

	// Database directory: Validate() already checks existence. Preflight
	// adds the actual write-permission check, which is what matters in
	// practice on Docker with a read-only bind mount or a volume owned by
	// the wrong uid.
	if c.Database.Driver == "sqlite" && c.Database.Path != "" {
		dir := filepath.Dir(c.Database.Path)
		if err := checkWritableDir(dir); err != nil {
			errs = append(errs, fmt.Errorf("database.path %q: %w", c.Database.Path, err))
		}
	}

	// Transcode cache directory. If the operator overrode it, require the
	// full path to already be usable; otherwise we fall back to the
	// default under the user's home and create it proactively so the
	// first transcode doesn't block on MkdirAll failures later.
	cacheDir := c.Streaming.EffectiveCacheDir()
	if cacheDir == "" {
		errs = append(errs, errors.New("streaming.cache_dir: cannot resolve default (no home directory)"))
	} else if err := checkWritableDir(cacheDir); err != nil {
		errs = append(errs, fmt.Errorf("streaming.cache_dir %q: %w", cacheDir, err))
	}

	return errors.Join(errs...)
}

// EffectiveCacheDir returns CacheDir if set, otherwise the default
// location under the current user's home. Shared between preflight and
// stream.Manager so both agree on where transcodes land. An empty return
// means the default could not be resolved (no home dir); callers should
// treat that as a hard configuration error.
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

// checkWritableDir ensures dir exists (creating it if needed) and that we
// can actually write in it. A real write probe is the only portable
// signal — mode bits don't account for ACLs, immutable flags, bind
// mounts, stale SMB sessions, etc.
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
