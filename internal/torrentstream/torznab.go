package torrentstream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Torznab/Newznab aggregation. A TorznabClient queries one or more
// operator-configured indexer endpoints (typically a self-hosted Prowlarr
// or Jackett) by IMDb id, parses the XML feed, normalises every item into
// a SearchResult and merges/sorts the results across indexers.
//
// The endpoints are configured by the operator (config file / env), never
// supplied by an end user, so — unlike the public .torrent fetch in
// manager.go — the query itself is NOT SSRF-guarded: a Prowlarr instance
// normally lives on localhost or the LAN, which the SSRF guard would
// (correctly, for user-supplied URLs) reject. What the operator points
// HubPlay at, and whether they are entitled to the results, is the
// operator's responsibility.

// MediaType selects the Torznab search mode (t=movie vs t=tvsearch) and
// the default category set.
type MediaType string

const (
	MediaTypeMovie  MediaType = "movie"
	MediaTypeSeries MediaType = "series"
)

// TorznabIndexer is one configured indexer endpoint. Categories/Trackers
// fall back to sane defaults when empty.
type TorznabIndexer struct {
	// Name labels the source in results and logs.
	Name string
	// URL is the Torznab API base (e.g. http://localhost:9696/api/v1/indexers/all/results/torznab).
	URL string
	// APIKey is appended as ?apikey=. Empty is allowed (some indexers
	// embed the key in the URL path).
	APIKey string
	// Categories sent as cat= (default: 2000 for movies, 5000 for tv).
	Categories []string
	// Trackers appended to magnets built from a bare infohash.
	Trackers []string
}

// TorznabClient queries a fixed set of indexers. Safe for concurrent use
// (the http.Client is, and the indexer slice is read-only after New).
type TorznabClient struct {
	indexers   []TorznabIndexer
	httpClient *http.Client
	logger     *slog.Logger
}

// NewTorznabClient builds a client over the given indexers.
func NewTorznabClient(indexers []TorznabIndexer, logger *slog.Logger) *TorznabClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &TorznabClient{
		indexers:   indexers,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		logger:     logger,
	}
}

// Search fans the query out across every configured indexer, normalises
// and merges the results, drops duplicate infohashes and sorts by
// availability/quality. An indexer that errors is logged and skipped so
// one bad endpoint doesn't sink the whole search; an all-empty run
// surfaces the first error so the caller can report a failure.
//
// imdbID is the canonical "ttNNNNNNN" form (the handler validates it);
// term is an optional free-text fallback for indexers that don't resolve
// by IMDb id.
func (c *TorznabClient) Search(ctx context.Context, mt MediaType, imdbID, term string) ([]SearchResult, error) {
	var (
		out      []SearchResult
		firstErr error
	)
	for _, idx := range c.indexers {
		items, err := c.queryIndexer(ctx, idx, mt, imdbID, term)
		if err != nil {
			// Note: we log the indexer NAME, never the built URL — it
			// carries the apikey.
			c.logger.Warn("torznab: indexer query failed", "indexer", idx.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, it := range items {
			if r, ok := normalizeItem(it, idx); ok {
				out = append(out, r)
			}
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sortSources(out)
	return dedupeByInfoHash(out), nil
}

// queryIndexer performs a single indexer round-trip and returns the raw
// parsed items.
func (c *TorznabClient) queryIndexer(ctx context.Context, idx TorznabIndexer, mt MediaType, imdbID, term string) ([]torznabItem, error) {
	reqURL, err := buildTorznabURL(idx, mt, imdbID, term)
	if err != nil {
		return nil, fmt.Errorf("torznab: build url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torznab: get: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		// Drain a little so the connection can be reused; ignore errors.
		_, _ = io.CopyN(io.Discard, resp.Body, 4<<10)
		return nil, fmt.Errorf("torznab: status %d", resp.StatusCode)
	}
	return parseTorznab(io.LimitReader(resp.Body, maxTorznabResponseBytes))
}

// maxTorznabResponseBytes bounds the XML we will buffer/parse from an
// indexer so a hostile or broken endpoint can't exhaust memory.
const maxTorznabResponseBytes = 8 << 20

// buildTorznabURL assembles the Torznab query string. The IMDb id is sent
// without the leading "tt" (Torznab spec). Existing query params on the
// base URL (some Jackett configs embed an apikey there) are preserved.
func buildTorznabURL(idx TorznabIndexer, mt MediaType, imdbID, term string) (string, error) {
	u, err := url.Parse(idx.URL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid indexer url %q", idx.URL)
	}
	q := u.Query()
	switch mt {
	case MediaTypeSeries:
		q.Set("t", "tvsearch")
	default:
		q.Set("t", "movie")
	}
	if id := imdbDigits(imdbID); id != "" {
		q.Set("imdbid", id)
	}
	if term != "" {
		q.Set("q", term)
	}
	cats := idx.Categories
	if len(cats) == 0 {
		if mt == MediaTypeSeries {
			cats = []string{"5000"}
		} else {
			cats = []string{"2000"}
		}
	}
	q.Set("cat", strings.Join(cats, ","))
	if idx.APIKey != "" {
		q.Set("apikey", idx.APIKey)
	}
	q.Set("o", "xml")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// imdbDigits strips the "tt" prefix and any surrounding space, returning
// the bare numeric id Torznab expects. Returns "" for an empty input.
func imdbDigits(imdbID string) string {
	s := strings.TrimSpace(imdbID)
	s = strings.TrimPrefix(strings.ToLower(s), "tt")
	return s
}

// dedupeByInfoHash collapses results that share an infohash, keeping the
// first occurrence. Callers sort by seeders first, so the survivor is the
// best-seeded copy. Results without an infohash are always kept (they
// can't be proven duplicates).
func dedupeByInfoHash(in []SearchResult) []SearchResult {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, r := range in {
		if r.InfoHash != "" {
			if _, dup := seen[r.InfoHash]; dup {
				continue
			}
			seen[r.InfoHash] = struct{}{}
		}
		out = append(out, r)
	}
	return out
}

// sortSources orders results: seeders desc, then quality (4K > 1080p >
// 720p …), then size desc as a stable tiebreak.
func sortSources(res []SearchResult) {
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Seeders != res[j].Seeders {
			return res[i].Seeders > res[j].Seeders
		}
		ri, rj := qualityRank(res[i].Quality), qualityRank(res[j].Quality)
		if ri != rj {
			return ri > rj
		}
		return res[i].SizeBytes > res[j].SizeBytes
	})
}
