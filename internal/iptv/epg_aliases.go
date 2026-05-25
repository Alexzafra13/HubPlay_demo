package iptv

// Tabla de aliases EPG → canal. Mapea alias normalizado a la forma
// canónica que convergen iptv-org y davidmuma/epg.pw.
//
// In-process (no tabla DB) porque es read-only. Reglas:
//   - Ambos lados normalizados: minúsculas, ASCII-folded, trimmed.
//   - No añadir aliases ambiguos (ej. "tv3" es TV3 en Catalunya
//     pero otra emisora en otros países).
//   - Solo entradas para mismatches observados en feeds reales.
var epgNameAliases = map[string]string{
	// Dígitos escritos ↔ numeral (cadenas TDT españolas).
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

	// Familia TVE — davidmuma tiende a unir las palabras.
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

	// Autonómicas / Catalunya (grupo TV3).
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

	// Movistar — davidmuma usa "laliga" junto, iptv-org "la liga" separado.
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
