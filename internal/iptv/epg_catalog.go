package iptv

// PublicEPGSource describes a curated, well-known XMLTV EPG feed. The
// catalog powers the admin dropdown used to wire multiple providers to
// a single library: pick "davidmuma-guiatv" for Spanish IPTV coverage,
// optionally add "epgpw-es" to fill channels davidmuma doesn't list.
//
// Sources live here (not in the DB) so a new binary ships the latest
// known-good URLs without any migration. If davidmuma ever reshuffles
// their paths we update the constant and release. Admin-supplied
// custom URLs live in `library_epg_sources` alongside catalog picks.
type PublicEPGSource struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`  // ISO 639-1
	Countries   []string `json:"countries"` // ISO 3166-1 alpha-2, lowercased
	URL         string   `json:"url"`
}

// PublicEPGSources returns the list of curated EPG providers shipped
// with the binary. Ordered by expected usefulness for a fresh install:
// Spanish-first because that's the largest user base today, then the
// other epg.pw language variants, then fallbacks. Custom URLs that
// operators add through the UI sit next to these in the same list.
func PublicEPGSources() []PublicEPGSource {
	return []PublicEPGSource{
		{
			ID:          "davidmuma-guiatv",
			Name:        "davidmuma — Guía TV (España)",
			Description: "TDT y cadenas IPTV españolas. Mantenida por la comunidad, actualización diaria.",
			Language:    "es",
			Countries:   []string{"es"},
			URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiatv.xml.gz",
		},
		{
			ID:          "davidmuma-guiaiptv",
			Name:        "davidmuma — Guía IPTV amplia (España)",
			Description: "Variante con más cadenas IPTV no-TDT (deportes, cine, autonómicas).",
			Language:    "es",
			Countries:   []string{"es"},
			URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiaiptv.xml",
		},
		{
			ID:          "davidmuma-movistar",
			Name:        "davidmuma — Movistar Plus+ (España)",
			Description: "Cadenas premium de Movistar Plus+.",
			Language:    "es",
			Countries:   []string{"es"},
			URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiaiptvmovistar.xml",
		},
		{
			ID:          "davidmuma-tdtsat",
			Name:        "davidmuma — TDT satélite (España)",
			Description: "Canales de TDT emitidos por satélite (Astra, Hispasat).",
			Language:    "es",
			Countries:   []string{"es"},
			URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/tdtsat.xml",
		},
		{
			ID:          "epgpw-es",
			Name:        "epg.pw — España",
			Description: "Proveedor internacional con cobertura amplia; útil como relleno de davidmuma.",
			Language:    "es",
			Countries:   []string{"es"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=es",
		},
		{
			ID:          "epgpw-en",
			Name:        "epg.pw — English",
			Description: "Cobertura general en inglés (US / UK / internacional).",
			Language:    "en",
			Countries:   []string{"us", "gb", "ca", "au", "ie"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=en",
		},
		{
			ID:          "epgpw-fr",
			Name:        "epg.pw — Français",
			Description: "Cadenas francófonas (Francia, Bélgica, Suiza, Canadá).",
			Language:    "fr",
			Countries:   []string{"fr", "be", "ch", "ca"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=fr",
		},
		{
			ID:          "epgpw-de",
			Name:        "epg.pw — Deutsch",
			Description: "Alemán (Alemania, Austria, Suiza).",
			Language:    "de",
			Countries:   []string{"de", "at", "ch"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=de",
		},
		{
			ID:          "epgpw-it",
			Name:        "epg.pw — Italiano",
			Description: "Cadenas italianas.",
			Language:    "it",
			Countries:   []string{"it", "sm", "va"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=it",
		},
		{
			ID:          "epgpw-pt",
			Name:        "epg.pw — Português",
			Description: "Portugal y Brasil.",
			Language:    "pt",
			Countries:   []string{"pt", "br"},
			URL:         "https://epg.pw/api/epg.xml.gz?lang=pt",
		},
	}
}

// FindEPGSource looks up a curated source by id.
func FindEPGSource(id string) (PublicEPGSource, bool) {
	for _, src := range PublicEPGSources() {
		if src.ID == id {
			return src, true
		}
	}
	return PublicEPGSource{}, false
}
