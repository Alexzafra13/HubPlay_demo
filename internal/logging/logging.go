package logging

import (
	"log/slog"
	"os"
	"regexp"
)

type Config struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	LogIPs bool   `yaml:"log_ips"`
}

// urlValueKeys son las claves cuyo valor es una URL que puede llevar
// credenciales embebidas. Las playlists IPTV (Xtream/M3U) casi siempre
// incrustan `?username=…&password=…` o userinfo `user:pass@host`; sin
// redacción acaban en stdout y en el ring-buffer del panel admin. Se
// redacta por VALOR (no por clave) porque la clave aquí es legítima.
var urlValueKeys = map[string]struct{}{
	"url":          {},
	"upstream":     {},
	"m3u_url":      {},
	"epg_url":      {},
	"playlist_url": {},
	"stream_url":   {},
}

// userinfoRe captura el `user:pass@` tras el esquema (`scheme://`). Se
// trabaja por regex en vez de url.Parse porque éste es demasiado
// permisivo (acepta casi cualquier cosa como path) y falla en URLs con
// caracteres de control — justo las playlists mal formadas que más
// interesa redactar.
var userinfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s]*@`)

// sensitiveParamRe captura `clave=valor` para las claves sensibles, en
// query string o en cualquier parte de la cadena. Case-insensitive.
var sensitiveParamRe = regexp.MustCompile(`(?i)(username|password|pass|token|auth)=[^&\s]*`)

// RedactURL elimina credenciales de una URL para logging: enmascara el
// userinfo (`user:pass@`) y los parámetros sensibles (`username`,
// `password`, `pass`, `token`, `auth`). No usa url.Parse — opera por
// substitución directa, así funciona también con cadenas mal formadas
// sin riesgo de panic ni de "limpiar de más".
func RedactURL(raw string) string {
	if raw == "" {
		return raw
	}
	out := userinfoRe.ReplaceAllString(raw, "${1}redacted@")
	out = sensitiveParamRe.ReplaceAllString(out, "${1}=REDACTED")
	return out
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
			// Redacción por valor: claves cuyo contenido es una URL con
			// posibles credenciales (playlists IPTV, EPG, upstreams).
			if _, ok := urlValueKeys[a.Key]; ok && a.Value.Kind() == slog.KindString {
				return slog.String(a.Key, RedactURL(a.Value.String()))
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
