package torrentstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is returned when an indexer answers HTTP 429. Handlers map
// it to a "try again shortly" response so the UI can show a clear message
// instead of a generic failure.
var ErrRateLimited = errors.New("torznab: indexer rate-limited")

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
//
// Prowlarr is the canonical source: it aggregates many trackers behind one
// Torznab endpoint, so a single instance usually suffices. HubPlay only
// ever speaks the standard Torznab API — it doesn't manage Prowlarr's own
// indexer config.
type TorznabIndexer struct {
	// Name labels the source in results and logs.
	Name string
	// URL is the resolved Torznab API endpoint (e.g.
	// http://localhost:9696/api/v1/indexers/all/results/torznab).
	URL string
	// BaseURL is the Prowlarr root (kept for the admin surface / native
	// API); empty when the operator supplied a full Torznab URL directly.
	BaseURL string
	// APIKey is appended as ?apikey=. Empty is allowed (some indexers
	// embed the key in the URL path).
	APIKey string
	// MovieCategories / SeriesCategories are sent as cat= per search type
	// (default: 2000 for movies, 5000 for tv).
	MovieCategories  []string
	SeriesCategories []string
	// Trackers appended to magnets built from a bare infohash.
	Trackers []string
}

// perIndexerTimeout bounds a single indexer round-trip so one hung/slow
// endpoint can't hold up the whole fan-out (which runs concurrently).
const perIndexerTimeout = 8 * time.Second

// IndexerProvider supplies the current set of indexers to query. It is
// read on every search, so a DB-backed provider lets admin edits take
// effect immediately (no restart).
type IndexerProvider interface {
	Indexers(ctx context.Context) []TorznabIndexer
}

// StaticIndexers is an IndexerProvider backed by a fixed slice — used for
// config-only setups and tests.
type StaticIndexers []TorznabIndexer

// Indexers implements IndexerProvider.
func (s StaticIndexers) Indexers(context.Context) []TorznabIndexer { return s }

// TorznabClient queries the indexers supplied by its IndexerProvider. Safe
// for concurrent use (the http.Client is, and the provider is read-only per
// call).
type TorznabClient struct {
	source       IndexerProvider
	httpClient   *http.Client
	perIndexerTO time.Duration
	logger       *slog.Logger
	// idxCache memoises each Prowlarr root's indexer list so we don't hit
	// /api/v1/indexer on every search (the list rarely changes).
	idxCache *ttlCache[[]prowlarrIndexer]
	// breaker skips an endpoint for a cooldown window after it answers 429,
	// so a single rate-limit doesn't snowball into a self-inflicted ban.
	breaker *cooldownBreaker
	// now is the clock (overridable in tests for the breaker).
	now func() time.Time
}

// rateLimitCooldown is how long an endpoint is skipped after a 429 before we
// probe it again. Public trackers throttle aggressively; backing off is the
// polite (and effective) response.
const rateLimitCooldown = 5 * time.Minute

// NewTorznabClient builds a client over the given indexer source.
func NewTorznabClient(source IndexerProvider, logger *slog.Logger) *TorznabClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &TorznabClient{
		source:       source,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		perIndexerTO: perIndexerTimeout,
		logger:       logger,
		idxCache:     newTTLCache[[]prowlarrIndexer](5 * time.Minute),
		breaker:      newCooldownBreaker(rateLimitCooldown),
		now:          time.Now,
	}
}

// cooldownBreaker is a tiny per-key circuit breaker: once tripped, a key is
// reported "open" (skip it) until its cooldown elapses. Concurrency-safe.
type cooldownBreaker struct {
	mu    sync.Mutex
	until map[string]time.Time
	dur   time.Duration
}

func newCooldownBreaker(dur time.Duration) *cooldownBreaker {
	return &cooldownBreaker{until: make(map[string]time.Time), dur: dur}
}

// open reports whether key is still cooling down (and lazily forgets expired
// entries).
func (b *cooldownBreaker) open(key string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	u, ok := b.until[key]
	if !ok {
		return false
	}
	if !now.Before(u) {
		delete(b.until, key)
		return false
	}
	return true
}

// trip starts (or extends) the cooldown for key.
func (b *cooldownBreaker) trip(key string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until[key] = now.Add(b.dur)
}

// prowlarrIndexers returns the (cached) native indexer list for a Prowlarr
// root.
func (c *TorznabClient) prowlarrIndexers(ctx context.Context, base, apiKey string) ([]prowlarrIndexer, error) {
	if v, ok := c.idxCache.get(base); ok {
		return v, nil
	}
	list, err := fetchProwlarrIndexers(ctx, c.httpClient, base, apiKey)
	if err != nil {
		return nil, err
	}
	c.idxCache.set(base, list)
	return list, nil
}

