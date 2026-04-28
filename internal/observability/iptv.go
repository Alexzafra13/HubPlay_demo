package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// IPTVTransmuxSink adapts the Metrics struct to the
// iptv.TransmuxMetrics interface (Allow / RecordSuccess / RecordFailure
// for the breaker is its own thing — this sink only handles the
// transmux-side counters defined in metrics.go).
//
// Kept as a separate type instead of methods on *Metrics so the
// adapter stays explicit at the wiring site and the metrics.go fields
// remain a flat list of collectors. The sink itself is stateless: it
// only forwards to the underlying counters.
type IPTVTransmuxSink struct {
	starts        *prometheus.CounterVec
	decodeMode    *prometheus.CounterVec
	reencodePromo prometheus.Counter
}

// NewIPTVTransmuxSink builds a sink that forwards to the IPTV transmux
// counters on the given Metrics. Returns nil if m is nil so callers
// can pass the result of a maybe-nil Metrics straight into
// TransmuxManagerConfig without a guard.
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

// IPTVTransmuxSessions is the minimal view of TransmuxManager the
// metrics layer needs to expose the active-sessions gauge at scrape
// time. Same pattern as KeyStoreCounts: the DB / runtime is source of
// truth, and a missed Set() during a future refactor would silently
// poison a dashboard.
type IPTVTransmuxSessions interface {
	ActiveSessions() int
}

// RegisterIPTVTransmuxGauges attaches the gauge-func that reads the
// transmux manager's session count live on every scrape. Returns an
// error only if the gauge name collides (test misuse).
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
