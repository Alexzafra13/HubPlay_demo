package torrentstream

import (
	"context"
	"log/slog"
	"time"
)

// SourceService is the aggregation + curation layer the HTTP API consumes:
// given an IMDb id it fetches sources from the configured Torznab indexers,
// then runs the curation pipeline (parse → filter → sort → group) before
// returning a clean, ordered list.
//
// Flow: Fetch Torznab → (metadata already parsed during normalisation) →
// Curate(filters + sort + per-resolution cap) → return.
//
// The RAW fetched set is cached per (type, imdbid) for a TTL; curation runs
// per request so different users' preferences (via WithFilterOptions on the
// context) reuse the same cache entry but get their own ordering/filtering.
//
// It is deliberately decoupled from the streaming Manager — searching for
// sources does not require the torrent engine to be running. Playback (the
// /torrent/stream endpoint) is a separate concern wired independently.
type SourceService struct {
	client     searcher
	cache      *ttlCache[[]SearchResult]
	filterOpts FilterOptions
	logger     *slog.Logger
}

// searcher is the slice of *TorznabClient SourceService needs, kept as an
// interface so tests can inject a fake (and assert cache behaviour).
type searcher interface {
	Search(ctx context.Context, mt MediaType, imdbID, term string) ([]SearchResult, error)
}

// NewSourceService builds the aggregator. ttl <= 0 defaults to 20 minutes.
// opts is the default curation policy (per-request overrides via
// WithFilterOptions take precedence).
func NewSourceService(client searcher, ttl time.Duration, opts FilterOptions, logger *slog.Logger) *SourceService {
	if ttl <= 0 {
		ttl = 20 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceService{
		client:     client,
		cache:      newTTLCache[[]SearchResult](ttl),
		filterOpts: opts,
		logger:     logger,
	}
}

// Sources returns the curated streamable sources for an IMDb id. The raw
// indexer results are served from the cache on a hit (queried + cached on a
// miss); curation is applied on every call so context-supplied preferences
// are honoured. The caller (handler) is responsible for validating imdbID.
func (s *SourceService) Sources(ctx context.Context, mt MediaType, imdbID string) ([]SearchResult, error) {
	key := string(mt) + ":" + imdbID
	raw, ok := s.cache.get(key)
	if !ok {
		res, err := s.client.Search(ctx, mt, imdbID, "")
		if err != nil {
			return nil, err
		}
		raw = res
		s.cache.set(key, raw)
	}
	opts := filterOptionsFrom(ctx, s.filterOpts)
	return Curate(raw, opts), nil
}
