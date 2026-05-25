package federation

import (
	"time"

	"github.com/google/uuid"
)

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

// peerStreamSessionTTL: ventana idle antes de reclamar la sesion.
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

// SweepStreamSessions reclama sesiones idle pasado TTL. Idempotente.
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

