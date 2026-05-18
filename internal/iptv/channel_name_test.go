package iptv

import "testing"

func TestSanitizeChannelName(t *testing.T) {
	// Casos reales del campo. La columna `wantQuality` está pensada
	// para el badge del frontend — `""` significa "no enseñar badge".
	tests := []struct {
		name         string
		raw          string
		wantName     string
		wantQuality  string
	}{
		// Limpia: nombres bien etiquetados no se tocan.
		{"clean", "ESPN", "ESPN", ""},
		{"clean with number", "Canal 24h", "Canal 24h", ""},

		// Tags de calidad entre brackets.
		{"bracket HD", "Cinemax [HD]", "Cinemax", "HD"},
		{"bracket FHD", "ESPN [1080p]", "ESPN", "FHD"},
		{"bracket 4K", "DAZN [4K]", "DAZN", "UHD"},
		{"bracket 2160p", "Movistar Plus [2160p]", "Movistar Plus", "UHD"},
		{"bracket SD", "Canal 13 [SD]", "Canal 13", "SD"},

		// Geo-blocked y VIP: se eliminan, sin badge.
		{"geo-blocked", "HBO [geo-blocked]", "HBO", ""},
		{"VIP tag", "Cinemax [VIP]", "Cinemax", ""},
		{"premium", "DAZN [Premium]", "DAZN", ""},

		// Calidad mezclada con basura.
		{"VIP plus HD", "Movistar Deportes [VIP] FHD", "Movistar Deportes", "FHD"},
		{"codec tag", "DAZN 4K [HEVC]", "DAZN", "UHD"},
		{"multiple brackets", "ESPN [HD] [Latino]", "ESPN", "HD"},

		// Pipes (convención común en M3U LATAM/ES).
		{"pipe ES", "AXN |ES|", "AXN", ""},
		{"pipe quality", "Comedy |HD|", "Comedy", "HD"},

		// Parens.
		{"paren HD", "Cinemax (HD)", "Cinemax", "HD"},
		{"paren ES", "Discovery (ES)", "Discovery", ""},

		// Trailing quality sin brackets.
		{"trailing HD", "ESPN HD", "ESPN", "HD"},
		{"trailing FHD", "DAZN FHD", "DAZN", "FHD"},
		{"trailing 1080p", "Sky 1080p", "Sky", "FHD"},

		// Símbolos decorativos.
		{"stars", "★ Disney Channel ★", "Disney Channel", ""},
		{"bullet", "• HBO •", "HBO", ""},

		// Mid-name 4K NO debe stripear (es parte del branding).
		// Pero el detector de quality SÍ debe detectar UHD aunque
		// el "4K" se quede en el nombre — es un trade-off: la
		// resolución es info útil, el nombre con el branding
		// completo también. El test fija el comportamiento actual:
		// quality detectada, mid-name conservado.
		{"mid-name 4K", "Studio 4K Network", "Studio 4K Network", "UHD"},

		// Edge case: nombre vacío → vacío.
		{"empty", "", "", ""},

		// Edge case: sólo tags → fallback al raw para no enseñar hueco.
		{"only tags", "[VIP]", "[VIP]", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, quality := SanitizeChannelName(tc.raw)
			if name != tc.wantName {
				t.Errorf("name: raw=%q got %q want %q", tc.raw, name, tc.wantName)
			}
			if quality != tc.wantQuality {
				t.Errorf("quality: raw=%q got %q want %q", tc.raw, quality, tc.wantQuality)
			}
		})
	}
}
