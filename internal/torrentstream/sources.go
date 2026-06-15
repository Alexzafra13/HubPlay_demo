package torrentstream

import (
	"context"
	"log/slog"
	"time"
)

// SourceService is the aggregation layer the HTTP API consumes: given an
// IMDb id it returns normalised, sorted StreamSources from the configured
// Torznab indexers, cached per (type, imdbid) for a TTL.
//
// It is deliberately decoupled from the streaming Manager — searching for
// sources does not require the torrent engine to be running. Playback (the
// /torrent/stream endpoint) is a separate concern wired independently.
type SourceService struct {
	client searcher
	cache  *ttlCache[[]SearchResult]
	logger *slog.Logger
}

// searcher is the slice of *TorznabClient SourceService needs, kept as an
// interface so tests can inject a fake (and assert cache behaviour).
type searcher interface {
	Search(ctx context.Context, mt MediaType, imdbID, term string) ([]SearchResult, error)
}

// NewSourceService builds the aggregator. ttl <= 0 defaults to 20 minutes.
func NewSourceService(client searcher, ttl time.Duration, logger *slog.Logger) *SourceService {
	if ttl <= 0 {
		ttl = 20 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceService{
		client: client,
		cache:  newTTLCache[[]SearchResult](ttl),
		logger: logger,
	}
}

// Sources returns the streamable sources for an IMDb id, serving from the
// cache on a hit and querying the indexers (then caching) on a miss. The
// caller (handler) is responsible for validating imdbID.
func (s *SourceService) Sources(ctx context.Context, mt MediaType, imdbID string) ([]SearchResult, error) {
	key := string(mt) + ":" + imdbID
	if v, ok := s.cache.get(key); ok {
		return v, nil
	}
	res, err := s.client.Search(ctx, mt, imdbID, "")
	if err != nil {
		return nil, err
	}
	s.cache.set(key, res)
	return res, nil
}
