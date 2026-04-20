package iptv

import (
	"strings"
)

// Category is a canonical, UI-stable category identifier.
// M3U `group-title` is free-form and multilingual; Category collapses it to
// a fixed, translatable set the frontend can key off for filtering and icons.
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

// AllCategories lists every canonical category in UI display order.
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

// categoryKeyword is a single matcher in the normalizer pipeline.
// Order in categoryKeywords matters: earlier entries win on ambiguous groups
// (e.g. "sports news" is Sports, not News).
type categoryKeyword struct {
	cat      Category
	keywords []string
}

// categoryKeywords holds the priority-ordered keyword table. Keywords are
// lowercased, accent-stripped substrings — see Canonical for normalization.
var categoryKeywords = []categoryKeyword{
	// Adult first: we never want a racy group misclassified as entertainment.
	{CategoryAdult, []string{"adult", "adulto", "xxx", "erotic", "erotica", "porn", "hustler", "playboy"}},

	// Sports before news — "sports news" should be Sports.
	{CategorySports, []string{
		"sport", "deport", "futbol", "football", "soccer", "laliga", "la liga",
		"champions", "uefa", "fifa", "nba", "nfl", "mlb", "nhl", "ufc", "boxeo", "boxing",
		"dazn", "eurosport", "gol", "movistar deportes", "movistar laliga",
		"f1", "motogp", "formula", "formula1", "formula 1", "tennis", "tenis", "golf",
		"teledeporte", "real madrid tv", "barca tv", "bein sport",
	}},

	// Kids before entertainment — "cartoon entertainment" is Kids.
	{CategoryKids, []string{
		"kids", "infantil", "ninos", "cartoon", "disney", "nick", "nickelodeon",
		"boomerang", "baby tv", "babytv", "clan", "pocoyo", "junior", "cbeebies",
	}},

	// Documentaries before culture — "history docs" is Documentaries.
	{CategoryDocumentaries, []string{
		"documental", "documentary", "doc ", " doc", "nat geo", "natgeo",
		"national geographic", "discovery", "history", "historia", "animal planet",
		"love nature", "dmax", "odisea",
	}},

	{CategoryNews, []string{
		// "informati" covers Spanish (informativo/informativos) AND Catalan
		// (informatiu/informatius). "telediari" covers Spanish "telediario"
		// and Catalan "telediari". "notici" covers noticia/noticias/notícies.
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

	// International goes last among real categories: most group-titles that include
	// "international" also include a stronger signal (news, sports). If nothing
	// else matched, fall through to this.
	{CategoryInternational, []string{
		"international", "internacional", "world", "mundo", "europa", "europe",
	}},
}

// Canonical maps a raw M3U `group-title` to a canonical Category.
// It is case-insensitive, accent-insensitive, and returns CategoryGeneral
// for empty or unknown groups. The mapping is deterministic and safe to
// cache or use in tests.
//
// The intended use is at the edge (handler/DTO), not inside the scanner —
// the raw GroupName is preserved in M3UChannel for operators who want to
// expose their provider's native grouping.
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

// diacriticFolder maps common Latin diacritics to their ASCII form so
// "Películas" and "peliculas" hit the same keywords. Kept as a small,
// stdlib-only table because the set of accented characters seen in IPTV
// group-titles is narrow (Spanish / Catalan / French / Portuguese).
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

// normalize lowercases, strips diacritics, and wraps in spaces so substring
// checks against " keyword " match exact tokens as well as phrases.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	s = diacriticFolder.Replace(s)
	// Collapse whitespace so " F1 " matches " f1 " after trim.
	return " " + strings.Join(strings.Fields(s), " ") + " "
}
