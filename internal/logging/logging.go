package logging

import (
	"log/slog"
	"os"
)

type Config struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	LogIPs bool   `yaml:"log_ips"`
}

// New: logger sin ring buffer. Casi todos los callers deben usar NewWithBuffer;
// esto queda para tests y herramientas que no necesitan el admin SSE.
func New(cfg Config) *slog.Logger {
	logger, _ := NewWithBuffer(cfg)
	return logger
}

// NewWithBuffer: logger + ring buffer en memoria para el panel admin "Logs".
// Cada record va a stdout Y al ring (fan-out por SSE). Capacidad fija 500
// (~500 KB worst-case) — encaja una incidencia sin inflar el proceso.
func NewWithBuffer(cfg Config) (*slog.Logger, *Buffer) {
	opts := &slog.HandlerOptions{
		Level: parseLevel(cfg.Level),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "password" || a.Key == "token" || a.Key == "refresh_token" {
				return slog.String(a.Key, "[REDACTED]")
			}
			return a
		},
	}

	var inner slog.Handler
	if cfg.Format == "json" {
		inner = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		inner = slog.NewTextHandler(os.Stdout, opts)
	}

	buf := NewBuffer(inner, 500)
	return slog.New(buf), buf
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
