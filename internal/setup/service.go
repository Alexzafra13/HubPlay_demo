package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hubplay/internal/config"
)

// DirEntry represents a single directory in a browse result.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BrowseResult is returned by BrowseDirectories.
type BrowseResult struct {
	Current     string     `json:"current"`
	Parent      string     `json:"parent"`
	Directories []DirEntry `json:"directories"`
}

// SystemCapabilities describes what the host system supports.
type SystemCapabilities struct {
	FFmpegPath string   `json:"ffmpeg_path"`
	FFmpegFound bool    `json:"ffmpeg_found"`
	HWAccels    []string `json:"hw_accels"`
}

// Service handles setup wizard logic.
type Service struct {
	config     *config.Config
	configPath string
	logger     *slog.Logger
}

// NewService creates a new setup service.
func NewService(cfg *config.Config, configPath string, logger *slog.Logger) *Service {
	return &Service{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

// NeedsSetup returns true if the initial setup has not been completed.
func (s *Service) NeedsSetup(ctx context.Context) bool {
	return !s.config.SetupCompleted
}

// BrowseDirectories lists directories at the given path.
// Hidden directories (starting with .) are filtered out.
// Sensitive system paths are blocked to prevent information disclosure.
func (s *Service) BrowseDirectories(path string) (*BrowseResult, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	// Block access to sensitive system directories
	if isSensitivePath(absPath) {
		return nil, fmt.Errorf("access denied: cannot browse system directory %q", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	dirs := make([]DirEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dirs = append(dirs, DirEntry{
			Name: entry.Name(),
			Path: filepath.Join(absPath, entry.Name()),
		})
	}

	parent := filepath.Dir(absPath)
	if parent == absPath {
		// At root, no parent
		parent = ""
	}

	return &BrowseResult{
		Current:     absPath,
		Parent:      parent,
		Directories: dirs,
	}, nil
}

// DetectCapabilities checks for FFmpeg and available hardware accelerators.
func (s *Service) DetectCapabilities() *SystemCapabilities {
	caps := &SystemCapabilities{}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.logger.Debug("ffmpeg not found in PATH")
		return caps
	}

	caps.FFmpegPath = ffmpegPath
	caps.FFmpegFound = true

	// Detect hardware accelerators (stdout only — stderr has the version banner)
	out, err := exec.Command("ffmpeg", "-hwaccels").Output()
	if err != nil {
		s.logger.Warn("failed to query ffmpeg hardware accelerators", "error", err)
		return caps
	}

	lines := strings.Split(string(out), "\n")
	pastHeader := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !pastHeader {
			// The first non-empty line is the header "Hardware acceleration methods:"
			pastHeader = true
			continue
		}
		caps.HWAccels = append(caps.HWAccels, line)
	}

	return caps
}

// sensitivePaths are system directories that should not be browsable.
var sensitivePaths = []string{
	"/etc", "/proc", "/sys", "/dev", "/boot", "/root",
	"/var/run", "/var/log", "/run", "/sbin", "/usr/sbin",
}

// isSensitivePath returns true if the path is inside a sensitive system directory.
func isSensitivePath(absPath string) bool {
	cleaned := filepath.Clean(absPath)
	for _, sp := range sensitivePaths {
		if cleaned == sp || strings.HasPrefix(cleaned, sp+"/") {
			return true
		}
	}
	return false
}

// CompleteSetup marks the setup as done and persists the config to
// disk via the shared config.Save helper (atomic write, 0600 perms).
// The YAML carries secrets the runtime treats as sensitive — JWT
// signing seed, provider API keys (TMDb, Fanart, OpenSubtitles), and
// the DB path or DSN — so the file is never world-readable.
func (s *Service) CompleteSetup(startScan bool) error {
	s.config.SetupCompleted = true

	if err := config.Save(s.config, s.configPath); err != nil {
		return fmt.Errorf("persisting config: %w", err)
	}

	s.logger.Info("setup completed, config persisted", "start_scan", startScan)
	return nil
}

// SaveDatabaseConfig persists a new database driver + DSN/path to the
// YAML so the next boot picks it up. Used by the wizard step 0 and the
// admin Database panel after the candidate connection passes the Open
// + Ping test. Does not validate the connection itself — callers must
// have tested it first; this is the persistence-only path.
//
// The change is not applied to the running process. The operator is
// expected to call Restart afterwards so the new driver takes effect.
func (s *Service) SaveDatabaseConfig(driver, path, dsn string) error {
	s.config.Database.Driver = driver
	s.config.Database.Path = path
	s.config.Database.DSN = dsn

	if err := config.Save(s.config, s.configPath); err != nil {
		return fmt.Errorf("persisting database config: %w", err)
	}

	s.logger.Info("database config persisted",
		"driver", driver,
		"path", path,
		"dsn_set", dsn != "")
	return nil
}
