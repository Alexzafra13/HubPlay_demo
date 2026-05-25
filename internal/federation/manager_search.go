package federation

// Metodos de Manager para busqueda cross-peer y rails "novedades".

import (
	"context"
	"time"
)

// SharedItemFromPeer empareja un resultado con el peer de origen.
type SharedItemFromPeer struct {
	Peer *Peer
	Item *SharedItem
}

// SearchLocalSharedItems responde busqueda inbound de un peer.
func (m *Manager) SearchLocalSharedItems(ctx context.Context, peerID, query string, limit int) ([]*SharedItem, error) {
	return m.repo.SearchSharedItems(ctx, peerID, query, limit)
}

// ListLocalRecentSharedItems responde "novedades" inbound de un peer.
func (m *Manager) ListLocalRecentSharedItems(ctx context.Context, peerID string, limit int) ([]*SharedItem, error) {
	return m.repo.ListRecentSharedItems(ctx, peerID, limit)
}

// SearchAllPeers hace fan-out de busqueda a todos los peers paired.
// Peer offline se loguea y se omite. perPeerTimeout acota la espera.
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

// RecentFromAllPeers hace fan-out de "novedades" a todos los peers.
// Alimenta el rail "Recien anadido en peers" del home.
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
