package federation

// Metodos de Manager para el browse remoto: fan-out a peers,
// agregacion y cache para browsing offline-friendly.

import (
	"context"
	"time"
)

// cacheStaleThreshold: pasado 1h se refresca en background.
const cacheStaleThreshold = time.Hour

// BrowsePeerLibraries devuelve bibliotecas compartidas. Siempre live (sin cache).
func (m *Manager) BrowsePeerLibraries(ctx context.Context, peerID string) ([]*SharedLibrary, error) {
	libs, err := m.FetchPeerLibraries(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if libs == nil {
		libs = []*SharedLibrary{}
	}
	return libs, nil
}

// BrowseAllPeerLibraries hace fan-out paralelo a todos los peers paired
// y agrega las bibliotecas. Peer offline se loguea y se omite.
func (m *Manager) BrowseAllPeerLibraries(ctx context.Context) ([]*SharedLibraryWithPeer, error) {
	peers, err := m.repo.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	out := []*SharedLibraryWithPeer{}
	type result struct {
		peer *Peer
		libs []*SharedLibrary
		err  error
	}
	results := make(chan result, len(peers))
	dispatched := 0
	for _, p := range peers {
		if p.Status != PeerPaired {
			continue
		}
		dispatched++
		go func(peer *Peer) {
			libs, err := m.FetchPeerLibraries(ctx, peer.ID)
			results <- result{peer: peer, libs: libs, err: err}
		}(p)
	}
	for i := 0; i < dispatched; i++ {
		r := <-results
		if r.err != nil {
			m.logger.Warn("federation: fetch peer libraries (unified view)",
				"peer_id", r.peer.ID, "err", r.err)
			continue
		}
		for _, lib := range r.libs {
			out = append(out, &SharedLibraryWithPeer{Peer: r.peer, Library: lib})
		}
	}
	return out, nil
}

// BrowseResult agrupa la respuesta paginada de BrowsePeerItems.
type BrowseResult struct {
	Items   []*SharedItem
	Total   int
	Partial bool // true cuando los datos vienen de cache (potencialmente stale)
}

// BrowsePeerItems returns paginated items for a peer's library with
// a read-through cache. Strategy:
//
//   1. Check cache age. If fresh (< staleThreshold), serve from cache.
//   2. If stale or empty, attempt live fetch. On success, write to
//      cache then serve.
//   3. If live fetch fails AND we have any cache, serve stale cache
//      with a "stale" indicator so the user sees content rather than
//      a broken page when the peer is offline.
func (m *Manager) BrowsePeerItems(ctx context.Context, peerID, libraryID string, offset, limit int) (BrowseResult, error) {
	page, cacheErr := m.repo.ListCachedItems(ctx, peerID, libraryID, offset, limit)
	if cacheErr != nil {
		m.logger.Warn("cache read failed, falling back to live",
			"peer_id", peerID, "err", cacheErr)
	}

	now := m.clock.Now()
	cacheFresh := cacheErr == nil && len(page.Items) > 0 && now.Sub(page.LastSync) < cacheStaleThreshold

	if cacheFresh {
		return BrowseResult{Items: page.Items, Total: page.Total, Partial: true}, nil
	}

	live, liveTotal, liveErr := m.FetchPeerItems(ctx, peerID, libraryID, offset, limit)
	if liveErr == nil {
		// Solo cachear offset=0 para snapshot coherente de primera pagina.
		if offset == 0 && len(live) > 0 {
			if err := m.repo.UpsertCachedItems(ctx, peerID, libraryID, live, now); err != nil {
				m.logger.Warn("cache write failed", "peer_id", peerID, "err", err)
			}
		}
		return BrowseResult{Items: live, Total: liveTotal, Partial: false}, nil
	}

	// Live failed — serve stale cache if any.
	if cacheErr == nil && len(page.Items) > 0 {
		m.logger.Info("serving stale cache (peer offline)",
			"peer_id", peerID, "age", now.Sub(page.LastSync), "live_err", liveErr)
		return BrowseResult{Items: page.Items, Total: page.Total, Partial: true}, nil
	}

	return BrowseResult{}, liveErr
}

// PurgeCache limpia cache de items para (peer, library).
func (m *Manager) PurgeCache(ctx context.Context, peerID, libraryID string) error {
	return m.repo.PurgeCachedItemsForLibrary(ctx, peerID, libraryID)
}
