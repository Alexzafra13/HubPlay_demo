package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// FederationStatsSource: vista mínima de federation.Manager con 3 accessors
// para mantener el arrow one-way (observability → federation).
type FederationStatsSource interface {
	PairedPeers() int
	PeerStreamSessions() int
	NonceCacheSize() int
}

// RegisterFederationGauges: 3 GaugeFuncs leídos en vivo del manager.
// GaugeFunc evita drift por Set() olvidado en refactors. nil src/m → no-op.
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

// FederationSink: adapta Metrics a federation.MetricsSink. Vive aquí para
// que federation no importe Prometheus (espejo del patrón de StreamSink).
type FederationSink struct {
	m *Metrics
}

// NewFederationSink: nil Metrics → nil sink (federation.Manager.SetMetricsSink
// lo reemplaza por su propio no-op).
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
