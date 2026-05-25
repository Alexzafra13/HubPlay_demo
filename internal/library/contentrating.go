// Helpers de ranking + filtrado de content-rating.
//
// Los ratings vienen de las certificaciones de TMDb: MPAA (movies) y
// US TV Parental Guidelines. Ambos se mapean a un ordinal comparable
// para responder "este item supera el cap del perfil?".
//
// Ratings desconocidos reciben el tier más alto — mejor sobre-restringir
// que filtrar contenido inesperado. Localización futura (BBFC, FSK, ICAA)
// extiende la tabla sin tocar los callsites.

package library

// ratingRank asigna un tier numérico a cada certificación conocida.
// Menor = audiencia más joven. MPAA y TV comparten tabla porque TMDb
// los mezcla y un perfil "PG-13" debe ver también contenido "TV-14".
var ratingRank = map[string]int{
	// MPAA (movies)
	"G":     1,
	"PG":    2,
	"PG-13": 3,
	"R":     4,
	"NC-17": 5,

	// US TV
	"TV-Y":  1,
	"TV-Y7": 2,
	"TV-G":  1,
	"TV-PG": 2,
	"TV-14": 3,
	"TV-MA": 4,
}

const ratingRankUnknown = 5 // pesimista: labels desconocidos = adulto

// ContentRatingRank devuelve el tier comparable de un rating.
// Rating vacío ("unrated") = rank 0: solo pasa en perfiles sin cap.
func ContentRatingRank(rating string) int {
	if rating == "" {
		return 0
	}
	if r, ok := ratingRank[rating]; ok {
		return r
	}
	return ratingRankUnknown
}

// AllowedRating indica si un item con `itemRating` está dentro del
// cap del perfil. Cap vacío = sin restricción. itemRating vacío
// contra cap no-vacío se deniega (contenido no clasificado puede
// no ser apto para un perfil infantil).
func AllowedRating(itemRating, capRating string) bool {
	if capRating == "" {
		return true
	}
	cap, ok := ratingRank[capRating]
	if !ok {
		// Cap desconocido → fail-open para no bloquear todo.
		return true
	}
	if itemRating == "" {
		return false
	}
	return ContentRatingRank(itemRating) <= cap
}

// AllowedRatingsAtMost devuelve los ratings que pasan el cap.
// Usado por filtros SQL que necesitan `IN (...)` explícito
// (SQLite sin CGO no permite registrar callbacks Go).
func AllowedRatingsAtMost(capRating string) []string {
	if capRating == "" {
		return nil
	}
	cap, ok := ratingRank[capRating]
	if !ok {
		return nil // fail-open
	}
	out := make([]string, 0, len(ratingRank))
	for rating, r := range ratingRank {
		if r <= cap {
			out = append(out, rating)
		}
	}
	return out
}
