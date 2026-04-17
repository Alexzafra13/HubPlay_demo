package iptv

import (
	"bufio"
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

// attrPattern matches key="value" pairs in EXTINF lines.
var attrPattern = regexp.MustCompile(`([a-zA-Z_-]+)="([^"]*)"`)

// utf8BOM is the byte sequence that UTF-8-encoded files often carry at the
// very start. Stripping it lets the header detector find "#EXTM3U".
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseM3U parses an M3U/M3U8 playlist from a reader and returns the channels.
//
// Deduplicates by TvgID: if a playlist lists the same TvgID twice, only the
// first occurrence is kept. Entries without a TvgID are never deduplicated
// (there is nothing to key on — fall back to accepting them all, which is
// the pre-fix behaviour). IPTV-org playlists are notorious for repeating the
// same channel under different group categories.
//
// Strips a leading UTF-8 BOM so the #EXTM3U header check still matches on
// files saved by Windows tools.
func ParseM3U(r io.Reader) ([]M3UChannel, error) {
	br := bufio.NewReader(r)
	// Peek at the first 3 bytes; discard a BOM if present.
	if prefix, err := br.Peek(3); err == nil && len(prefix) == 3 &&
		prefix[0] == utf8BOM[0] && prefix[1] == utf8BOM[1] && prefix[2] == utf8BOM[2] {
		_, _ = br.Discard(3)
	}

	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	var channels []M3UChannel
	seen := make(map[string]bool) // TvgIDs already kept
	var current *M3UChannel
	lineNum := 0
	headerSeen := false

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		// First non-empty line should be #EXTM3U
		if !headerSeen {
			if strings.HasPrefix(line, "#EXTM3U") {
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
				channels = append(channels, *current)
			}
			current = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading M3U at line %d: %w", lineNum, err)
	}

	return channels, nil
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
