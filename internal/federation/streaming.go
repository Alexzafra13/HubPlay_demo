package federation

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// PeerStreamSession is the bookkeeping handle for an open stream
// session opened on behalf of a peer's user. The Manager tracks one
// per (peer × remote_user_id × item × profile) combination and uses
// the count per-peer for the concurrency gate.
//
// SessionID is the opaque token returned to the calling peer so the
// peer's own clients can address the variant playlists and segments
// without leaking the underlying stream.Manager session key.
type PeerStreamSession struct {
	SessionID    string
	PeerID       string
	RemoteUserID string
	ItemID       string
	Profile      string
	StartedAt    time.Time
}

// peerStreamGate counts active stream sessions per peer and gates new
// ones at a configurable cap. Separate from stream.Manager's global
// cap so a peer hostile enough to ignore ratelimit can't drain the
// transcode budget meant for local users.
//
// Concurrency: a single mutex covers both the count map and the
// session map. Federation sessions are infrequent (humans clicking
// play) and per-session work dominates, so a global lock is fine.
type peerStreamGate struct {
	cap int

	mu       sync.Mutex
	counts   map[string]int                 // peer_id → live session count
	sessions map[string]*PeerStreamSession  // session_id → session
	byKey    map[string]string              // peerID:remoteUser:itemID:profile → sessionID (de-dupe)
}

func newPeerStreamGate(cap int) *peerStreamGate {
	return &peerStreamGate{
		cap:      cap,
		counts:   make(map[string]int),
		sessions: make(map[string]*PeerStreamSession),
		byKey:    make(map[string]string),
	}
}

// open allocates a session (or returns the existing one if the same
// (peer, user, item, profile) combination is already streaming —
// shared sessions follow the local stream.Manager pattern). Returns
// nil + false when the per-peer cap would be exceeded.
//
// The caller is responsible for invoking close when the underlying
// stream is torn down (idle reap, explicit DELETE). A leaked open
// without a matching close eventually consumes the cap; the periodic
// sweep in sweepIdle is the safety net.
func (g *peerStreamGate) open(peerID, remoteUserID, itemID, profile string, now time.Time) (*PeerStreamSession, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	dedupeKey := peerID + ":" + remoteUserID + ":" + itemID + ":" + profile
	if existingID, ok := g.byKey[dedupeKey]; ok {
		if s := g.sessions[existingID]; s != nil {
			return s, true
		}
	}
	if g.cap > 0 && g.counts[peerID] >= g.cap {
		return nil, false
	}
	id := newSessionID()
	s := &PeerStreamSession{
		SessionID:    id,
		PeerID:       peerID,
		RemoteUserID: remoteUserID,
		ItemID:       itemID,
		Profile:      profile,
		StartedAt:    now,
	}
	g.sessions[id] = s
	g.byKey[dedupeKey] = id
	g.counts[peerID]++
	return s, true
}

// get retrieves a session by ID. Returns nil when unknown — the
// handler maps that to 404. Read-only; safe to call from multiple
// goroutines.
func (g *peerStreamGate) get(sessionID string) *PeerStreamSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessions[sessionID]
}

// close releases the per-peer counter and forgets the session. Safe
// to call multiple times — the second call is a no-op.
func (g *peerStreamGate) close(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.sessions[sessionID]
	if s == nil {
		return
	}
	delete(g.sessions, sessionID)
	delete(g.byKey, s.PeerID+":"+s.RemoteUserID+":"+s.ItemID+":"+s.Profile)
	if g.counts[s.PeerID] > 0 {
		g.counts[s.PeerID]--
	}
}

// sweepIdle drops sessions older than maxAge. Called periodically as
// the safety net for leaked opens (a peer client that crashes mid-
// playback never sends DELETE; without this the per-peer cap leaks).
//
// Callers MUST also tear down the underlying stream.Manager session
// for the dropped IDs — this gate only owns its own bookkeeping.
// Returns the dropped session IDs so the caller can do that.
func (g *peerStreamGate) sweepIdle(now time.Time, maxAge time.Duration) []*PeerStreamSession {
	g.mu.Lock()
	defer g.mu.Unlock()
	var dropped []*PeerStreamSession
	cutoff := now.Add(-maxAge)
	for id, s := range g.sessions {
		if s.StartedAt.Before(cutoff) {
			dropped = append(dropped, s)
			delete(g.sessions, id)
			delete(g.byKey, s.PeerID+":"+s.RemoteUserID+":"+s.ItemID+":"+s.Profile)
			if g.counts[s.PeerID] > 0 {
				g.counts[s.PeerID]--
			}
		}
	}
	return dropped
}

// peerStreamMaxAge is the safety-net cutoff for sweepIdle. A real
// stream session in flight will refresh its underlying
// stream.Manager session via segment requests; a session sitting at
// 4 hours has almost certainly been abandoned. Local stream sessions
// idle-reap after 5 min by default; we're more generous because peer
// flows have an extra hop's worth of latency uncertainty.
const peerStreamMaxAge = 4 * time.Hour

// PeerStreamCount returns the current open-session count for a peer.
// Test + audit surface; not used in hot paths.
func (m *Manager) PeerStreamCount(peerID string) int {
	if m == nil || m.streams == nil {
		return 0
	}
	m.streams.mu.Lock()
	defer m.streams.mu.Unlock()
	return m.streams.counts[peerID]
}

// OpenPeerStream allocates a peer-streaming session. Returns
// domain.ErrPeerScopeInsufficient when the per-peer cap is full —
// the calling peer's HTTP client should map that to a user-facing
// "too many concurrent streams from your server" message.
//
// The session is tied to a (peer, remote_user_id, item, profile)
// combination. Re-requests for the same combination return the
// existing session id (matches stream.Manager's local de-duplication
// behaviour).
func (m *Manager) OpenPeerStream(peerID, remoteUserID, itemID, profile string) (*PeerStreamSession, bool) {
	if m == nil || m.streams == nil {
		return nil, false
	}
	return m.streams.open(peerID, remoteUserID, itemID, profile, m.clock.Now())
}

// GetPeerStream returns the session for a sessionID, or nil if
// unknown / already closed.
func (m *Manager) GetPeerStream(sessionID string) *PeerStreamSession {
	if m == nil || m.streams == nil {
		return nil
	}
	return m.streams.get(sessionID)
}

// ClosePeerStream releases the per-peer cap and forgets the session.
// Idempotent. Wired to the inbound DELETE peer endpoint and to the
// idle sweep.
func (m *Manager) ClosePeerStream(sessionID string) {
	if m == nil || m.streams == nil {
		return
	}
	m.streams.close(sessionID)
}

// SweepIdlePeerStreams runs the safety-net cutoff. Returns the
// dropped sessions so the caller can also tear down the underlying
// stream.Manager sessions for them.
func (m *Manager) SweepIdlePeerStreams() []*PeerStreamSession {
	if m == nil || m.streams == nil {
		return nil
	}
	return m.streams.sweepIdle(m.clock.Now(), peerStreamMaxAge)
}

// newSessionID returns a fresh opaque hex token. 16 random bytes →
// 32 hex chars. Plenty of entropy; short enough to fit URLs.
func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is exotic; fall back to a timestamp-
		// derived id so the call site still returns a usable token.
		return "ses-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}
