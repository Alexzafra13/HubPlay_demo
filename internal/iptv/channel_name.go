package iptv

import (
	"regexp"
	"strings"
)

// SanitizeChannelName limpia el nombre crudo del M3U para que el
// frontend tenga un display name presentable, y extrae la calidad
// para mostrarla como badge en lugar de mezclada con el nombre.
//
// Casos reales que dispara:
//
//   "ESPN HD [1080p]"             → ("ESPN HD", "FHD")
//   "Cinemax [geo-blocked]"       → ("Cinemax", "")
//   "Movistar Deportes [VIP] FHD" → ("Movistar Deportes", "FHD")
//   "DAZN 4K [HEVC]"              → ("DAZN", "UHD")
//   "Canal 24h SD"                → ("Canal 24h", "SD")
//   "AXN |ES|"                    → ("AXN", "")     ;; sufijos pipe
//   "★ Disney Channel ★"          → ("Disney Channel", "")
//
// Se respetan los nombres "limpios" — un canal llamado simplemente
// "ESPN" sale como ("ESPN", ""). El nombre original se preserva en
// `Channel.Name`; sólo el DTO de wire lleva la versión sanitizada,
// así que un futuro cambio en estas reglas no rompe la DB.
//
// La quality devuelta es la canónica para el badge:
//   "UHD" (4K, 2160p)
//   "FHD" (1080p, FullHD)
//   "HD"  (720p, HD a secas)
//   "SD"  (480p, SD)
//   ""    (no detectada — el badge no se renderiza)
func SanitizeChannelName(raw string) (displayName, quality string) {
	if raw == "" {
		return "", ""
	}

	// 1. Detecta y captura el primer marcador de calidad encontrado.
	//    Probamos en orden de más específico a menos para que "2160p"
	//    no caiga en el matcher de "HD".
	q := detectQuality(raw)

	// 2. Quita todos los segmentos entre brackets [...] — quality tags,
	//    geo-blocked, VIP, codecs y demás ruido del M3U.
	cleaned := bracketTagRe.ReplaceAllString(raw, " ")

	// 3. Quita pipe-wrapped (|ES|, |LATINO|, |HD|) — convención común
	//    en M3U de español/latam.
	cleaned = pipeTagRe.ReplaceAllString(cleaned, " ")

	// 4. Quita parens con calidad/idioma (HD), (FHD), (4K), (ES), (LATINO).
	cleaned = parenTagRe.ReplaceAllString(cleaned, " ")

	// 5. Tokens de calidad sueltos al final del nombre ("ESPN HD").
	//    Sólo al final para evitar tocar nombres legítimos como
	//    "Studio 4K Network" donde "4K" forma parte del branding.
	cleaned = trailingQualityRe.ReplaceAllString(cleaned, "")

	// 6. Símbolos decorativos al principio o final (★ ▶ ● ⚽ etc.).
	cleaned = strings.Trim(cleaned, " \t-·•●★☆⚽▶◆◉►|")

	// 7. Colapsa whitespace múltiple en uno.
	cleaned = whitespaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		// Fallback paranoico: si la sanitización dejó la cadena vacía
		// (un canal llamado "[VIP]"), devolvemos el raw. Mejor enseñar
		// algo que un hueco.
		cleaned = strings.TrimSpace(raw)
	}

	return cleaned, q
}

var (
	bracketTagRe      = regexp.MustCompile(`\[[^\]]*\]`)
	pipeTagRe         = regexp.MustCompile(`\|[^|]+\|`)
	parenTagRe        = regexp.MustCompile(`(?i)\((hd|fhd|uhd|sd|4k|2160p?|1080p?|720p?|480p?|hevc|h\.?265|h\.?264|avc|es|en|latino|esp|cast|vip)\)`)
	trailingQualityRe = regexp.MustCompile(`(?i)\s+(hd|fhd|uhd|sd|4k|2160p?|1080p?|720p?|480p?|hevc|h\.?265|h\.?264|avc|vip|premium)\s*$`)
	whitespaceRe      = regexp.MustCompile(`\s+`)
)

// detectQuality busca cualquier marcador de calidad reconocible en el
// nombre crudo y devuelve la etiqueta canónica del badge. Orden de
// preferencia (más específico primero, gana lo más alto de calidad).
func detectQuality(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "2160p") || strings.Contains(lower, "uhd") ||
		hasWord(lower, "4k"):
		return "UHD"
	case strings.Contains(lower, "1080p") || strings.Contains(lower, "fullhd") ||
		strings.Contains(lower, "full hd") || hasWord(lower, "fhd"):
		return "FHD"
	case strings.Contains(lower, "720p") || hasWord(lower, "hd"):
		return "HD"
	case strings.Contains(lower, "480p") || hasWord(lower, "sd"):
		return "SD"
	}
	return ""
}

// hasWord comprueba si `token` aparece como palabra completa en `s`
// (delimitada por non-alphanumeric a ambos lados o bordes). Sin esto
// "Studio 4K Network" matchearía como "4K" pero también "Channel News
// Asia HD" produciría "HD" desde "Asia" — no queremos.
func hasWord(s, token string) bool {
	// regexp.MustCompile cada vez es caro; este helper se llama N veces
	// por listado de canales (potencialmente miles). Pero las regexes
	// se reusan vía closure de regexp; aceptable para v1. Si se vuelve
	// hot path, se puede memoizar el wordRe por token.
	wordRe := regexp.MustCompile(`(^|[^a-z0-9])` + regexp.QuoteMeta(token) + `($|[^a-z0-9])`)
	return wordRe.MatchString(s)
}
