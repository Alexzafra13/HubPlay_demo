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
	Retention      RetentionConfig     `yaml:"retention"`
	Upload         UploadConfig        `yaml:"upload"`
	MDNS           MDNSConfig          `yaml:"mdns"`
	SetupCompleted bool                `yaml:"setup_completed"`
}

// MDNSConfig: auto-anuncio del servidor en la LAN. Cuando enabled,
// cualquier dispositivo de la red resuelve "<hostname>.local" sin
// configurar router ni DNS.
type MDNSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Hostname string `yaml:"hostname"`
}

// UploadConfig: knobs runtime para subidas de media (PR2 feature upload).
// El cero-valor de cada knob deja al servicio aplicar sus defaults
// internos en `upload.DefaultConfig`; este struct sólo añade lo que es
// específicamente operator-facing (path del staging, switch enabled).
type UploadConfig struct {
	// Enabled gobierna el cableado del feature al completo. False ⇒ los
	// handlers HTTP no se montan y el binario arranca sin staging dir.
	// Default true.
	Enabled bool `yaml:"enabled"`

	// StagingDir: directorio donde tusd escribe los chunks antes de
	// validar + mover a librería. Default <config dir>/uploads/staging
	// si vacío. Debe estar en el MISMO volumen que las librerías destino
	// para que el move sea rename atómico — cross-fs cae a copy+remove.
	StagingDir string `yaml:"staging_dir"`

	// MaxBytesPerUpload tope absoluto por fichero. Independiente de la
	// cuota per-user (que es agregada). Default 50 GiB. 0 = sin tope
	// (riesgoso: un cliente puede anunciar 100 PB y reservar toda la
	// cuota antes de pegar un byte; el service rechaza igualmente
	// porque ReserveUploadBytes mira la cuota, pero un tope explícito
	// es mejor que un fallo en cascada).
	MaxBytesPerUpload int64 `yaml:"max_bytes_per_upload"`

	// MinDurationMs duración mínima reportada por ffprobe para que un
	// upload de video se acepte. Defensa contra payloads de 1s que
	// pasan magic-bytes + ffprobe pero no son media real. Default 1000.
	MinDurationMs int64 `yaml:"min_duration_ms"`
}

// RetentionConfig: vida útil de tablas append-only (EPG + audit federation).
// Sin esto, un install con IPTV + federation paired llega a GB de SQLite en
// semanas. Valores ≤0 desactivan el sweep correspondiente.
type RetentionConfig struct {
	// EPGPrograms: ventana relativa a `now`; rows con end_time anterior se
	// borran cada tick. Default 24h.
	EPGPrograms time.Duration `yaml:"epg_programs"`

	// FederationAuditLog: ventana de audit. La tabla crece en cada request
	// cross-peer (search, stream, progress, share). Default 30d.
	FederationAuditLog time.Duration `yaml:"federation_audit_log"`

	// SweepInterval: cadencia del ticker. Default 24h. Más bajo = carga
	// DB más estable; más alto = más deletes batch por tick.
	SweepInterval time.Duration `yaml:"sweep_interval"`
}

// IPTVConfig: knobs runtime del subsistema IPTV. Sólo transmux expuesto hoy;
// el resto (proxy timeouts, scheduler) sigue hard-coded y migrará cuando
// necesite tuning operator-facing.
type IPTVConfig struct {
	Transmux IPTVTransmuxConfig `yaml:"transmux"`
}

