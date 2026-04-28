package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"hubplay/internal/logging"
)

type Config struct {
	Server         ServerConfig        `yaml:"server"`
	Database       DatabaseConfig      `yaml:"database"`
	Auth           AuthConfig          `yaml:"auth"`
	Logging        logging.Config      `yaml:"logging"`
	RateLimit      RateLimitConfig     `yaml:"rate_limit"`
	Streaming      StreamingConfig     `yaml:"streaming"`
	IPTV           IPTVConfig          `yaml:"iptv"`
	Observability  ObservabilityConfig `yaml:"observability"`
	SetupCompleted bool                `yaml:"setup_completed"`
}

// IPTVConfig groups runtime knobs for the IPTV subsystem. Only
// transmux is exposed today; other subsystems (proxy timeouts,
// scheduler intervals) are still hard-coded in their respective
// packages and will move here when they need operator-facing tuning.
type IPTVConfig struct {
	Transmux IPTVTransmuxConfig `yaml:"transmux"`
}

// IPTVTransmuxConfig controls the live MPEG-TS → HLS transmux
// session manager (internal/iptv/transmux.go). Defaults are
// production-sane for a single-host self-hosted deployment.
type IPTVTransmuxConfig struct {
	// Enabled gates the entire transmux subsystem. When false, the
	// channel-stream handler falls back to the raw passthrough proxy
	// and MPEG-TS upstreams break in browsers (HLS upstreams keep
	// working). Default true.
	Enabled bool `yaml:"enabled"`

	// MaxSessions caps simultaneous ffmpeg processes. One session is
	// shared by all viewers of the same channel, so a household of 5
	// watching different channels needs 5 sessions. Default 10.
	MaxSessions int `yaml:"max_sessions"`

	// MaxReencodeSessions caps how many active sessions can run in
	// re-encode mode (the codec-rescue path). Reencode is the only
	// path that costs real CPU / GPU, so capping it separately
	// prevents a codec-crash storm from saturating every encoder
	// slot. Zero = default to MaxSessions/2 (with a floor of 1).
	MaxReencodeSessions int `yaml:"max_reencode_sessions"`

	// IdleTimeout is how long a session stays alive with no segment
	// requests before the reaper kills it. Lower trades faster
	// cleanup for more spawn churn on rapid channel-zap. Default 30s.
	IdleTimeout time.Duration `yaml:"idle_timeout"`

	// ReadyTimeout is how long the manifest handler waits for ffmpeg
	// to produce its first segment before declaring the session
	// failed. Default 15s — bigger than typical first-segment latency
	// (3-5s) for healthy upstreams; bounded so dead providers don't
	// hang the player UI. Default 15s.
	ReadyTimeout time.Duration `yaml:"ready_timeout"`
}

// ObservabilityConfig controls the Prometheus /metrics endpoint. Defaults are
// applied in Load: metrics are enabled out of the box and exposed at /metrics.
// Operators who do not want to expose them can set enabled: false; those who
// want to move the path for reverse-proxy hygiene can override it.
type ObservabilityConfig struct {
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	MetricsPath    string `yaml:"metrics_path"`
}

type StreamingConfig struct {
	SegmentDuration      int           `yaml:"segment_duration"`       // seconds, default 6
	MaxTranscodeSessions int           `yaml:"max_transcode_sessions"` // default 2
	TranscodePreset      string        `yaml:"transcode_preset"`       // veryfast, fast, medium
	DefaultAudioBitrate  string        `yaml:"default_audio_bitrate"`  // e.g. "192k"
	CacheDir             string        `yaml:"cache_dir"`              // directory for transcode output
	IdleTimeout          time.Duration `yaml:"idle_timeout"`           // cleanup idle sessions, default 5m
	TranscodeTimeout     time.Duration `yaml:"transcode_timeout"`      // max duration per transcode, default 4h
	HWAccel              HWAccelConfig `yaml:"hardware_acceleration"`
}

type HWAccelConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Preferred string `yaml:"preferred"` // auto, vaapi, qsv, nvenc, videotoolbox
}

type ServerConfig struct {
	Bind           string   `yaml:"bind"`
	Port           int      `yaml:"port"`
	BaseURL        string   `yaml:"base_url"`
	TrustedProxies []string `yaml:"trusted_proxies"`
}

func (s ServerConfig) Addr() string {
	return net.JoinHostPort(s.Bind, strconv.Itoa(s.Port))
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
	DSN    string `yaml:"dsn"`
}

type AuthConfig struct {
	JWTSecret          string        `yaml:"jwt_secret"`
	BCryptCost         int           `yaml:"bcrypt_cost"`
	AccessTokenTTL     time.Duration `yaml:"access_token_ttl"`
	RefreshTokenTTL    time.Duration `yaml:"refresh_token_ttl"`
	MaxSessionsPerUser int           `yaml:"max_sessions_per_user"`
}

type RateLimitConfig struct {
	Enabled        bool          `yaml:"enabled"`
	LoginAttempts  int           `yaml:"login_attempts"`
	LoginWindow    time.Duration `yaml:"login_window"`
	LoginLockout   time.Duration `yaml:"login_lockout"`
	GlobalRPM      int           `yaml:"global_rpm"`
	TrustedSubnets []string      `yaml:"trusted_subnets"` // subnets exempt from rate limiting (e.g. LAN)
}

// Load reads and parses the config file at path. Environment variables
// in the form ${VAR} are expanded before parsing.
// When no config file exists, defaults are used and the database path
// is placed in the same directory as the config file (so Docker volumes work).
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — use defaults and generate JWT secret.
			// Place the database in the same directory as the config file
			// so it lands inside the mounted volume (e.g. /config/).
			configDir := filepath.Dir(path)
			if configDir != "." {
				cfg.Database.Path = filepath.Join(configDir, "hubplay.db")
			}
			if cfg.Auth.JWTSecret == "" {
				cfg.Auth.JWTSecret = generateSecret()
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Expand environment variables (${TMDB_API_KEY} etc.)
	data = []byte(os.ExpandEnv(string(data)))

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply env var overrides (HUBPLAY_SERVER_PORT etc.)
	applyEnvOverrides(cfg)

	// Generate JWT secret if not set
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = generateSecret()
	}

	// YAML merge can leave sub-structs half-populated (e.g. user set
	// observability.metrics_enabled: true but omitted path). Fill gaps so
	// every downstream consumer has a usable value.
	if cfg.Observability.MetricsEnabled && cfg.Observability.MetricsPath == "" {
		cfg.Observability.MetricsPath = "/metrics"
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	var errs []error

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port))
	}
	if c.Auth.BCryptCost < 10 || c.Auth.BCryptCost > 14 {
		errs = append(errs, fmt.Errorf("auth.bcrypt_cost must be 10-14, got %d", c.Auth.BCryptCost))
	}
	if c.Auth.AccessTokenTTL < time.Minute {
		errs = append(errs, fmt.Errorf("auth.access_token_ttl must be >= 1m"))
	}
	if c.Auth.RefreshTokenTTL < time.Hour {
		errs = append(errs, fmt.Errorf("auth.refresh_token_ttl must be >= 1h"))
	}

	switch c.Database.Driver {
	case "sqlite":
		if c.Database.Path == "" {
			errs = append(errs, fmt.Errorf("database.path must not be empty for sqlite"))
		} else {
			dir := filepath.Dir(c.Database.Path)
			if dir != "." {
				if _, err := os.Stat(dir); os.IsNotExist(err) {
					errs = append(errs, fmt.Errorf("database.path directory %q does not exist", dir))
				}
			}
		}
	case "postgres":
		if c.Database.DSN == "" {
			errs = append(errs, fmt.Errorf("database.dsn must not be empty for postgres"))
		}
	default:
		errs = append(errs, fmt.Errorf("database.driver must be 'sqlite' or 'postgres', got %q", c.Database.Driver))
	}

	return errors.Join(errs...)
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Bind:           "0.0.0.0",
			Port:           8096,
			TrustedProxies: []string{"127.0.0.1", "172.16.0.0/12"},
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			Path:   "./hubplay.db",
		},
		Auth: AuthConfig{
			BCryptCost:         12,
			AccessTokenTTL:     15 * time.Minute,
			RefreshTokenTTL:    720 * time.Hour,
			MaxSessionsPerUser: 10,
		},
		Logging: logging.Config{
			Level:  "info",
			Format: "text",
			LogIPs: true,
		},
		RateLimit: RateLimitConfig{
			Enabled:        true,
			LoginAttempts:  10,
			LoginWindow:    15 * time.Minute,
			LoginLockout:   5 * time.Minute,
			GlobalRPM:      0, // 0 = unlimited (self-hosted default)
			TrustedSubnets: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		},
		Streaming: StreamingConfig{
			SegmentDuration:      6,
			MaxTranscodeSessions: 4,
			TranscodePreset:      "veryfast",
			DefaultAudioBitrate:  "192k",
			CacheDir:             "",
			IdleTimeout:          5 * time.Minute,
			TranscodeTimeout:     4 * time.Hour,
			HWAccel: HWAccelConfig{
				Enabled:   false,
				Preferred: "auto",
			},
		},
		IPTV: IPTVConfig{
			Transmux: IPTVTransmuxConfig{
				Enabled:             true,
				MaxSessions:         10,
				MaxReencodeSessions: 0, // 0 = derive from MaxSessions in transmux mgr
				IdleTimeout:         30 * time.Second,
				ReadyTimeout:        15 * time.Second,
			},
		},
		Observability: ObservabilityConfig{
			MetricsEnabled: true,
			MetricsPath:    "/metrics",
		},
	}
}

