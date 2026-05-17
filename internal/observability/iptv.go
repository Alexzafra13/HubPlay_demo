package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// IPTVTransmuxSink: adapta Metrics a iptv.TransmuxMetrics (counters de
// transmux; el breaker Allow/RecordSuccess/Failure va por su lado).
//
// Tipo aparte en vez de methods en *Metrics para que el adapter sea explícito
// en el wiring y metrics.go quede como lista plana de collectors. Stateless.
type IPTVTransmuxSink struct {
	starts        *prometheus.CounterVec
	decodeMode    *prometheus.CounterVec
	reencodePromo prometheus.Counter
}

// NewIPTVTransmuxSink: nil Metrics → nil sink, así callers pueden pasar el
// resultado de un Metrics maybe-nil directo a TransmuxManagerConfig sin guard.
func NewIPTVTransmuxSink(m *Metrics) *IPTVTransmuxSink {
	if m == nil {
		return nil
	}
	return &IPTVTransmuxSink{
		starts:        m.IPTVTransmuxStarts,
		decodeMode:    m.IPTVTransmuxDecodeMode,
		reencodePromo: m.IPTVTransmuxReencodePromo,
	}
}

func (s *IPTVTransmuxSink) IncStarts(outcome string) {
	if s == nil {
		return
	}
	s.starts.WithLabelValues(outcome).Inc()
}

func (s *IPTVTransmuxSink) IncDecodeMode(mode string) {
	if s == nil {
		return
	}
	s.decodeMode.WithLabelValues(mode).Inc()
}

func (s *IPTVTransmuxSink) IncReencodePromotions() {
	if s == nil {
		return
	}
	s.reencodePromo.Inc()
}

// IPTVTransmuxSessions: vista mínima del TransmuxManager para el gauge de
// sesiones activas. Mismo patrón que KeyStoreCounts.
type IPTVTransmuxSessions interface {
	ActiveSessions() int
}

// RegisterIPTVTransmuxGauges: gauge-func que lee sesiones del manager en
// cada scrape. Error sólo si choca el nombre (mal uso en tests).
func RegisterIPTVTransmuxGauges(m *Metrics, t IPTVTransmuxSessions) error {
	if m == nil || t == nil {
		return nil
	}
	g := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "hubplay_iptv_transmux_active_sessions",
			Help: "Currently active IPTV transmux ffmpeg sessions.",
		},
		func() float64 { return float64(t.ActiveSessions()) },
	)
	return m.Registry.Register(g)
}
