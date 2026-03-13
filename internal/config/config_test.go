package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()

	if cfg.Server.Port != 8096 {
		t.Errorf("expected default port 8096, got %d", cfg.Server.Port)
	}
	if cfg.Server.Bind != "0.0.0.0" {
		t.Errorf("expected default bind 0.0.0.0, got %s", cfg.Server.Bind)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected default driver sqlite, got %s", cfg.Database.Driver)
	}
	if cfg.Auth.BCryptCost != 12 {
		t.Errorf("expected default bcrypt cost 12, got %d", cfg.Auth.BCryptCost)
	}
	if cfg.Auth.AccessTokenTTL != 15*time.Minute {
		t.Errorf("expected default access TTL 15m, got %v", cfg.Auth.AccessTokenTTL)
	}
	if cfg.Auth.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("expected default refresh TTL 720h, got %v", cfg.Auth.RefreshTokenTTL)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("expected default log level info, got %s", cfg.Logging.Level)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := defaults()
	cfg.Database.Path = "./test.db"

	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			cfg.Server.Port = tt.port
			if err := cfg.Validate(); err == nil {
				t.Error("expected validation error for invalid port")
			}
		})
	}
}

func TestValidate_InvalidBCryptCost(t *testing.T) {
	tests := []struct {
		name string
		cost int
	}{
		{"too low", 5},
		{"too high", 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			cfg.Auth.BCryptCost = tt.cost
			if err := cfg.Validate(); err == nil {
				t.Error("expected validation error for invalid bcrypt cost")
			}
		})
	}
}

func TestValidate_InvalidTokenTTL(t *testing.T) {
	cfg := defaults()
	cfg.Auth.AccessTokenTTL = 30 * time.Second
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for access TTL < 1m")
	}

	cfg = defaults()
	cfg.Auth.RefreshTokenTTL = 30 * time.Minute
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for refresh TTL < 1h")
	}
}

func TestValidate_InvalidDriver(t *testing.T) {
	cfg := defaults()
	cfg.Database.Driver = "mysql"
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for unsupported driver")
	}
}

func TestValidate_PostgresRequiresDSN(t *testing.T) {
	cfg := defaults()
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty postgres DSN")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing config, got: %v", err)
	}
	if cfg.Auth.JWTSecret == "" {
		t.Error("expected auto-generated JWT secret")
	}
	if cfg.Server.Port != 8096 {
		t.Errorf("expected default port, got %d", cfg.Server.Port)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hubplay.yaml")

	content := `
server:
  port: 9090
  bind: "127.0.0.1"
database:
  driver: "sqlite"
  path: "./test.db"
auth:
  bcrypt_cost: 11
logging:
  level: "debug"
  format: "json"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.Bind != "127.0.0.1" {
		t.Errorf("expected bind 127.0.0.1, got %s", cfg.Server.Bind)
	}
	if cfg.Auth.BCryptCost != 11 {
		t.Errorf("expected bcrypt cost 11, got %d", cfg.Auth.BCryptCost)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected log level debug, got %s", cfg.Logging.Level)
	}
}

func TestLoad_EnvVarExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hubplay.yaml")

	t.Setenv("TEST_JWT_SECRET", "my-test-secret")

	content := `
server:
  port: 8096
database:
  driver: "sqlite"
  path: "./test.db"
auth:
  jwt_secret: "${TEST_JWT_SECRET}"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Auth.JWTSecret != "my-test-secret" {
		t.Errorf("expected jwt secret 'my-test-secret', got %q", cfg.Auth.JWTSecret)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hubplay.yaml")

	content := `
server:
  port: 8096
database:
  driver: "sqlite"
  path: "./test.db"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HUBPLAY_SERVER_PORT", "3000")
	t.Setenv("HUBPLAY_LOGGING_LEVEL", "error")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("expected env override port 3000, got %d", cfg.Server.Port)
	}
	if cfg.Logging.Level != "error" {
		t.Errorf("expected env override log level error, got %s", cfg.Logging.Level)
	}
}

func TestServerAddr(t *testing.T) {
	s := ServerConfig{Bind: "127.0.0.1", Port: 8096}
	if addr := s.Addr(); addr != "127.0.0.1:8096" {
		t.Errorf("expected 127.0.0.1:8096, got %s", addr)
	}
}

func TestTestConfig(t *testing.T) {
	cfg := TestConfig()
	if cfg.Database.Path != ":memory:" {
		t.Error("test config should use in-memory db")
	}
	if cfg.Auth.JWTSecret == "" {
		t.Error("test config should have a JWT secret")
	}
	if cfg.Auth.BCryptCost != 10 {
		t.Error("test config should use low bcrypt cost for speed")
	}
}
