package torrentstream

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// torznabRSS mirrors the slice of a Torznab/Newznab RSS feed we consume.
// We only declare the fields we read; encoding/xml ignores the rest.
type torznabRSS struct {
	XMLName xml.Name      `xml:"rss"`
	Items   []torznabItem `xml:"channel>item"`
}

// torznabItem is one result row. `torznab:attr` and `newznab:attr` both
// have the local name "attr", so a single `xml:"attr"` field captures
// either namespace's extra-metadata elements.
type torznabItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Link      string `xml:"link"`
	Size      int64  `xml:"size"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	Attrs []torznabAttr `xml:"attr"`
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// attr returns the value of the first torznab/newznab attr with the given
// name (case-insensitive), or "".
func (it torznabItem) attr(name string) string {
	for _, a := range it.Attrs {
		if strings.EqualFold(a.Name, name) {
			return a.Value
		}
	}
	return ""
}

// parseTorznab decodes a Torznab/Newznab XML feed. Split out so it can be
// unit-tested with a fixed body (no network).
func parseTorznab(r io.Reader) ([]torznabItem, error) {
	var feed torznabRSS
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return nil, fmt.Errorf("torznab: decode feed: %w", err)
	}
	return feed.Items, nil
}

// normalizeItem converts a raw Torznab item into a SearchResult. It
// returns ok=false for items we can't make playable or identify (no
// magnet/infohash/link to key on).
func normalizeItem(it torznabItem, idx TorznabIndexer) (SearchResult, bool) {
	title := strings.TrimSpace(it.Title)

	// Size: prefer the <size> element, then the torznab:attr "size", then
	// the enclosure length.
	size := it.Size
	if size == 0 {
		size = parseInt64(it.attr("size"))
	}
	if size == 0 {
		size = it.Enclosure.Length
	}

	// Magnet: enclosure url, then <link>, then a torznab:attr.
	magnet := firstMagnet(it.Enclosure.URL, it.Link, it.attr("magneturl"), it.attr("magneturi"))

	infoHash := strings.ToLower(strings.TrimSpace(it.attr("infohash")))
	if infoHash == "" && magnet != "" {
		infoHash = infoHashFromMagnet(magnet)
	}
	// Build a magnet when the indexer only gave us a bare infohash.
	if magnet == "" && infoHash != "" {
		magnet = buildMagnet(infoHash, title, idx.Trackers)
	}

	// Stream source: a magnet plays directly; otherwise an http(s)
	// .torrent link is fetched (SSRF-guarded) by the engine.
	streamSrc := magnet
	if streamSrc == "" && isHTTPURL(it.Link) {
		streamSrc = it.Link
	}

	// Stable id: infohash is best; fall back to guid, then link.
	id := infoHash
	if id == "" {
		id = strings.TrimSpace(it.GUID)
	}
	if id == "" {
		id = strings.TrimSpace(it.Link)
	}
	if id == "" || streamSrc == "" {
		return SearchResult{}, false
	}

	// Parse the title once to derive the curation metadata.
	meta := parseTitle(title)

	return SearchResult{
		Identifier:   id,
		Title:        title,
		Provider:     idx.Name,
		TorrentURL:   streamSrc,
		SizeBytes:    size,
		Seeders:      parseInt(it.attr("seeders")),
		Quality:      meta.qualityBucket(),
		InfoHash:     infoHash,
		MagnetURI:    magnet,
		IMDbID:       normalizeIMDbAttr(it.attr("imdbid"), it.attr("imdb")),
		Resolution:   meta.Resolution,
		Codec:        meta.Codec,
		Languages:    meta.Languages,
		IsCam:        meta.IsCam,
		QualityScore: meta.QualityScore,
	}, true
}

// firstMagnet returns the first argument that is a magnet URI.
func firstMagnet(candidates ...string) string {
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c)), "magnet:") {
			return strings.TrimSpace(c)
		}
	}
	return ""
}

// isHTTPURL reports whether s is an http(s) URL.
func isHTTPURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// infoHashFromMagnet extracts the btih hash from a magnet's xt parameter,
// lowercased. Returns "" if absent.
func infoHashFromMagnet(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		const prefix = "urn:btih:"
		if strings.HasPrefix(strings.ToLower(xt), prefix) {
			return strings.ToLower(xt[len(prefix):])
		}
	}
	return ""
}

// buildMagnet assembles a magnet URI from an infohash, a display name and
// optional trackers — the fallback when an indexer returns only the hash.
func buildMagnet(infoHash, title string, trackers []string) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(infoHash)
	if title != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(title))
	}
	for _, tr := range trackers {
		if strings.TrimSpace(tr) == "" {
			continue
		}
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tr))
	}
	return b.String()
}

// extractQuality maps a release title to a coarse quality bucket. Thin
// wrapper over the metadata parser so there's a single source of truth for
// resolution detection.
func extractQuality(title string) string {
	return parseTitle(title).qualityBucket()
}

// qualityRank orders quality buckets for sorting (higher is better).
func qualityRank(q string) int {
	switch q {
	case "4K":
		return 4
	case "1080p":
		return 3
	case "720p":
		return 2
	case "480p":
		return 1
	default:
		return 0
	}
}

// normalizeIMDbAttr canonicalises an indexer-reported IMDb id to "ttNNN…".
// Torznab reports it under the "imdbid" attr (bare digits or with the tt
// prefix); some feeds use "imdb". Returns "" when absent/invalid.
func normalizeIMDbAttr(candidates ...string) string {
	for _, c := range candidates {
		s := strings.TrimSpace(c)
		if s == "" {
			continue
		}
		s = strings.TrimPrefix(strings.ToLower(s), "tt")
		if s == "" || strings.Trim(s, "0123456789") != "" {
			continue // not all digits
		}
		return "tt" + s
	}
	return ""
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}
