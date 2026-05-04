package federation

// MetricsSink is the small observability surface the Manager calls into
// for counters and histograms. Gauges go via the Stats() snapshot
// pulled at scrape time so we cannot drift out of sync with reality.
//
// Declared in this package (rather than imported from observability)
// so internal/observability can depend on internal/federation but not
// the other way around — the existing project anti-cycle pattern, see
// internal/stream/manager.go::MetricsSink for the prior art.
type MetricsSink interface {
	// HandshakeDuration records one outbound or inbound handshake
	// attempt. direction is "outbound" (we initiated AcceptInvite) or
	// "inbound" (peer hit our /peer/handshake). outcome is "ok" or
	// "error".
	HandshakeDuration(direction, outcome string, seconds float64)

	// OutboundRequest counts one server-to-server peer call. kind is
	// the call category ("libraries", "items", "stream_session",
	// "stream_proxy"). outcome is "ok", "4xx", "5xx", or
	// "transport_error".
	OutboundRequest(kind, outcome string)
}

// noopMetricsSink is the default when no metrics are wired (tests,
// federation-disabled startup). Every method is a no-op so the
// Manager doesn't need nil checks at every call site.
type noopMetricsSink struct{}

func (noopMetricsSink) HandshakeDuration(string, string, float64) {}
func (noopMetricsSink) OutboundRequest(string, string)            {}

// SetMetricsSink swaps in a real sink. Safe to call once at startup;
// not concurrent with traffic. Passing nil restores the no-op sink.
func (m *Manager) SetMetricsSink(s MetricsSink) {
	if s == nil {
		m.metrics = noopMetricsSink{}
		return
	}
	m.metrics = s
}

// Stats is the live snapshot the observability layer reads at scrape
// time. Sourced entirely from in-memory state — no DB call — so a
// scrape adds zero IO load. Revoked / pending peer counts are not
// included because they live in the DB; admins who need those run
// a SQL query against federation_peers directly.
type Stats struct {
	PairedPeers        int
	PeerStreamSessions int
	NonceCacheSize     int
}

// PairedPeers reports the number of currently paired peers (in-memory
// cache size). Backs the hubplay_federation_paired_peers gauge.
func (m *Manager) PairedPeers() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peerCache)
}

// PeerStreamSessions reports the number of active inbound peer stream
// sessions tracked by this server. Backs the
// hubplay_federation_peer_stream_sessions gauge.
func (m *Manager) PeerStreamSessions() int {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	return len(m.streamSessions)
}

// NonceCacheSize reports replay-protection nonces currently held in
// memory. Backs the hubplay_federation_nonce_cache_size gauge.
func (m *Manager) NonceCacheSize() int {
	if m.nonces == nil {
		return 0
	}
	return m.nonces.size()
}

// Stats returns the current observability snapshot. Reads each of the
// manager's internal collections under their respective locks; never
// blocks federation traffic for more than a microsecond.
func (m *Manager) Stats() Stats {
	out := Stats{}
	m.mu.RLock()
	out.PairedPeers = len(m.peerCache)
	m.mu.RUnlock()

	m.streamMu.Lock()
	out.PeerStreamSessions = len(m.streamSessions)
	m.streamMu.Unlock()

	if m.nonces != nil {
		out.NonceCacheSize = m.nonces.size()
	}
	return out
}
