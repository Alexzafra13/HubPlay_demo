package torrentstream

import "context"

// SearchProvider is one searchable source. The manager fans a query out
// across every registered provider and merges the results. This is the
// extension point for adding MORE LEGAL catalogues later (other Internet
// Archive collections, public-domain / Creative Commons APIs, a peer's
// shared library…). The project ships exactly one provider — Internet
// Archive — and does not include any third-party torrent-indexer scraper.
type SearchProvider interface {
	// Name identifies the provider in results and logs.
	Name() string
	// Search runs a free-text query and returns up to `limit` results,
	// each carrying a streamable TorrentURL (or magnet).
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// archiveProvider is the built-in Internet Archive provider.
type archiveProvider struct{}

func (archiveProvider) Name() string { return "internetarchive" }

func (archiveProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	res, err := SearchArchive(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	for i := range res {
		res[i].Provider = "internetarchive"
	}
	return res, nil
}

// defaultProviders is the set wired by New. Internet Archive only.
func defaultProviders() []SearchProvider {
	return []SearchProvider{archiveProvider{}}
}

// Search fans the query out across the registered providers and merges
// the results. A provider that errors is logged and skipped so one bad
// source doesn't sink the whole search; an all-empty run surfaces the
// first error so the caller can report a failure.
func (m *Manager) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	var (
		out      []SearchResult
		firstErr error
	)
	for _, p := range m.providers {
		res, err := p.Search(ctx, query, limit)
		if err != nil {
			m.logger.Warn("torrentstream: provider search failed", "provider", p.Name(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, res...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