func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback: combine multiple entropy sources and hash with SHA-256.
		// This should never happen on a healthy system, but avoids crashing the server.
		hostname, _ := os.Hostname()
		entropy := fmt.Sprintf("%d:%d:%s:%d", time.Now().UnixNano(), os.Getpid(), hostname, time.Now().UnixNano())
		hash := sha256.Sum256([]byte(entropy))
		return hex.EncodeToString(hash[:])
	}
	return hex.EncodeToString(b)
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("HUBPLAY_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = p
		}
	}
	if v := os.Getenv("HUBPLAY_SERVER_BIND"); v != "" {
		cfg.Server.Bind = v
	}
	if v := os.Getenv("HUBPLAY_DATABASE_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v := os.Getenv("HUBPLAY_DATABASE_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("HUBPLAY_DATABASE_DSN"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("HUBPLAY_AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("HUBPLAY_AUTH_BCRYPT_COST"); v != "" {
		if c, err := strconv.Atoi(v); err == nil {
			cfg.Auth.BCryptCost = c
		}
	}
	if v := os.Getenv("HUBPLAY_LOGGING_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("HUBPLAY_LOGGING_FORMAT"); v != "" {
		cfg.Logging.Format = v
	}
	if v := os.Getenv("HUBPLAY_RATE_LIMIT_ENABLED"); v != "" {
		cfg.RateLimit.Enabled = strings.EqualFold(v, "true")
	}
	if v := os.Getenv("HUBPLAY_STREAMING_CACHE_DIR"); v != "" {
		cfg.Streaming.CacheDir = v
	}
}

// TestConfig returns a config suitable for tests.
func TestConfig() *Config {
	cfg := defaults()
	cfg.Database.Path = ":memory:"
	cfg.Auth.JWTSecret = "test-secret-do-not-use-in-production"
	cfg.Auth.BCryptCost = 10
	cfg.Logging.Level = "debug"
	cfg.Logging.Format = "text"
	cfg.RateLimit.Enabled = false
	return cfg
}
