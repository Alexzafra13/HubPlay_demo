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

type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type BrowseResult struct {
	Current     string     `json:"current"`
	Parent      string     `json:"parent"`
	Directories []DirEntry `json:"directories"`
}

type SystemCapabilities struct {
	FFmpegPath string   `json:"ffmpeg_path"`
	FFmpegFound bool    `json:"ffmpeg_found"`
	HWAccels    []string `json:"hw_accels"`
}

type Service struct {
	config     *config.Config
	configPath string
	logger     *slog.Logger
}

func NewService(cfg *config.Config, configPath string, logger *slog.Logger) *Service {
	return &Service{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

func (s *Service) NeedsSetup(ctx context.Context) bool {
	return !s.config.SetupCompleted
}

// BrowseDirectories: lista directorios. Oculta `.` y bloquea paths sensibles
// del sistema para evitar information disclosure.
func (s *Service) BrowseDirectories(path string) (*BrowseResult, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

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
		// raíz, sin parent
		parent = ""
	}

	return &BrowseResult{
		Current:     absPath,
		Parent:      parent,
		Directories: dirs,
	}, nil
}

// DetectCapabilities: presencia de FFmpeg + lista de hwaccels.
func (s *Service) DetectCapabilities() *SystemCapabilities {
	caps := &SystemCapabilities{}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.logger.Debug("ffmpeg not found in PATH")
		return caps
	}

	caps.FFmpegPath = ffmpegPath
	caps.FFmpegFound = true

	// hwaccels: leemos sólo stdout (stderr trae el version banner).
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
			// la primera línea no vacía es el header "Hardware acceleration methods:"
			pastHeader = true
			continue
		}
		caps.HWAccels = append(caps.HWAccels, line)
	}

	return caps
}

// sensitivePaths: directorios de sistema que no se pueden navegar.
var sensitivePaths = []string{
	"/etc", "/proc", "/sys", "/dev", "/boot", "/root",
	"/var/run", "/var/log", "/run", "/sbin", "/usr/sbin",
}

func isSensitivePath(absPath string) bool {
	cleaned := filepath.Clean(absPath)
	for _, sp := range sensitivePaths {
		if cleaned == sp || strings.HasPrefix(cleaned, sp+"/") {
			return true
		}
	}
	return false
}

// CompleteSetup: marca setup hecho y persiste el YAML vía config.Save
// (write atómico, 0600). Nunca world-readable: contiene secretos (JWT signing
// seed, API keys de TMDb/Fanart/OpenSubtitles, DSN).
func (s *Service) CompleteSetup(startScan bool) error {
	s.config.SetupCompleted = true

	if err := config.Save(s.config, s.configPath); err != nil {
		return fmt.Errorf("persisting config: %w", err)
	}

	s.logger.Info("setup completed, config persisted", "start_scan", startScan)
	return nil
}

// SaveDatabaseConfig: persiste driver + DSN/path al YAML para el próximo boot.
// Lo usan el wizard step 0 y el panel admin Database tras pasar Open + Ping.
// NO valida la conexión (eso es del caller) — esto es ruta de persistencia pura.
// Tampoco aplica al proceso vivo: el operador llama Restart después.
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