// IPTVTransmuxConfig: controla el session manager de MPEG-TS → HLS en vivo
// (internal/iptv/transmux.go). Defaults sanos para single-host self-hosted.
type IPTVTransmuxConfig struct {
	// Enabled: false → channel-stream cae al passthrough raw y los upstreams
	// MPEG-TS dejan de funcionar en navegadores (HLS sigue ok). Default true.
	Enabled bool `yaml:"enabled"`

	// MaxSessions: tope de ffmpegs simultáneos. Una sesión la comparten todos
	// los viewers del mismo canal — 5 personas viendo 5 canales = 5 sesiones.
	// Default 10.
	MaxSessions int `yaml:"max_sessions"`

	// MaxReencodeSessions: tope en re-encode (la única ruta con coste real
	// CPU/GPU). Cap separado evita que una tormenta de codec-crash sature
	// todos los encoder slots. 0 = MaxSessions/2 (floor 1).
	MaxReencodeSessions int `yaml:"max_reencode_sessions"`

	// IdleTimeout: vida sin segment requests antes de que el reaper mate.
	// Más bajo = cleanup más rápido pero más spawn churn en zap rápido.
	// Default 30s.
	IdleTimeout time.Duration `yaml:"idle_timeout"`

	// ReadyTimeout: espera del manifest handler al primer segmento de ffmpeg
	// antes de fallar la sesión. Default 15s — más alto que la latencia
	// típica (3-5s) en upstreams sanos; acotado para que providers muertos
	// no cuelguen el player UI.
	ReadyTimeout time.Duration `yaml:"ready_timeout"`
}

// ObservabilityConfig: endpoint Prometheus /metrics. Default activado en
// /metrics. enabled:false para no exponerlos; metrics_path para moverlos por
// higiene de reverse-proxy.
//
// MetricsToken: cuando se define, /metrics exige `Authorization: Bearer
// <token>` (o `?token=<token>` para scrapers que no pueden poner cabecera).
// Vacío = sin auth (comportamiento histórico) — en ese caso el operador
// DEBE bloquear /metrics en su reverse proxy, porque el bind por defecto
// es 0.0.0.0 y las métricas filtran internals (sesiones, rutas, FDs). La
// app loguea un aviso al arrancar si está expuesto sin token.
type ObservabilityConfig struct {
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	MetricsPath    string `yaml:"metrics_path"`
	MetricsToken   string `yaml:"metrics_token"`
	// PprofEnabled exposes the net/http/pprof profiling endpoints under
	// /debug/pprof. Off by default and, when on, served ONLY behind the
	// metrics_token (heap dumps leak memory contents and CPU/trace
	// profiles are a DoS lever, so it fails closed without a token).
	PprofEnabled bool `yaml:"pprof_enabled"`
}

type StreamingConfig struct {
	SegmentDuration             int           `yaml:"segment_duration"`                 // segundos, default 6
	MaxTranscodeSessions        int           `yaml:"max_transcode_sessions"`           // cap global, default 4
	MaxTranscodeSessionsPerUser int           `yaml:"max_transcode_sessions_per_user"`  // cap per-user, default 2 — evita que 1 user agote el pool con seek-loops o fanout
	TranscodePreset             string        `yaml:"transcode_preset"`                 // veryfast, fast, medium
	DefaultAudioBitrate         string        `yaml:"default_audio_bitrate"`            // p.ej. "192k"
	CacheDir                    string        `yaml:"cache_dir"`                        // directorio de salida del transcode
	IdleTimeout                 time.Duration `yaml:"idle_timeout"`                     // limpieza de sesiones idle, default 90s
	TranscodeTimeout            time.Duration `yaml:"transcode_timeout"`                // duración máxima por transcode, default 4h
	HWAccel                     HWAccelConfig `yaml:"hardware_acceleration"`
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
	TrustedSubnets []string      `yaml:"trusted_subnets"` // subnets exentas (p.ej. LAN)
}

