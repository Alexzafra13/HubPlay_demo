package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeMatch es lo que el nombre del fichero y la carpeta padre nos
// pueden decir sin metadatos. SeriesName es el nombre tal y como está
// en disco; el canónico lo dará TMDb después.
//
// Si OK es false, el path no parece un episodio (estructura rara o
// fichero suelto) y el que llama debe guardar el fichero como elemento
// solitario para no perderlo.
type EpisodeMatch struct {
	SeriesName    string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	OK            bool
}

// Patrones que entienden Plex, Jellyfin y Kodi: "S01E05", "s01e05",
// "S1E5", "1x05", "01x05", "S01.E05". Captura temporada y episodio.
// Anclado para que un año no se cuele (p.ej. "2024 1x05" funciona).
var epPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^a-z\d])s(\d{1,3})[\.\s_-]?e(\d{1,3})(?:[^a-z\d]|$)`),
	regexp.MustCompile(`(?:^|\D)(\d{1,3})x(\d{1,3})(?:\D|$)`),
}

// Patrones para reconocer el número de temporada en la carpeta padre,
// cuando el fichero por sí solo no lo lleva (ej. "Season 03/01.mkv").
var seasonDirPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:season|temporada|saison|staffel)[\.\s_-]*(\d{1,3})$`),
	regexp.MustCompile(`(?i)^s(\d{1,3})$`),
}

// Caracteres que recortamos a los lados del título.
const titleStripChars = " .-_[]()"

// ParseEpisode saca temporada y episodio de la ruta del fichero. El
// layout esperado es el de Plex/Jellyfin:
//
//	<raíz>/<Nombre serie>/<Temporada N>/<fichero>.ext
//	<raíz>/<Nombre serie>/<fichero>.ext            (raro pero válido)
//
// libraryRoot impide que un fichero suelto en la raíz pretenda
// pertenecer a una serie inexistente.
func ParseEpisode(libraryRoot, filePath string) EpisodeMatch {
	rel, err := filepath.Rel(libraryRoot, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return EpisodeMatch{OK: false}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		// Fichero suelto en la raíz, sin carpeta de serie.
		return EpisodeMatch{OK: false}
	}

	// La primera carpeta es la serie; lo que haya entre medias es la
	// temporada (lo normal es que haya sólo una).
	seriesDir := parts[0]
	fileName := parts[len(parts)-1]
	var seasonDirs []string
	if len(parts) >= 3 {
		seasonDirs = parts[1 : len(parts)-1]
	}

	se, ee, titleFromFile, hasSE := extractEpisodeFromFilename(fileName)
	hasSeasonDir := false
	seasonFromDir := 0
	for i := len(seasonDirs) - 1; i >= 0; i-- {
		if n, ok := parseSeasonDir(seasonDirs[i]); ok {
			seasonFromDir = n
			hasSeasonDir = true
			break
		}
	}

	switch {
	case hasSE:
		// El fichero ya lleva SxxExx. Si además hay una carpeta de
		// temporada, preferimos ese número (desambigua series renumeradas
		// como Doctor Who 2005).
		if hasSeasonDir {
			se = seasonFromDir
		}
		return EpisodeMatch{
			SeriesName:    seriesDir,
			SeasonNumber:  se,
			EpisodeNumber: ee,
			EpisodeTitle:  titleFromFile,
			OK:            true,
		}
	case hasSeasonDir:
		// El fichero no lleva SxxExx; probamos a sacar sólo el número de
		// episodio (ej. "01.mkv" dentro de la carpeta Season 03).
		if epOnly, ok := extractTrailingEpisodeNumber(fileName); ok {
			return EpisodeMatch{
				SeriesName:    seriesDir,
				SeasonNumber:  seasonFromDir,
				EpisodeNumber: epOnly,
				EpisodeTitle:  cleanTitle(strings.TrimSuffix(fileName, filepath.Ext(fileName))),
				OK:            true,
			}
		}
	}

	return EpisodeMatch{OK: false}
}

// extractEpisodeFromFilename prueba cada patrón y devuelve temporada,
// episodio y un intento de título (lo que vaya detrás del SxxExx).
func extractEpisodeFromFilename(name string) (season, episode int, title string, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, re := range epPatterns {
		m := re.FindStringSubmatchIndex(base)
		if m == nil {
			continue
		}
		s, _ := strconv.Atoi(base[m[2]:m[3]])
		e, _ := strconv.Atoi(base[m[4]:m[5]])
		// Lo que viene después del SxxExx puede ser el título.
		tail := ""
		if m[1] < len(base) {
			tail = cleanTitle(base[m[1]:])
		}
		return s, e, tail, true
	}
	return 0, 0, "", false
}

// extractTrailingEpisodeNumber: captura ficheros tipo "01.mkv" donde sólo
// está el número de episode (la season viene del dir parent).
var trailingNumRE = regexp.MustCompile(`^(\d{1,3})(?:[\.\s_-].*)?$`)

func extractTrailingEpisodeNumber(name string) (int, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	m := trailingNumRE.FindStringSubmatch(base)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// parseSeasonDir: número de season desde el nombre del dir. false si no
// matchea ningún patrón conocido (el caller sigue probando arriba).
func parseSeasonDir(name string) (int, bool) {
	trimmed := strings.TrimSpace(name)
	for _, re := range seasonDirPatterns {
		m := re.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// cleanTitle: trim separadores y sustituye . / _ por espacios (los release
// packagers usan puntos: "S01E05.Halloween.Party"). "" es válido si tras el
// trim no queda nada.
func cleanTitle(raw string) string {
	out := strings.TrimLeft(raw, titleStripChars)
	out = strings.TrimRight(out, titleStripChars)
	out = strings.ReplaceAll(out, ".", " ")
	out = strings.ReplaceAll(out, "_", " ")
	out = strings.Join(strings.Fields(out), " ")
	return out
}