// torznabEndpoint is a concrete Torznab feed to query (already resolved
// from an indexer record).
type torznabEndpoint struct {
	url    string
	apiKey string
	name   string // for logging only
}

// looksLikeTorznab reports whether raw is already a Torznab feed URL (a
// Jackett "/all/results/torznab" aggregate, a Prowlarr per-indexer "/{id}/
// api", etc.) rather than a bare Prowlarr root that must be expanded.
func looksLikeTorznab(raw string) bool {
	r := strings.ToLower(raw)
	return strings.Contains(r, "torznab") || strings.Contains(r, "/api/v2.0/")
}

// resolveEndpoints turns one configured indexer into the concrete Torznab
// feeds to query.
//
//   - A full Torznab URL (Jackett aggregate, or an explicit feed) is used
//     as-is.
//   - A bare Prowlarr root is EXPANDED: Prowlarr has no combined Torznab
//     feed, so we enumerate its indexers via the native API
//     (/api/v1/indexer) and query each one's per-indexer Torznab feed
//     (/{id}/api), which honours imdbid. Results are merged upstream.
func (c *TorznabClient) resolveEndpoints(ctx context.Context, idx TorznabIndexer) []torznabEndpoint {
	raw := strings.TrimSpace(idx.URL)
	if raw == "" {
		raw = strings.TrimSpace(idx.BaseURL)
	}
	if raw == "" {
		return nil
	}
	if looksLikeTorznab(raw) {
		return []torznabEndpoint{{url: raw, apiKey: idx.APIKey, name: idx.Name}}
	}
	list, err := c.prowlarrIndexers(ctx, raw, idx.APIKey)
	if err != nil {
		c.logger.Warn("torznab: prowlarr indexer list failed", "indexer", idx.Name, "error", err)
		return nil
	}
	base := strings.TrimRight(raw, "/")
	eps := make([]torznabEndpoint, 0, len(list))
	for _, pix := range list {
		if !pix.Enable {
			continue
		}
		eps = append(eps, torznabEndpoint{
			url:    fmt.Sprintf("%s/%d/api", base, pix.ID),
			apiKey: idx.APIKey,
			name:   idx.Name + "/" + pix.Name,
		})
	}
	return eps
}

// Search fans the query out across every resolved Torznab feed
// CONCURRENTLY, normalises and merges the results, drops duplicate
// infohashes and sorts by availability/quality. Each feed gets its own
// strict timeout (perIndexerTO), so one hung/slow endpoint can't hold up
// the others or the API response. A feed that errors is logged and skipped;
// an all-empty run surfaces the first error so the caller can report it.
//
// The Query drives a HYBRID search: each endpoint is queried by imdbid
// (precise, trusted) AND by composed free text (title+year / title+SxxEyy,
// broadly supported). The two passes run concurrently and their results are
// merged, so an indexer that ignores imdbid still contributes via text. The
// text pass is title-verified (matchesQuery) to drop false positives; the
// imdbid pass is trusted as-is.
func (c *TorznabClient) Search(ctx context.Context, q Query) ([]SearchResult, error) {
	indexers := c.source.Indexers(ctx)
	modes := q.modes()

	type target struct {
		ep  torznabEndpoint
		idx TorznabIndexer
	}
	var targets []target
	for _, idx := range indexers {
		for _, ep := range c.resolveEndpoints(ctx, idx) {
			targets = append(targets, target{ep: ep, idx: idx})
		}
	}

	results := make([][]SearchResult, len(targets))
	errs := make([]error, len(targets))
	rateLimited := make([]bool, len(targets)) // tripped now OR still cooling down
	now := c.now()

	var wg sync.WaitGroup
	for i := range targets {
		// Circuit breaker: skip an endpoint that 429'd recently instead of
		// hammering it again (the snowball that causes the self-ban).
		if c.breaker.open(targets[i].ep.url, now) {
			rateLimited[i] = true
			c.logger.Debug("torznab: skipping endpoint in cooldown", "endpoint", targets[i].ep.name)
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, limited, err := c.searchEndpoint(ctx, targets[i].ep, targets[i].idx, q, modes)
			switch {
			case limited:
				// Back off this endpoint for the cooldown window.
				c.breaker.trip(targets[i].ep.url, c.now())
				rateLimited[i] = true
				c.logger.Warn("torznab: endpoint rate-limited; cooling down",
					"endpoint", targets[i].ep.name, "cooldown", c.breaker.dur)
			case err != nil:
				// Log the endpoint NAME, never the built URL — it carries the apikey.
				c.logger.Warn("torznab: query failed", "endpoint", targets[i].ep.name, "error", err)
				errs[i] = err
			default:
				results[i] = out
			}
		}(i)
	}
	wg.Wait()

	var (
		out      []SearchResult
		firstErr error
		anyRL    bool
	)
	for i := range targets {
		out = append(out, results[i]...)
		if rateLimited[i] {
			anyRL = true
		}
		if firstErr == nil && errs[i] != nil {
			firstErr = errs[i]
		}
	}
	if len(out) == 0 {
		// Prefer the rate-limit signal so the UI shows "wait and retry" rather
		// than a generic failure when everything we have is cooling down.
		if anyRL {
			return nil, ErrRateLimited
		}
		if firstErr != nil {
			return nil, firstErr
		}
	}
	sortSources(out)
	return dedupeByInfoHash(out), nil
}

