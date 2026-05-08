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

// New returns a logger that writes to stdout with the configured
// level/format. Most callers should use NewWithBuffer instead — this
// constructor stays around for tests and tools that don't need the
// in-memory ring + admin SSE surface.
func New(cfg Config) *slog.Logger {
	logger, _ := NewWithBuffer(cfg)
	return logger
}

// NewWithBuffer returns a logger plus the in-memory ring buffer
// the admin "Logs" surface tails. The ring wraps the configured
// JSON/text handler — every record still hits stdout, AND it lands
// in the ring + fans out to any SSE subscriber. Capacity is fixed
// at 500 entries (about 500 KB worst-case) which fits a single
// incident's noise window without bloating the process.
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
