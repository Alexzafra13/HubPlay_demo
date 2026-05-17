package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeMatch: lo que el filename + dir parent nos dicen sin tocar
// metadata. SeriesName es el dir on-disk (TMDb dará el canónico después).
//
// OK==false: el path no parece episode (lib root plano, estructura rara) —
// el caller debe caer al "single item sin parents" para no perder el fichero.
type EpisodeMatch struct {
	SeriesName    string
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	OK            bool
}

// Patrones comunes (Plex/Jellyfin/Kodi):
//   "S01E05" / "s01e05" / "S1E5" / "1x05" / "01x05" / "S01.E05"
// Captura $1=season, $2=episode. Anclado a boundary no-dígito para no
// tragarse parte de un año ("2024 1x05" funciona).
var epPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[^a-z\d])s(\d{1,3})[\.\s_-]?e(\d{1,3})(?:[^a-z\d]|$)`),
	regexp.MustCompile(`(?:^|\D)(\d{1,3})x(\d{1,3})(?:\D|$)`),
}

// Patrones de season-only para el dir parent (caso "Season 03/01.mkv"
// donde el filename no lleva SxxExx).
var seasonDirPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:season|temporada|saison|staffel)[\.\s_-]*(\d{1,3})$`),
	regexp.MustCompile(`(?i)^s(\d{1,3})$`),
}

// titleStripChars: separadores que recortamos del título extraído.
const titleStripChars = " .-_[]()"

// ParseEpisode: extrae coordenadas de episode del path.
//
// libraryRoot evita que un fichero en la raíz pretenda pertenecer a una
// series inexistente. Layout esperado (convención Plex/Jellyfin):
//
//	<libRoot>/<Series Name>/<Season N>/<file>.ext
//	<libRoot>/<Series Name>/<file>.ext            (raro pero soportado)
//
// En el segundo, season sale del SxxExx en el filename si está; si no,
// default 1.
func ParseEpisode(libraryRoot, filePath string) EpisodeMatch {
	rel, err := filepath.Rel(libraryRoot, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return EpisodeMatch{OK: false}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		// fichero en la raíz — sin dir de series.
		return EpisodeMatch{OK: false}
	}

	// parts[0] = hijo directo del lib root → dir de la series; lo más
	// profundo son season(s).
	seriesDir := parts[0]
	fileName := parts[len(parts)-1]
	// Segmentos entre series y file = candidatos a season. Típicamente 1.
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
		// SxxExx en filename. Preferimos season-from-dir si existe
		// (desambigua shows re-numerados tipo Doctor Who 2005); si no,
		// el de filename.
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
		// Sin SxxExx en filename — probar a sacar sólo el número de episode
		// (p.ej. "01.mkv" dentro de Season 03).
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

// extractEpisodeFromFilename: prueba cada patrón. Devuelve season, episode,
// intento de título (lo que viene tras el token SxxExx) y flag.
func extractEpisodeFromFilename(name string) (season, episode int, title string, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for _, re := range epPatterns {
		m := re.FindStringSubmatchIndex(base)
		if m == nil {
			continue
		}
		s, _ := strconv.Atoi(base[m[2]:m[3]])
		e, _ := strconv.Atoi(base[m[4]:m[5]])
		// Match en m[0]..m[1]; lo posterior es candidato a título.
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