// Load: lee/parsea el YAML expandiendo ${VAR} antes. Si el fichero no
// existe, defaults + DB junto al config path (para que vol. Docker funcione).
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		// Expande ${TMDB_API_KEY} etc.
		data = []byte(os.ExpandEnv(string(data)))
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	case os.IsNotExist(err):
		// Sin fichero — usamos defaults. DB junto al config path para que
		// caiga en el volumen montado (p.ej. /config/).
		configDir := filepath.Dir(path)
		if configDir != "." {
			cfg.Database.Path = filepath.Join(configDir, "hubplay.db")
		}
	default:
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Env overrides aplican aunque no haya fichero — HUBPLAY_SERVER_BASE_URL
	// / HUBPLAY_AUTH_JWT_SECRET en deploys fresh sin yaml tienen que ir
	// a algún sitio.
	applyEnvOverrides(cfg)

	// JWT secret auto-gen si ni file ni env lo dieron.
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = generateSecret()
	}

	// El merge YAML puede dejar sub-structs a medias (p.ej. usuario puso
	// metrics_enabled:true pero omitió path). Rellenamos huecos para que
	// downstream tenga siempre valor utilizable.
	if cfg.Observability.MetricsEnabled && cfg.Observability.MetricsPath == "" {
		cfg.Observability.MetricsPath = "/metrics"
	}

	// Upload staging dir: si el operador no lo especificó, lo dejamos
	// junto a la DB en el mismo volumen montado. Es CRÍTICO que viva en
	// el mismo filesystem que las librerías destino para que el move
	// final sea rename atómico — si no, cae a copy+remove cross-device.
	if cfg.Upload.Enabled && cfg.Upload.StagingDir == "" {
		configDir := filepath.Dir(path)
		if configDir == "" || configDir == "." {
			configDir = "./uploads"
		} else {
			configDir = filepath.Join(configDir, "uploads")
		}
		cfg.Upload.StagingDir = filepath.Join(configDir, "staging")
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
			TrustedProxies: []string{"127.0.0.1/32", "172.16.0.0/12"},
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
			TrustedSubnets: []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		},
		Streaming: StreamingConfig{
			SegmentDuration: 6,
			// MaxTranscodeSessions / MaxTranscodeSessionsPerUser /
			// TranscodePreset default a 0 / "" a propósito:
			// stream.AutoTuneStreaming (lo llama NewManager tras detectar HW)
			// los rellena con valores HW-aware en cada boot. Si el operador
			// los pone explícitos en yaml/panel, el auto-tuner sólo toca los
			// que siguen en sentinel-zero — los explícitos sobreviven.
			DefaultAudioBitrate: "192k",
			CacheDir:            "",
			IdleTimeout:         90 * time.Second,
			TranscodeTimeout:    4 * time.Hour,
			HWAccel: HWAccelConfig{
				Enabled:   false,
				Preferred: "auto",
			},
		},
		IPTV: IPTVConfig{
			Transmux: IPTVTransmuxConfig{
				Enabled:             true,
				MaxSessions:         10,
				MaxReencodeSessions: 0, // 0 = el transmux mgr lo deriva de MaxSessions
				IdleTimeout:         30 * time.Second,
				ReadyTimeout:        15 * time.Second,
			},
		},
		Observability: ObservabilityConfig{
			MetricsEnabled: true,
			MetricsPath:    "/metrics",
		},
		Retention: RetentionConfig{
			EPGPrograms:        24 * time.Hour,
			FederationAuditLog: 30 * 24 * time.Hour,
			SweepInterval:      24 * time.Hour,
		},
		Upload: UploadConfig{
			Enabled:           true,
			StagingDir:        "", // resolved relative to config dir in Load
			MaxBytesPerUpload: 50 * 1024 * 1024 * 1024, // 50 GiB
			MinDurationMs:     1000,
		},
		MDNS: MDNSConfig{
			Enabled:  true,
			Hostname: "hubplay",
		},
	}
}

func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback: combinamos varias fuentes de entropía + SHA-256. No
		// debería pasar en un sistema sano, pero evita crashear el server.
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
	if v := os.Getenv("HUBPLAY_SERVER_BASE_URL"); v != "" {
		// Federation publica esta URL en /federation/info — los peers la
		// usan para venir a nosotros. La env override es el escape hatch
		// explícito; si está vacía, federation la deriva del request
		// inbound (default plug-and-play). Usar sólo cuando el admin entra
		// por una URL distinta a la que deben usar los peers (p.ej.
		// Tailscale interno vs dominio público).
		cfg.Server.BaseURL = v
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
	if v := os.Getenv("HUBPLAY_OBSERVABILITY_METRICS_TOKEN"); v != "" {
		cfg.Observability.MetricsToken = v
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

// TestConfig: config segura para tests.
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
