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
	// HandshakeDuration registra un intento de handshake.
	HandshakeDuration(direction, outcome string, seconds float64)

	// OutboundRequest cuenta una llamada server-to-server.
	OutboundRequest(kind, outcome string)
}

// noopMetricsSink: default sin metricas. No-op en cada metodo.
type noopMetricsSink struct{}

func (noopMetricsSink) HandshakeDuration(string, string, float64) {}
func (noopMetricsSink) OutboundRequest(string, string)            {}

// SetMetricsSink inyecta un sink real. Una vez en startup.
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

// PairedPeers: numero de peers paired (tamano de cache in-memory).
func (m *Manager) PairedPeers() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peerCache)
}

// PeerStreamSessions: sesiones de streaming inbound activas.
func (m *Manager) PeerStreamSessions() int {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	return len(m.streamSessions)
}

// NonceCacheSize: nonces anti-replay en memoria.
func (m *Manager) NonceCacheSize() int {
	if m.nonces == nil {
		return 0
	}
	return m.nonces.size()
}

// Stats devuelve el snapshot de observabilidad actual.
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
