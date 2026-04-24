package iptv

// PublicEPGSource describes a curated, well-known XMLTV EPG feed. The
// catalog powers the admin dropdown used to wire multiple providers
// to a single library. Admin-supplied custom URLs live next to
// catalog picks in the same `library_epg_sources` table.
//
// Shipping policy: catalog URLs MUST be verified (200 OK, returns
// XMLTV) at release time. A broken catalog entry is worse than no
// entry because it pre-fills the dropdown with a trap. When a
// provider changes their layout, update the constants and cut a
// release — the persisted rows keep their own URL snapshot so
// operators with a prior version keep working until they re-add.
type PublicEPGSource struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`  // ISO 639-1
	Countries   []string `json:"countries"` // ISO 3166-1 alpha-2, lowercased
	URL         string   `json:"url"`
}

// publicEPGSources is the verified catalog.
//
// Every URL here was HEAD-checked against upstream at the time the
// entry landed. If you add more, do the same: a catalog that ships
// 404-ing entries creates the "added it, badge says error, what do
// I do now?" confusion we just fixed. Custom URLs the admin pastes
// are always free-form — only the curated list carries the trust of
// being ready-to-use.
//
// International coverage deliberately omitted in this iteration
// because the epg.pw and iptv-org URLs originally shipped either
// 404 or require API keys we can't verify. Adding a wrong URL is
// worse than an empty row; operators can still paste any XMLTV
// endpoint they trust through the "URL personalizada" field.
//
// Kept as a package-level var (not a function) so the handler and
// service don't re-allocate on every catalog lookup. The slice is
// never mutated after init; callers receive a shared, read-only view.
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

// PublicEPGSources returns the verified catalog.
func PublicEPGSources() []PublicEPGSource {
	return publicEPGSources
}

// FindEPGSource looks up a curated source by id.
func FindEPGSource(id string) (PublicEPGSource, bool) {
	for _, src := range publicEPGSources {
		if src.ID == id {
			return src, true
		}
	}
	return PublicEPGSource{}, false
}
