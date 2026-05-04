package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeStatsSource lets the gauge test pin known values into the
// FederationStatsSource interface so we don't need a real
// federation.Manager (which would pull half the package graph).
type fakeStatsSource struct {
	paired   int
	sessions int
	nonces   int
}

func (f *fakeStatsSource) PairedPeers() int        { return f.paired }
func (f *fakeStatsSource) PeerStreamSessions() int { return f.sessions }
func (f *fakeStatsSource) NonceCacheSize() int     { return f.nonces }

// TestRegisterFederationGauges_ReportsLiveValues makes sure the three
// gauges actually scrape the underlying stats source — a registration
// regression would silently freeze them at zero, exactly the kind of
// dashboard-rotting bug a unit test must catch.
func TestRegisterFederationGauges_ReportsLiveValues(t *testing.T) {
	m, err := NewMetrics("test")
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeStatsSource{paired: 3, sessions: 7, nonces: 42}
	if err := RegisterFederationGauges(m, src); err != nil {
		t.Fatalf("RegisterFederationGauges: %v", err)
	}

	assertGauge(t, m, "hubplay_federation_paired_peers", 3)
	assertGauge(t, m, "hubplay_federation_peer_stream_sessions", 7)
	assertGauge(t, m, "hubplay_federation_nonce_cache_size", 42)

	// Bump the source and re-scrape — values must follow without any
	// explicit Set() call. This is the entire point of using
	// GaugeFunc rather than a regular Gauge.
	src.paired = 4
	src.sessions = 0
	src.nonces = 100
	assertGauge(t, m, "hubplay_federation_paired_peers", 4)
	assertGauge(t, m, "hubplay_federation_peer_stream_sessions", 0)
	assertGauge(t, m, "hubplay_federation_nonce_cache_size", 100)
}

// TestRegisterFederationGauges_NilSrcIsNoop confirms the nil-guard.
// Wiring metrics for a federation-disabled startup must not panic
// nor pollute the registry.
func TestRegisterFederationGauges_NilSrcIsNoop(t *testing.T) {
	m, err := NewMetrics("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterFederationGauges(m, nil); err != nil {
		t.Fatalf("nil src must return nil: %v", err)
	}
}

// TestFederationSink_RecordsCounterAndHistogram verifies both label
// sets land on the right collectors. A label typo here would cost
// nothing at runtime but break every dashboard query — exactly what
// a unit test catches cheaply.
func TestFederationSink_RecordsCounterAndHistogram(t *testing.T) {
	m, err := NewMetrics("test")
	if err != nil {
		t.Fatal(err)
	}
	sink := NewFederationSink(m)

	sink.HandshakeDuration("outbound", "ok", 0.123)
	sink.HandshakeDuration("inbound", "error", 4.5)
	sink.OutboundRequest("libraries", "ok")
	sink.OutboundRequest("libraries", "ok")
	sink.OutboundRequest("stream_proxy", "5xx")

	if got := testutil.ToFloat64(m.FederationOutboundRequests.WithLabelValues("libraries", "ok")); got != 2 {
		t.Errorf("libraries/ok counter = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.FederationOutboundRequests.WithLabelValues("stream_proxy", "5xx")); got != 1 {
		t.Errorf("stream_proxy/5xx counter = %v, want 1", got)
	}

	// CollectAndCount returns the number of distinct (metric,
	// label-set) series — for a histogram this counts label-set
	// instances. Two calls with two distinct label sets → 2.
	if got := testutil.CollectAndCount(m.FederationHandshakeDuration); got != 2 {
		t.Errorf("handshake histogram series = %d, want 2", got)
	}
}

// TestFederationSink_NilMetricsReturnsNil keeps the constructor
// honest — a nil Metrics in test rigs must surface as nil sink
// rather than a sink that NPEs on first use.
func TestFederationSink_NilMetricsReturnsNil(t *testing.T) {
	if NewFederationSink(nil) != nil {
		t.Fatal("nil metrics must yield nil sink")
	}
}

// assertGauge gathers the registry and asserts the named gauge
// reports `want`. Uses the protobuf gather path so GaugeFunc
// callbacks fire (testutil.ToFloat64 only works on Gauge values
// with a known label set, not on bare GaugeFunc collectors).
func assertGauge(t *testing.T, m *Metrics, name string, want float64) {
	t.Helper()
	mfs, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if metric.Gauge != nil {
				if metric.Gauge.GetValue() != want {
					t.Errorf("gauge %s: got %v, want %v", name, metric.Gauge.GetValue(), want)
				}
				return
			}
		}
	}
	t.Errorf("gauge %s not found in registry", name)
}
