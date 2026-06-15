package torrentstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The in-app search queries Internet Archive's public advancedsearch API
// ONLY. It is a free-text search (the user can type anything) over a
// legal catalogue: millions of public-domain / Creative Commons items,
// many of which Archive.org publishes official torrents for. This is the
// deliberate, legitimate alternative to scraping third-party torrent
// indexers — same "type anything, get streamable results" UX, legal source.
const (
	archiveSearchEndpoint = "https://archive.org/advancedsearch.php"
	// downloadBase + "/<id>/<id>_archive.torrent" is Archive.org's stable
	// convention for the per-item torrent.
	archiveDownloadBase = "https://archive.org/download"
	userAgent           = "HubPlay/0.1 (+https://github.com/Alexzafra13/HubPlay_demo)"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// SearchResult is one streamable item produced by a SearchProvider (the
// Internet Archive catalogue) or by the Torznab aggregator. TorrentURL
// (magnet or http(s) .torrent) is what the streaming engine plays.
//
// The richer fields (SizeBytes, Seeders, Quality, InfoHash, MagnetURI)
// are populated by the Torznab path — they map onto the frontend's
// StreamSource view. The Archive provider leaves them zero, so they are
// all `omitempty`: a result is identified by Identifier/Title/TorrentURL
// at minimum, and the extra metadata rides along when a source has it.
type SearchResult struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Mediatype  string `json:"mediatype,omitempty"`
	Year       string `json:"year,omitempty"`
	TorrentURL string `json:"torrent_url"`
	// Provider is the name of the source that produced this result, so a
	// multi-source UI can group / label results.
	Provider string `json:"provider,omitempty"`

	// SizeBytes is the file size in bytes (the UI formats it to GB). 0 when
	// the source didn't report a size.
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// Seeders is the availability ranking signal from the indexer.
	Seeders int `json:"seeders,omitempty"`
	// Quality is the coarse bucket parsed from the title (4K / 1080p /
	// 720p / 480p / HDTV) — kept for the existing UI chip.
	Quality string `json:"quality,omitempty"`
	// InfoHash is the BitTorrent infohash, used both as a stable id and to
	// build a magnet when the indexer only returned the hash.
	InfoHash string `json:"infohash,omitempty"`
	// MagnetURI is the full magnet link (supplied by the indexer or built
	// from InfoHash + trackers).
	MagnetURI string `json:"magnet_uri,omitempty"`

	// ── Curation metadata (parsed from the title; see metadata_parser.go) ──

	// Resolution is the raw resolution token (2160p / 1080p / 720p / 480p).
	Resolution string `json:"resolution,omitempty"`
	// Codec is the normalised video codec (HEVC / H.264 / AV1 / XviD).
	Codec string `json:"codec,omitempty"`
	// Languages are the audio/sub language tags detected in the title
	// (Spanish, Latino, Dual, Multi, VOST, English, French). Defaults to
	// ["Original"] when none are present.
	Languages []string `json:"languages,omitempty"`
	// IsCam flags low-quality source releases (CAM / TS / Screener / R5…)
	// so the filter engine can drop them.
	IsCam bool `json:"is_cam,omitempty"`
	// QualityScore is a computed ranking score (resolution + source + codec
	// + HDR/DV, minus a heavy penalty for cams) used for sorting.
	QualityScore int `json:"quality_score,omitempty"`
}

// archiveResponse mirrors the slice of the Archive.org JSON we consume.
type archiveResponse struct {
	Response struct {
		NumFound int `json:"numFound"`
		Docs     []struct {
			Identifier string          `json:"identifier"`
			Title      string          `json:"title"`
			Mediatype  string          `json:"mediatype"`
			Year       json.RawMessage `json:"year"` // IA returns string OR number
		} `json:"docs"`
	} `json:"response"`
}

// SearchArchive runs a free-text search against Internet Archive and
// returns up to `limit` results, each carrying the URL of its official
// torrent. It restricts to media types that make sense to stream
// (movies / audio / etv) but otherwise passes the query through verbatim.
func SearchArchive(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	// Constrain to streamable media types; the user's free-text query is
	// AND-ed with this so "type anything" still works within the catalogue.
	q := fmt.Sprintf("(%s) AND (mediatype:(movies) OR mediatype:(audio) OR mediatype:(etree))", query)

	params := url.Values{}
	params.Set("q", q)
	params.Add("fl[]", "identifier")
	params.Add("fl[]", "title")
	params.Add("fl[]", "mediatype")
	params.Add("fl[]", "year")
	params.Set("rows", fmt.Sprintf("%d", limit))
	params.Set("page", "1")
	params.Set("output", "json")
	params.Set("sort[]", "downloads desc")

	reqURL := archiveSearchEndpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrentstream: archive search: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrentstream: archive search status %d", resp.StatusCode)
	}

	return parseArchiveResponse(resp.Body)
}

// parseArchiveResponse is split out so it can be unit-tested with a fixed
// JSON body (no network).
func parseArchiveResponse(r io.Reader) ([]SearchResult, error) {
	var ar archiveResponse
	if err := json.NewDecoder(r).Decode(&ar); err != nil {
		return nil, fmt.Errorf("torrentstream: decode archive response: %w", err)
	}
	out := make([]SearchResult, 0, len(ar.Response.Docs))
	for _, d := range ar.Response.Docs {
		if d.Identifier == "" {
			continue
		}
		out = append(out, SearchResult{
			Identifier: d.Identifier,
			Title:      d.Title,
			Mediatype:  d.Mediatype,
			Year:       trimYear(d.Year),
			TorrentURL: ArchiveTorrentURL(d.Identifier),
		})
	}
	return out, nil
}

// ArchiveTorrentURL builds the stable per-item torrent URL.
func ArchiveTorrentURL(identifier string) string {
	return fmt.Sprintf("%s/%s/%s_archive.torrent", archiveDownloadBase, identifier, identifier)
}

// trimYear normalises IA's year field, which arrives as a JSON string
// ("1971") or number (1971) depending on the item.
func trimYear(raw json.RawMessage) string {
	s := string(raw)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "null" {
		return ""
	}
	return s
}
