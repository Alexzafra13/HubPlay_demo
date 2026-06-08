package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// substrings que NO deben aparecer tras redactar
		forbidden []string
		// substrings que SÍ deben seguir (host, scheme)
		want []string
	}{
		{
			name:      "xtream query credentials",
			in:        "http://provider.tv:8080/get.php?username=alice&password=s3cr3t&type=m3u_plus",
			forbidden: []string{"alice", "s3cr3t"},
			want:      []string{"provider.tv", "username=REDACTED", "password=REDACTED"},
		},
		{
			name:      "userinfo in URL",
			in:        "https://user:hunter2@cdn.example.com/playlist.m3u8",
			forbidden: []string{"hunter2", "user:hunter2"},
			want:      []string{"cdn.example.com"},
		},
		{
			name:      "token query param",
			in:        "https://epg.example.com/xmltv.php?token=abcd1234",
			forbidden: []string{"abcd1234"},
			want:      []string{"epg.example.com", "token=REDACTED"},
		},
		{
			name:      "no credentials passes through",
			in:        "https://cdn.example.com/seg-00001.ts",
			forbidden: nil,
			want:      []string{"https://cdn.example.com/seg-00001.ts"},
		},
		{
			name:      "unparseable with password substring",
			in:        "not a url password=leak",
			forbidden: []string{"leak"},
			want:      []string{"password=REDACTED"},
		},
		{
			name:      "empty",
			in:        "",
			forbidden: nil,
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.in)
			for _, f := range tt.forbidden {
				if strings.Contains(got, f) {
					t.Errorf("RedactURL(%q) = %q, no debería contener %q", tt.in, got, f)
				}
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("RedactURL(%q) = %q, debería contener %q", tt.in, got, w)
				}
			}
		})
	}
}

// TestLoggerRedactsURLValues verifica que el logger redacta credenciales
// embebidas en valores con claves de URL conocidas (m3u_url, url, …) sin
// que el call-site tenga que acordarse de redactar.
func TestLoggerRedactsURLValues(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == "password" || a.Key == "token" || a.Key == "refresh_token" {
				return slog.String(a.Key, "[REDACTED]")
			}
			if _, ok := urlValueKeys[a.Key]; ok && a.Value.Kind() == slog.KindString {
				return slog.String(a.Key, RedactURL(a.Value.String()))
			}
			return a
		},
	}
	logger := slog.New(slog.NewJSONHandler(&buf, opts))

	logger.Info("m3u refresh",
		"m3u_url", "http://prov.tv/get.php?username=bob&password=p4ss",
		"epg_url", "https://epg.tv/x.xml?token=zzz",
		"url", "https://cdn.tv/seg.ts",
	)

	out := buf.String()
	for _, leak := range []string{"bob", "p4ss", "zzz"} {
		if strings.Contains(out, leak) {
			t.Errorf("log line filtró credencial %q: %s", leak, out)
		}
	}
	// El host no sensible debe sobrevivir para que el log siga siendo útil.
	if !strings.Contains(out, "prov.tv") || !strings.Contains(out, "cdn.tv") {
		t.Errorf("log perdió info útil de host: %s", out)
	}
	// Sanity: es JSON válido.
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("log no es JSON válido: %v\n%s", err, out)
	}
}
