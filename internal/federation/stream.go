package federation

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPeerStreamLimit is returned by AdmitPeerStream when a peer is
// already streaming MaxConcurrentStreamsPerPeer distinct items. The
// handler maps it to 429 + Retry-After so the peer backs off rather
// than spawning yet another transcode against our local budget.
var ErrPeerStreamLimit = errors.New("federation: peer concurrent stream limit reached")

// AdmitPeerStream enforces the per-peer concurrent-stream ceiling
// BEFORE a federated stream session is started (i.e. before a transcode
// spawns). It counts the DISTINCT items the peer currently has active
// in the session registry; re-requesting an item the peer is already
// streaming is always admitted, so retries / reconnects of an existing
// stream never trip the cap. Cap <= 0 means unlimited. F-2.
//
// There is a small TOCTOU window between this check and the subsequent
// RegisterPeerStreamSession (which happens after the transcode starts),
// but federation traffic is already token-bucket rate-limited per peer,
// so the worst case is one extra session past the cap under a tight
// race — acceptable for a resource-exhaustion ceiling.
func (m *Manager) AdmitPeerStream(peerID, itemID string) error {
	limit := m.cfg.MaxConcurrentStreamsPerPeer
	if limit <= 0 {
		return nil
	}
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	distinct := make(map[string]struct{})
	for _, s := range m.streamSessions {
		if s.PeerID != peerID {
			continue
		}
		if s.ItemID == itemID {
			return nil // already streaming this item — retries are free
		}
		distinct[s.ItemID] = struct{}{}
	}
	if len(distinct) >= limit {
		return ErrPeerStreamLimit
	}
	return nil
}

// PeerStreamSession is the bookkeeping entry for one active streaming
// session that originates from a paired peer browsing our catalog.
//
// We don't own the underlying transcode -- stream.Manager does. We
// only remember the (peerID, itemID, profile) tuple that the session
// UUID we minted maps to, so that subsequent HLS manifest + segment
// requests carrying that UUID can be routed back to the right
// stream.Manager session key.
//
// Lifecycle: created by RegisterPeerStreamSession when a peer hits
// POST /peer/stream/{itemId}/session, swept after peerStreamSessionTTL
// of inactivity by SweepStreamSessions.
type PeerStreamSession struct {
	ID         string    // opaque UUID we hand back to the requesting peer
	PeerID     string    // ID of the peer that started the session
	ItemID     string    // local item ID on this server (the source)
	Profile    string    // initial transcode profile name
	CreatedAt  time.Time // for TTL sweeps
	LastSeenAt time.Time // bumped on every manifest/segment touch
}

// peerStreamSessionTTL is the idle window after which a registered
// peer stream session is reclaimed. Generous enough that a buffering
// player or a brief network blip doesn't drop the mapping; tight
// enough that a malicious peer cannot accumulate dead entries.
const peerStreamSessionTTL = 5 * time.Minute

// RegisterPeerStreamSession records a fresh streaming session for a
// peer. The returned ID is what the peer sees in the master playlist
// URL it'll re-request for manifests + segments.
//
// Concurrency: the Manager's streamMu protects streamSessions.
// Callers don't need to hold any lock.
func (m *Manager) RegisterPeerStreamSession(peerID, itemID, profile string) *PeerStreamSession {
	id := uuid.NewString()
	now := m.clock.Now()
	s := &PeerStreamSession{
		ID:         id,
		PeerID:     peerID,
		ItemID:     itemID,
		Profile:    profile,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	m.streamMu.Lock()
	m.streamSessions[id] = s
	m.streamMu.Unlock()
	return s
}

// LookupPeerStreamSession returns the session for the given UUID and
// bumps its LastSeenAt. Returns nil when the session has been swept
// or never existed. Callers MUST verify peer.ID matches s.PeerID
// before serving any bytes -- the registry alone is not an
// authorisation check.
func (m *Manager) LookupPeerStreamSession(id string) *PeerStreamSession {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	s, ok := m.streamSessions[id]
	if !ok {
		return nil
	}
	s.LastSeenAt = m.clock.Now()
	return s
}

// SweepStreamSessions reclaims peer stream sessions idle past TTL.
// Idempotent and safe to call concurrently with Register/Lookup.
func (m *Manager) SweepStreamSessions() {
	cutoff := m.clock.Now().Add(-peerStreamSessionTTL)
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	for id, s := range m.streamSessions {
		if s.LastSeenAt.Before(cutoff) {
			delete(m.streamSessions, id)
		}
	}
}
