package iptv

// PublicEPGSource describe una fuente EPG pública curada para el
// dropdown admin. Las URLs del catálogo DEBEN verificarse (200 OK,
// XMLTV válido) en cada release. Las rows persistidas mantienen su
// propia URL snapshot.
type PublicEPGSource struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`  // ISO 639-1
	Countries   []string `json:"countries"` // ISO 3166-1 alpha-2, lowercased
	URL         string   `json:"url"`
}

// publicEPGSources — catálogo verificado. Cada URL fue comprobada
// contra upstream al entrar. Cobertura internacional omitida porque
// las URLs de epg.pw/iptv-org dan 404 o requieren API keys.
// Var a nivel paquete, read-only post-init.
var publicEPGSources = []PublicEPGSource{
	{
		ID:          "davidmuma-guiatv",
		Name:        "davidmuma — Guía TV (España)",
		Description: "TDT y cadenas IPTV españolas. Comprimida (.gz), actualización diaria.",
		Language:    "es",
		Countries:   []string{"es"},
		URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiatv.xml.gz",
	},
	{
		ID:          "davidmuma-guiaiptv",
		Name:        "davidmuma — Guía IPTV amplia (España)",
		Description: "Variante amplia con más cadenas IPTV (deportes, cine, autonómicas).",
		Language:    "es",
		Countries:   []string{"es"},
		URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiaiptv.xml",
	},
	{
		ID:          "davidmuma-guiatv-plex",
		Name:        "davidmuma — Guía TV optimizada (España)",
		Description: "Variante del mismo autor optimizada para clientes tipo Plex / Jellyfin; incluye metadatos extra.",
		Language:    "es",
		Countries:   []string{"es"},
		URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiatv_plex.xml.gz",
	},
	{
		ID:          "davidmuma-guiafanart",
		Name:        "davidmuma — Guía con Fanart (España)",
		Description: "Igual que guiatv pero con imágenes de fondo enriquecidas desde Fanart.tv.",
		Language:    "es",
		Countries:   []string{"es"},
		URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/guiafanart.xml.gz",
	},
	{
		ID:          "davidmuma-tiviepg",
		Name:        "davidmuma — TiviEPG (España)",
		Description: "Guía alternativa ligera del mismo autor; útil como fallback cuando guiatv tarda en refrescar.",
		Language:    "es",
		Countries:   []string{"es"},
		URL:         "https://raw.githubusercontent.com/davidmuma/EPG_dobleM/master/tiviepg.xml",
	},
}

// PublicEPGSources devuelve el catálogo verificado.
func PublicEPGSources() []PublicEPGSource {
	return publicEPGSources
}

// FindEPGSource busca una fuente curada por id.
func FindEPGSource(id string) (PublicEPGSource, bool) {
	for _, src := range publicEPGSources {
		if src.ID == id {
			return src, true
		}
	}
	return PublicEPGSource{}, false
}
