package federation

// Manager methods for cross-peer search and "what's new?" rails.
// Mirrors Browse in shape (fan-out, per-peer timeout, errors don't
// blank the whole result) but the inputs and aggregation differ
// enough that grouping with Browse made manager.go a maze. Lifted
// here so each "fan-out shape" reads as one self-contained file.

import (
	"context"
	"time"
)

// SharedItemFromPeer pairs a search hit with the peer it came from
// so the user-facing handler can render an origin badge and route
// Play through the right peer.
type SharedItemFromPeer struct {
	Peer *Peer
	Item *SharedItem
}

// SearchLocalSharedItems answers an inbound peer search request: what
// items in libraries shared with `peerID` match `query`. The repo
// applies the share ACL (CanBrowse JOIN); we just bound the result.
func (m *Manager) SearchLocalSharedItems(ctx context.Context, peerID, query string, limit int) ([]*SharedItem, error) {
	return m.repo.SearchSharedItems(ctx, peerID, query, limit)
}

// ListLocalRecentSharedItems answers an inbound peer "what's new?"
// request: most recently added items across libraries shared with
// `peerID`. Same ACL gate as the search path. Bound to a sensible
// per-peer cap server-side so a peer cannot pull the whole catalog
// in one call.
func (m *Manager) ListLocalRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*SharedItem, error) {
	return m.repo.ListRecentSharedItems(ctx, peerID, limit)
}

// SearchAllPeers fans out the user's query to every paired peer in
// parallel and aggregates the results with origin metadata. A peer
// that times out, errors, or is offline is logged and skipped — the
// rest still surface, so a single misbehaving peer cannot blank a
// federated search.
//
// perPeerTimeout caps the wait per outbound call so a slow peer
// cannot drag the user-visible response past the (separate) request
// deadline. Sized for a fast LAN/Tailscale topology; admins running
// over a slow WAN can extend via config in a future revision.
func (m *Manager) SearchAllPeers(ctx context.Context, query string, perPeerLimit int, perPeerTimeout time.Duration) ([]*SharedItemFromPeer, error) {
	if query == "" {
		return []*SharedItemFromPeer{}, nil
	}
	peers, err := m.repo.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	if perPeerLimit <= 0 {
		perPeerLimit = 25
	}
	if perPeerTimeout <= 0 {
		perPeerTimeout = 2 * time.Second
	}

	type result struct {
		peer  *Peer
		items []*SharedItem
		err   error
	}
	results := make(chan result, len(peers))
	dispatched := 0
	for _, p := range peers {
		if p.Status != PeerPaired {
			continue
		}
		dispatched++
		go func(peer *Peer) {
			callCtx, cancel := context.WithTimeout(ctx, perPeerTimeout)
			defer cancel()
			items, err := m.FetchPeerSearch(callCtx, peer.ID, query, perPeerLimit)
			results <- result{peer: peer, items: items, err: err}
		}(p)
	}

	out := []*SharedItemFromPeer{}
	for i := 0; i < dispatched; i++ {
		r := <-results
		if r.err != nil {
			m.logger.Info("federation: peer search failed",
				"peer_id", r.peer.ID, "err", r.err)
			continue
		}
		for _, it := range r.items {
			out = append(out, &SharedItemFromPeer{Peer: r.peer, Item: it})
		}
	}
	return out, nil
}

// RecentFromAllPeers fans out a "what's new?" request to every paired
// peer in parallel and aggregates with origin attribution. Mirrors
// SearchAllPeers in shape (per-peer timeout, errors-don't-blank,
// per-peer limit for fairness) — the only differences are no query
// string and the per-peer endpoint hit by the client. Powers the
// home page's "Recently added on peers" rail.
func (m *Manager) RecentFromAllPeers(ctx context.Context, perPeerLimit int, perPeerTimeout time.Duration) ([]*SharedItemFromPeer, error) {
	peers, err := m.repo.ListPeers(ctx)
	if err != nil {
		return nil, err
	}
	if perPeerLimit <= 0 {
		perPeerLimit = 12
	}
	if perPeerTimeout <= 0 {
		perPeerTimeout = 2 * time.Second
	}

	type result struct {
		peer  *Peer
		items []*SharedItem
		err   error
	}
	results := make(chan result, len(peers))
	dispatched := 0
	for _, p := range peers {
		if p.Status != PeerPaired {
			continue
		}
		dispatched++
		go func(peer *Peer) {
			callCtx, cancel := context.WithTimeout(ctx, perPeerTimeout)
			defer cancel()
			items, err := m.FetchPeerRecent(callCtx, peer.ID, perPeerLimit)
			results <- result{peer: peer, items: items, err: err}
		}(p)
	}

	out := []*SharedItemFromPeer{}
	for i := 0; i < dispatched; i++ {
		r := <-results
		if r.err != nil {
			m.logger.Info("federation: peer recent failed",
				"peer_id", r.peer.ID, "err", r.err)
			continue
		}
		for _, it := range r.items {
			out = append(out, &SharedItemFromPeer{Peer: r.peer, Item: it})
		}
	}
	return out, nil
}
