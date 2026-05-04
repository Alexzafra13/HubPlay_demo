package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// FederationStatsSource is the minimal view of federation.Manager the
// observability layer needs to read live gauge values. Declared with
// three plain int accessors (rather than a shared struct) so the
// federation package does not need to import observability — the
// dependency arrow stays one-way: observability → federation.
type FederationStatsSource interface {
	PairedPeers() int
	PeerStreamSessions() int
	NonceCacheSize() int
}

// RegisterFederationGauges wires three GaugeFuncs sourced live from
// the manager. GaugeFunc avoids the drift risk of a regular Gauge we
// would have to .Set() after every mutation — the manager's in-memory
// state is the source of truth, queried once per scrape.
//
// Returns an error on duplicate registration (typically a test misuse).
// Safe to pass nil src or nil m: both short-circuit to no-op.
func RegisterFederationGauges(m *Metrics, src FederationStatsSource) error {
	if m == nil || src == nil {
		return nil
	}

	pairedPeers := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "hubplay_federation_paired_peers",
			Help: "Number of currently paired federation peers (in-memory cache size).",
		},
		func() float64 { return float64(src.PairedPeers()) },
	)
	streamSessions := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "hubplay_federation_peer_stream_sessions",
			Help: "Number of active inbound peer stream sessions on this server.",
		},
		func() float64 { return float64(src.PeerStreamSessions()) },
	)
	nonceCacheSize := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "hubplay_federation_nonce_cache_size",
			Help: "Tracked replay-protection nonces currently held in memory.",
		},
		func() float64 { return float64(src.NonceCacheSize()) },
	)

	for _, c := range []prometheus.Collector{pairedPeers, streamSessions, nonceCacheSize} {
		if err := m.Registry.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// FederationSink adapts Metrics to the federation.MetricsSink
// interface, mirroring the StreamSink pattern. Kept in this package
// so the federation package remains free of Prometheus.
type FederationSink struct {
	m *Metrics
}

// NewFederationSink builds a sink backed by the given Metrics. Passing
// a nil Metrics returns a nil sink; the federation Manager's
// SetMetricsSink replaces a nil with its own no-op.
func NewFederationSink(m *Metrics) *FederationSink {
	if m == nil {
		return nil
	}
	return &FederationSink{m: m}
}

func (s *FederationSink) HandshakeDuration(direction, outcome string, seconds float64) {
	s.m.FederationHandshakeDuration.WithLabelValues(direction, outcome).Observe(seconds)
}

func (s *FederationSink) OutboundRequest(kind, outcome string) {
	s.m.FederationOutboundRequests.WithLabelValues(kind, outcome).Inc()
}