// searchEndpoint queries one feed across the requested modes SEQUENTIALLY (not
// a parallel burst — kinder to per-second rate limits) with early-exit: the
// precise imdbid pass runs first, and the broad text pass is skipped entirely
// when imdbid already produced hits. Returns the normalised results, whether
// the endpoint rate-limited us (429), and the first non-429 error otherwise.
func (c *TorznabClient) searchEndpoint(ctx context.Context, ep torznabEndpoint, idx TorznabIndexer, q Query, modes []queryMode) ([]SearchResult, bool, error) {
	var (
		out      []SearchResult
		firstErr error
	)
	for _, mode := range modes {
		ictx, cancel := context.WithTimeout(ctx, c.perIndexerTO)
		items, err := c.queryEndpoint(ictx, ep, idx, q, mode)
		cancel()
		if err != nil {
			if errors.Is(err, ErrRateLimited) {
				return out, true, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, it := range items {
			r, ok := normalizeItem(it, idx)
			if !ok {
				continue
			}
			// Verify the text pass against the requested title; trust the
			// imdbid pass (the indexer matched the id).
			if mode == modeText && !matchesQuery(r, q) {
				continue
			}
			out = append(out, r)
		}
		// De-amplify: if the imdbid pass already found hits, don't also run the
		// broad text pass against the same endpoint.
		if mode == modeIMDb && len(out) > 0 {
			break
		}
	}
	return out, false, firstErr
}

// queryEndpoint performs a single Torznab feed round-trip and returns the
// raw parsed items.
func (c *TorznabClient) queryEndpoint(ctx context.Context, ep torznabEndpoint, idx TorznabIndexer, q Query, mode queryMode) ([]torznabItem, error) {
	reqURL, err := buildTorznabURL(ep.url, ep.apiKey, idx, q, mode)
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
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("%w (status 429)", ErrRateLimited)
		}
		return nil, fmt.Errorf("torznab: status %d", resp.StatusCode)
	}
	return parseTorznab(io.LimitReader(resp.Body, maxTorznabResponseBytes))
}

// maxTorznabResponseBytes bounds the XML we will buffer/parse from an
// indexer so a hostile or broken endpoint can't exhaust memory.
const maxTorznabResponseBytes = 8 << 20

// buildTorznabURL assembles the Torznab query string onto a concrete feed
// endpoint for one search mode. The IMDb id is sent without the leading "tt"
// (Torznab spec). Existing query params on the endpoint are preserved.
//
//   - modeIMDb uses the typed search (t=movie / t=tvsearch) with imdbid, plus
//     season/ep for a scoped series episode — the precise path for indexers
//     that index by IMDb id.
//   - modeText uses the generic t=search with a composed query string
//     ("Title Year" / "Title SxxEyy"); many indexers return nothing for
//     t=movie/tvsearch with only free text, so the generic type is widest.
func buildTorznabURL(endpoint, apiKey string, idx TorznabIndexer, query Query, mode queryMode) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid endpoint url %q", endpoint)
	}
	q := u.Query()

	switch mode {
	case modeIMDb:
		if query.Type == MediaTypeSeries {
			q.Set("t", "tvsearch")
			if query.Season > 0 {
				q.Set("season", strconv.Itoa(query.Season))
				if query.Episode > 0 {
					q.Set("ep", strconv.Itoa(query.Episode))
				}
			}
		} else {
			q.Set("t", "movie")
		}
		if id := imdbDigits(query.IMDbID); id != "" {
			q.Set("imdbid", id)
		}
	default: // modeText
		q.Set("t", "search")
		if term := query.textQuery(); term != "" {
			q.Set("q", term)
		}
	}

	var cats []string
	if query.Type == MediaTypeSeries {
		cats = idx.SeriesCategories
		if len(cats) == 0 {
			cats = []string{"5000"}
		}
	} else {
		cats = idx.MovieCategories
		if len(cats) == 0 {
			cats = []string{"2000"}
		}
	}
	q.Set("cat", strings.Join(cats, ","))
	if apiKey != "" {
		q.Set("apikey", apiKey)
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
