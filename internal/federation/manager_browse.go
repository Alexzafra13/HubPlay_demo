package federation

// Manager methods for the user-facing remote-browse surface (Phase 4):
// fan-out to peer endpoints, aggregate, cache for offline-friendly
// browsing. Lifted out of manager.go so the cache + fan-out logic
// stays separate from the share ACL gate.

import (
	"context"
	"time"
)

// cacheStaleThreshold — beyond this we kick a background refresh.
// 1h matches "user expects fresh-ish but not real-time" — they
// already saw a peer add titles when they opened the app earlier.
const cacheStaleThreshold = time.Hour

// BrowsePeerLibraries returns the libraries a peer has shared with us.
// Always live — libraries are a small list, no caching needed.
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

// BrowseAllPeerLibraries fans out to every paired peer in parallel
// and aggregates their shared libraries into a single flat list with
// the originating peer attached. Powers the unified "/peers" landing
// page — one round trip from the user's perspective even if there
// are five peers.
//
// A peer that's offline (or returns an error) is logged and skipped;
// the rest still surface. This keeps the user view useful even when
// one server is down.
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

// BrowsePeerItems returns paginated items for a peer's library with
// a read-through cache. Strategy:
//
//   1. Check cache age. If fresh (< staleThreshold), serve from cache.
//   2. If stale or empty, attempt live fetch. On success, write to
//      cache then serve.
//   3. If live fetch fails AND we have any cache, serve stale cache
//      with a "stale" indicator so the user sees content rather than
//      a broken page when the peer is offline.
//
// Returns items, total, and a fromCache flag the API layer can pass
// through so the UI shows the right freshness badge.
func (m *Manager) BrowsePeerItems(ctx context.Context, peerID, libraryID string, offset, limit int) ([]*SharedItem, int, bool, error) {
	cached, cachedTotal, cachedAt, cacheErr := m.repo.ListCachedItems(ctx, peerID, libraryID, offset, limit)
	if cacheErr != nil {
		m.logger.Warn("cache read failed, falling back to live",
			"peer_id", peerID, "err", cacheErr)
	}

	now := m.clock.Now()
	cacheFresh := cacheErr == nil && len(cached) > 0 && now.Sub(cachedAt) < cacheStaleThreshold

	if cacheFresh {
		return cached, cachedTotal, true, nil
	}

	live, liveTotal, liveErr := m.FetchPeerItems(ctx, peerID, libraryID, offset, limit)
	if liveErr == nil {
		// Persist to cache only when offset=0 so the cached snapshot
		// is a coherent first-page view. Phase 7+ extends this to a
		// background full-catalog walk.
		if offset == 0 && len(live) > 0 {
			if err := m.repo.UpsertCachedItems(ctx, peerID, libraryID, live, now); err != nil {
				m.logger.Warn("cache write failed", "peer_id", peerID, "err", err)
			}
		}
		return live, liveTotal, false, nil
	}

	// Live failed — serve stale cache if any.
	if cacheErr == nil && len(cached) > 0 {
		m.logger.Info("serving stale cache (peer offline)",
			"peer_id", peerID, "age", now.Sub(cachedAt), "live_err", liveErr)
		return cached, cachedTotal, true, nil
	}

	return nil, 0, false, liveErr
}

// PurgeCache clears cached items for (peer, library) — wired to the
// admin "force refresh" button and called when a peer is revoked.
func (m *Manager) PurgeCache(ctx context.Context, peerID, libraryID string) error {
	return m.repo.PurgeCachedItemsForLibrary(ctx, peerID, libraryID)
}
