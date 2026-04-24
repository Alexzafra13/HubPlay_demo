package iptv

// Curated alias table for EPG → channel matching. Maps a
// normalised alias (lowercased, diacritic-folded, whitespace-
// collapsed) to the canonical form that iptv-org playlists and
// the davidmuma / epg.pw community feeds converge on.
//
// Kept as in-process Go data rather than a DB table because the
// list is read-only from the operator's perspective (no admin
// UI to edit aliases, and no per-library tuning yet). When that
// need appears, promote this to a table with the same
// alias→canonical schema and layer per-library rows on top of
// this default set.
//
// Rules for adding entries:
//
//   - Both sides MUST be already normalised: lowercase, ASCII-
//     folded (see diacriticFolder), single-spaced, trimmed. The
//     matcher does not re-normalise before lookup.
//   - Do NOT add aliases that collide with a different channel's
//     canonical name. Example: "tv3" is Catalan TV3 in Spain but
//     a regional station elsewhere — leave ambiguous forms out
//     rather than guess.
//   - Add entries for mismatches observed against real XMLTV
//     feeds (davidmuma, epg.pw). Drive-by entries "just in case"
//     are discouraged — every alias is a potential false match
//     for some other channel we haven't seen yet.
//
// Applied on both sides of the match: the hub-channel index
// registers each variant and, where an alias exists, its
// canonical form; the matcher also alias-folds each programme
// display-name before lookup.
var epgNameAliases = map[string]string{
	// Spelled-out digits ↔ digit (Spanish free-to-air cadenas).
	"la uno":                 "la 1",
	"la dos":                 "la 2",
	"cuatro tv":              "cuatro",
	"antena tres":            "antena 3",
	"canal antena 3":         "antena 3",
	"la sexta":               "lasexta",
	"la 6":                   "lasexta",
	"la seis":                "lasexta",
	"tele cinco":             "telecinco",
	"telecinco hd":           "telecinco",
	"tele 5":                 "telecinco",

	// TVE family — davidmuma tends to spell these without spaces.
	"tve 1":                  "la 1",
	"tve1":                   "la 1",
	"tve 2":                  "la 2",
	"tve2":                   "la 2",
	"tve internacional":      "tve i",
	"24 horas":               "canal 24 horas",
	"canal 24h":              "canal 24 horas",
	"24h":                    "canal 24 horas",
	"clan tve":               "clan",
	"teledeporte":            "tdp",

	// Autonómicas / Catalunya (TV3 group).
	"tv 3":                   "tv3",
	"3 cat":                  "3cat",
	"3cat info":              "3catinfo",
	"3 cat info":             "3catinfo",
	"3cat 24":                "3/24",
	"3 24":                   "3/24",
	"canal super3":           "super3",
	"super 3":                "super3",
	"esport 3":               "esport3",
	"esports 3":              "esport3",

	// Galicia / Euskadi / Valencia / Andalucia.
	"tvg":                    "g",
	"tv galicia":             "g",
	"galicia tv":             "g",
	"etb 1":                  "etb1",
	"etb 2":                  "etb2",
	"eitb":                   "etb",
	"apunt":                  "a punt",
	"apunt tv":               "a punt",
	"canal sur tv":           "canal sur",
	"canal sur andalucia":    "canal sur",
	"canal sur 2":            "andalucia tv",

	// Movistar branded bundle (davidmuma uses "laliga" concatenated,
	// iptv-org uses "la liga" with space).
	"movistar laliga":        "movistar la liga",
	"movistar la liga 1":     "movistar la liga",
	"movistar liga campeones": "movistar liga de campeones",
	"movistar champions":     "movistar liga de campeones",
	"movistar deportes":      "movistar deportes 1",
	"m+ la liga":             "movistar la liga",
	"m+ liga de campeones":   "movistar liga de campeones",

	// DAZN family — consistent across feeds except hyphenation.
	"dazn laliga":            "dazn la liga",
	"dazn f 1":               "dazn f1",
	"dazn formula 1":         "dazn f1",
	"dazn motogp":            "dazn moto gp",

	// Common corporate brand normalisations.
	"be mad tv":              "bemad",
	"be mad":                 "bemad",
	"divinity tv":            "divinity",
	"energy tv":              "energy",
	"paramount network":      "paramount",
	"mega tv":                "mega",
	"ten tv":                 "ten",
	"neox tv":                "neox",
	"nova tv":                "nova",
	"atreseries tv":          "atreseries",
	"atres series":           "atreseries",

	// International news — match-friendly forms.
	"cnn international":      "cnn int",
	"cnn intl":               "cnn int",
	"bbc world news":         "bbc world",
	"france 24 en":           "france 24",
	"euronews en":            "euronews",
	"euronews english":       "euronews",
	"euronews spanish":       "euronews es",
	"euronews espanol":       "euronews es",
	"dw english":             "dw",
	"dw deutsch":             "dw de",
}

// canonicalize returns the alias-folded form of v if one exists,
// otherwise v unchanged. Both input and output are expected to be
// already normalised (see nameVariants).
func canonicalize(v string) string {
	if c, ok := epgNameAliases[v]; ok {
		return c
	}
	return v
}
