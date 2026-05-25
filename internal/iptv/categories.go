package iptv

import (
	"strings"
)

// Category colapsa el group-title libre del M3U a un conjunto fijo
// que el frontend usa para filtros e iconos.
type Category string

const (
	CategoryGeneral        Category = "general"
	CategoryNews           Category = "news"
	CategorySports         Category = "sports"
	CategoryMovies         Category = "movies"
	CategoryMusic          Category = "music"
	CategoryEntertainment  Category = "entertainment"
	CategoryKids           Category = "kids"
	CategoryCulture        Category = "culture"
	CategoryDocumentaries  Category = "documentaries"
	CategoryInternational  Category = "international"
	CategoryTravel         Category = "travel"
	CategoryReligion       Category = "religion"
	CategoryAdult          Category = "adult"
)

// AllCategories en orden de UI.
var AllCategories = []Category{
	CategoryGeneral,
	CategoryNews,
	CategorySports,
	CategoryMovies,
	CategoryMusic,
	CategoryEntertainment,
	CategoryDocumentaries,
	CategoryKids,
	CategoryCulture,
	CategoryInternational,
	CategoryTravel,
	CategoryReligion,
	CategoryAdult,
}

// categoryKeyword — las entradas anteriores ganan en ambiguos
// (ej. "sports news" → Sports, no News).
type categoryKeyword struct {
	cat      Category
	keywords []string
}

// categoryKeywords — tabla de keywords por prioridad. Minúsculas, sin acentos.
var categoryKeywords = []categoryKeyword{
	// Adult primero: evitar clasificar contenido adulto como entretenimiento.
	{CategoryAdult, []string{"adult", "adulto", "xxx", "erotic", "erotica", "porn", "hustler", "playboy"}},

	// Deportes antes que noticias — "sports news" → Sports.
	{CategorySports, []string{
		"sport", "deport", "futbol", "football", "soccer", "laliga", "la liga",
		"champions", "uefa", "fifa", "nba", "nfl", "mlb", "nhl", "ufc", "boxeo", "boxing",
		"dazn", "eurosport", "gol", "movistar deportes", "movistar laliga",
		"f1", "motogp", "formula", "formula1", "formula 1", "tennis", "tenis", "golf",
		"teledeporte", "real madrid tv", "barca tv", "bein sport",
	}},

	// Kids antes que entretenimiento.
	{CategoryKids, []string{
		"kids", "infantil", "ninos", "cartoon", "disney", "nick", "nickelodeon",
		"boomerang", "baby tv", "babytv", "clan", "pocoyo", "junior", "cbeebies",
	}},

	// Documentales antes que cultura.
	{CategoryDocumentaries, []string{
		"documental", "documentary", "doc ", " doc", "nat geo", "natgeo",
		"national geographic", "discovery", "history", "historia", "animal planet",
		"love nature", "dmax", "odisea",
	}},

	{CategoryNews, []string{
		// "informati" cubre ES (informativo/s) y CA (informatiu/s).
		// "telediari" cubre ES y CA. "notici" cubre noticia/s/notícies.
		"news", "noticia", "notici", "informati", "telediari", "24h", "24 horas",
		"rne", "cnn", "bbc news", "bbc world", "euronews", "al jazeera", "bloomberg",
		"la sexta noticias", "antena 3 noticias", "sky news",
	}},

	{CategoryMovies, []string{
		"movie", "film", "pelicula", "pelis", "cinema", "cine",
		"hollywood", "classic movies", "tcm", "amc", "fox movies", "paramount",
		"sundance", "somos", "cinefilos",
	}},

	{CategoryMusic, []string{
		"music", "musica", "mtv", "vh1", "vevo", "hits", " 40 ", "los40",
		"los 40", "cadena dial", "kiss fm", "radiole", "jazz", "reggae",
	}},

	{CategoryTravel, []string{
		"travel", "viaje", "voyage", "tourism", "turismo", "discovery travel",
		"national geographic travel",
	}},

	{CategoryCulture, []string{
		"culture", "cultura", "cultural", "arte ", " arte", "art channel", "mezzo", "stingray",
	}},

	{CategoryReligion, []string{
		"religion", "religi", "catholic", "catolic", "cristiano", "christian",
		"ewtn", "13 tv", "trece", "iglesia",
	}},

	{CategoryEntertainment, []string{
		"entertain", "entreten", "show", "reality", "variety", "comedy", "comedia",
		"divinity", "neox", "cuatro", "energy", "fdf", "paramount comedy",
	}},

	// Internacional último: la mayoría de group-titles con "international"
	// tienen señal más fuerte (news, sports). Fallback.
	{CategoryInternational, []string{
		"international", "internacional", "world", "mundo", "europa", "europe",
	}},
}

// Canonical mapea un group-title crudo a una Category canónica.
// Case/accent-insensitive. Devuelve CategoryGeneral para vacíos o
// desconocidos. El GroupName crudo se preserva en M3UChannel.
func Canonical(groupTitle string) Category {
	needle := normalize(groupTitle)
	if needle == "" {
		return CategoryGeneral
	}
	for _, row := range categoryKeywords {
		for _, kw := range row.keywords {
			if strings.Contains(needle, kw) {
				return row.cat
			}
		}
	}
	return CategoryGeneral
}

// diacriticFolder pliega diacríticos latinos a ASCII. Tabla stdlib-only
// porque el set de acentos en group-titles IPTV es acotado (ES/CA/FR/PT).
var diacriticFolder = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i",
	"ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u",
	"ñ", "n", "ç", "c",
	"Á", "a", "À", "a", "Ä", "a", "Â", "a", "Ã", "a", "Å", "a",
	"É", "e", "È", "e", "Ë", "e", "Ê", "e",
	"Í", "i", "Ì", "i", "Ï", "i", "Î", "i",
	"Ó", "o", "Ò", "o", "Ö", "o", "Ô", "o", "Õ", "o",
	"Ú", "u", "Ù", "u", "Ü", "u", "Û", "u",
	"Ñ", "n", "Ç", "c",
)

// normalize: minúsculas + strip diacríticos + wrapping en espacios
// para que las comprobaciones por substring matcheen tokens exactos.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	s = diacriticFolder.Replace(s)
	// Colapsa whitespace para que " F1 " matchee " f1 ".
	return " " + strings.Join(strings.Fields(s), " ") + " "
}
