package iptv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// allowedStreamSchemes defines the set of URL schemes considered safe for stream URLs.
var allowedStreamSchemes = map[string]bool{
	"http":  true,
	"https": true,
	"rtmp":  true,
	"rtsp":  true,
	"udp":   true,
	"rtp":   true,
}

// isValidStreamURL checks whether a stream URL uses a safe, allowed scheme.
// It rejects dangerous schemes like file://, javascript:, data:, etc.
func isValidStreamURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return allowedStreamSchemes[scheme]
}

// M3UChannel represents a parsed channel entry from an M3U playlist.
type M3UChannel struct {
	Name      string
	Number    int
	GroupName string
	LogoURL   string
	StreamURL string
	TvgID     string
	TvgName   string
	Language  string
	Country   string
}

// Playlist is the full result of parsing an M3U file: the channel list plus
// any playlist-level metadata we care about (currently just the XMLTV EPG
// URL advertised in the #EXTM3U header).
type Playlist struct {
	Channels []M3UChannel
	// EPGURL is the URL advertised by the playlist for fetching an XMLTV
	// electronic programme guide. Empty if the playlist didn't publish one.
	// Populated from `url-tvg`, `x-tvg-url`, or `tvg-url` on the #EXTM3U line.
	EPGURL string
}

// attrPattern matches key="value" pairs in EXTINF lines.
var attrPattern = regexp.MustCompile(`([a-zA-Z_-]+)="([^"]*)"`)

// utf8BOM is the byte sequence that UTF-8-encoded files often carry at the
// very start. Stripping it lets the header detector find "#EXTM3U".
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseM3U parses an M3U/M3U8 playlist from a reader and returns the parsed
// playlist: channel list plus any playlist-level metadata (EPG URL).
//
// Deduplicates by TvgID: if a playlist lists the same TvgID twice, only the
// first occurrence is kept. Entries without a TvgID are never deduplicated
// (there is nothing to key on — fall back to accepting them all, which is
// the pre-fix behaviour). IPTV-org playlists are notorious for repeating the
// same channel under different group categories.
//
// Strips a leading UTF-8 BOM so the #EXTM3U header check still matches on
// files saved by Windows tools.
//
// This is a thin wrapper over ParseM3UStream that accumulates every
// emitted channel into a slice. Suitable for small playlists and tests;
// for large feeds (Xtream-Codes M3U_PLUS catalogs with VOD can run into
// hundreds of thousands of entries) prefer ParseM3UStream so the caller
// can filter or persist incrementally without holding the whole list
// in memory.
func ParseM3U(r io.Reader) (*Playlist, error) {
	playlist := &Playlist{}
	epgURL, _, err := ParseM3UStream(r, func(ch M3UChannel) error {
		playlist.Channels = append(playlist.Channels, ch)
		return nil
	})
	if err != nil {
		return nil, err
	}
	playlist.EPGURL = epgURL
	return playlist, nil
}

// ParseM3UStream parses an M3U/M3U8 stream and invokes onChannel for
// every fully-formed channel entry (EXTINF + URL pair) it sees. The
// callback runs synchronously on the parser goroutine — keep it
// non-blocking, or buffer through a goroutine if expensive work is
// needed.
//
// Returns the URL advertised on the #EXTM3U header (url-tvg / x-tvg-url
// / tvg-url, may be empty), the number of source lines processed, and
// any scanner error. A non-nil error from onChannel aborts parsing and
// is returned wrapped — useful so callers can short-circuit on context
// cancellation or DB write failure.
//
// Memory profile: O(1) — no slice accumulation. The largest live
// allocation is the bufio.Scanner buffer (10MB max line, see below).
//
// Tolerance: TvgID dedup, BOM skipping, and #EXTM3U-optional behaviour
// are preserved verbatim from ParseM3U so swapping the implementation
// is a no-op for existing callers.
func ParseM3UStream(r io.Reader, onChannel func(M3UChannel) error) (epgURL string, lineNum int, err error) {
	br := bufio.NewReader(r)
	// Peek at the first 3 bytes; discard a BOM if present.
	if prefix, perr := br.Peek(3); perr == nil && len(prefix) == 3 &&
		prefix[0] == utf8BOM[0] && prefix[1] == utf8BOM[1] && prefix[2] == utf8BOM[2] {
		_, _ = br.Discard(3)
	}

	// Sniff first non-whitespace byte. If it is "<" we are looking at
	// HTML / XML / SOAP — almost certainly an error page from the IPTV
	// provider (account suspended, IP blocked, rate-limit, captive
	// portal). Without this guard the scanner would happily walk the
	// whole HTML and report "0 channels imported", which makes the
	// real failure invisible to the operator. Returning ErrNotM3U lets
	// the service layer translate this into a useful UI message.
	if first, perr := peekFirstNonSpace(br); perr == nil && first == '<' {
		return "", 0, ErrNotM3U
	}

	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	seen := make(map[string]bool) // TvgIDs already kept
	var current *M3UChannel
	headerSeen := false

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		// First non-empty line should be #EXTM3U. iptv-org and many other
		// public feeds advertise their XMLTV URL right here as
		// `url-tvg="…"` (and some tools use `x-tvg-url` or `tvg-url`).
		if !headerSeen {
			if strings.HasPrefix(line, "#EXTM3U") {
				epgURL = parseHeaderEPGURL(line)
				headerSeen = true
				continue
			}
			// Be lenient — allow files without #EXTM3U header
			headerSeen = true
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			ch := parseExtInf(line)
			current = &ch
			continue
		}

		// Skip other directives
		if strings.HasPrefix(line, "#") {
			continue
		}

		// This is a URL line
		if current != nil {
			current.StreamURL = line
			if isValidStreamURL(line) {
				if current.TvgID != "" {
					if seen[current.TvgID] {
						current = nil
						continue
					}
					seen[current.TvgID] = true
				}
				if cbErr := onChannel(*current); cbErr != nil {
					return epgURL, lineNum, fmt.Errorf("onChannel at line %d: %w", lineNum, cbErr)
				}
			}
			current = nil
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return epgURL, lineNum, fmt.Errorf("reading M3U at line %d: %w", lineNum, scanErr)
	}

	return epgURL, lineNum, nil
}

