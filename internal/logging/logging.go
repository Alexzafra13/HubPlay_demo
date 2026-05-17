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

// New devuelve un logger sin anillo en memoria. Casi siempre interesa
// usar NewWithBuffer; esto queda para tests y herramientas que no
// necesitan el panel admin de logs.
func New(cfg Config) *slog.Logger {
	logger, _ := NewWithBuffer(cfg)
	return logger
}

// NewWithBuffer devuelve un logger y un anillo en memoria que alimenta
// el panel admin "Logs". Cada log va a stdout y también al anillo.
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