// vodGroupTokens are case-insensitive substrings that, when present in
// an M3U entry's group-title, mark it as VOD (movies / series / box
// office) rather than a live channel. Tuned for Xtream-Codes feeds
// whose M3U_PLUS export bundles live + VOD into a single playlist.
var vodGroupTokens = []string{
	"vod", "movies", "movie", "películas", "peliculas", "pelis",
	"cinema", "films", "series", "shows", "tv shows", "kids vod",
	"adult", "adultos", "xxx",
}

// IsVODChannel returns true when the entry looks like Video-On-Demand
// (a movie or a series episode) rather than a live channel.
//
// Heuristics, in priority order:
//  1. Stream URL path contains the Xtream-Codes VOD path segments
//     (`/movie/` or `/series/`). This is the strongest signal — Xtream
//     servers serve those endpoints exclusively for VOD.
//  2. Group-title contains a known VOD token (movies, series, vod,
//     películas, etc., case-insensitive).
//
// We deliberately err on the side of *under-filtering* — a false
// negative (a movie classified as live) is harmless beyond list
// clutter, while a false positive (a real channel skipped) loses
// content. If a feed uses a non-obvious group-title naming scheme,
// the user can still see the entry; conversely a Xtream URL is
// near-impossible to confuse.
func IsVODChannel(ch M3UChannel) bool {
	if u := strings.ToLower(ch.StreamURL); u != "" {
		if strings.Contains(u, "/movie/") || strings.Contains(u, "/series/") {
			return true
		}
	}
	if g := strings.ToLower(ch.GroupName); g != "" {
		for _, token := range vodGroupTokens {
			if strings.Contains(g, token) {
				return true
			}
		}
	}
	return false
}

// parseHeaderEPGURL extracts the XMLTV URL from the #EXTM3U header line.
// Checks `url-tvg`, `x-tvg-url`, and `tvg-url` in that order — different
// tools use different spellings for the same concept. A header may advertise
// a comma-separated list of XMLTV URLs; we take the first entry since our
// storage model keeps a single URL per library.
func parseHeaderEPGURL(headerLine string) string {
	matches := attrPattern.FindAllStringSubmatch(headerLine, -1)
	preferred := []string{"url-tvg", "x-tvg-url", "tvg-url"}
	found := make(map[string]string, len(preferred))
	for _, m := range matches {
		key := strings.ToLower(m[1])
		found[key] = m[2]
	}
	for _, key := range preferred {
		if val, ok := found[key]; ok && val != "" {
			// Some feeds ship a comma-separated list of XMLTV URLs; we keep
			// the first one since we store a single URL per library.
			if idx := strings.Index(val, ","); idx != -1 {
				val = val[:idx]
			}
			val = strings.TrimSpace(val)
			if isValidStreamURL(val) {
				return val
			}
		}
	}
	return ""
}

// parseExtInf parses an #EXTINF line into a channel.
// Format: #EXTINF:-1 tvg-id="..." tvg-name="..." tvg-logo="..." group-title="...",Channel Name
func parseExtInf(line string) M3UChannel {
	ch := M3UChannel{}

	// Extract attributes
	matches := attrPattern.FindAllStringSubmatch(line, -1)
	for _, m := range matches {
		key := strings.ToLower(m[1])
		val := m[2]

		switch key {
		case "tvg-id":
			ch.TvgID = val
		case "tvg-name":
			ch.TvgName = val
		case "tvg-logo":
			ch.LogoURL = val
		case "group-title":
			ch.GroupName = val
		case "tvg-language":
			ch.Language = val
		case "tvg-country":
			ch.Country = val
		case "channel-number", "tvg-chno":
			ch.Number, _ = strconv.Atoi(val)
		}
	}

	// Extract channel name (after the last comma)
	if idx := strings.LastIndex(line, ","); idx != -1 {
		ch.Name = strings.TrimSpace(line[idx+1:])
	}

	// Fallback: use tvg-name if name is empty
	if ch.Name == "" {
		ch.Name = ch.TvgName
	}

	return ch
}

// ErrNotM3U signals that the body the parser was handed is not a
// playlist at all — typically an HTML error page from the IPTV
// provider (account suspended, IP blocked by court order in ES, captive
// portal, rate-limit, etc.). The service layer maps it to a friendly
// admin-facing message rather than the misleading "0 channels".
var ErrNotM3U = errors.New("response is not an M3U playlist (got HTML/other)")

// peekFirstNonSpace reads ahead in the buffered reader until it finds a
// non-whitespace byte and returns it without consuming. We bound the
// scan to avoid pathological inputs (mostly-whitespace headers); 4 KB
// is more than enough for any real-world preamble.
func peekFirstNonSpace(br *bufio.Reader) (byte, error) {
	const maxScan = 4096
	prefix, err := br.Peek(maxScan)
	if err != nil && err != io.EOF && len(prefix) == 0 {
		return 0, err
	}
	for _, b := range prefix {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b, nil
		}
	}
	return 0, io.EOF
}
