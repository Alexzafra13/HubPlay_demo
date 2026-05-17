// Package observability: registry Prometheus + métricas que expone /metrics.
//
// Decisiones:
//   - Registry dedicado (no DefaultRegisterer) para aislar tests paralelos.
//   - Labels low-cardinality. La label `route` es chi RoutePattern
//     ("/libraries/{id}"), nunca el path crudo — el crudo crea 1 serie por
//     item id y revienta el storage.
//   - Buckets de histogram a mano para una API HTTP (sub-5ms hasta 10s); los
//     default (0.005..10) malgastan resolución en ambos extremos.
//   - Collectors expuestos como struct fields tipados — no map lookup ni
//     indirección por string.
package observability

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics: collectors Prometheus. Registry privado — tests y prod no
// comparten estado.
type Metrics struct {
	Registry *prometheus.Registry

	BuildInfo *prometheus.GaugeVec

	HTTPRequests        *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPErrors          *prometheus.CounterVec

	StreamActiveSessions  prometheus.Gauge
	StreamTranscodeStarts *prometheus.CounterVec

	IPTVTransmuxStarts        *prometheus.CounterVec
	IPTVTransmuxDecodeMode    *prometheus.CounterVec
	IPTVTransmuxReencodePromo prometheus.Counter

	AuthKeyRotations *prometheus.CounterVec

	FederationHandshakeDuration *prometheus.HistogramVec
	FederationOutboundRequests  *prometheus.CounterVec
}

// NewMetrics: crea y registra todos los collectors. Error si choca un nombre
// (típicamente mal uso en tests).
func NewMetrics(version string) (*Metrics, error) {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Registry: reg,

		BuildInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hubplay_build_info",
				Help: "Build information (version, go version). Always 1.",
			},
			[]string{"version", "go_version"},
		),

		HTTPRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_http_requests_total",
				Help: "Total HTTP requests handled, partitioned by method, route template and status.",
			},
			[]string{"method", "route", "status"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "hubplay_http_request_duration_seconds",
				Help: "HTTP request duration, in seconds.",
				// Health checks cacheados sub-ms hasta espera del primer
				// manifest de transcode ~10s.
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "route"},
		),

		HTTPErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_http_errors_total",
				Help: "HTTP errors grouped by AppError code (low cardinality by design).",
			},
			[]string{"code"},
		),

		StreamActiveSessions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "hubplay_stream_active_sessions",
				Help: "Number of currently active streaming sessions (direct play + transcode).",
			},
		),

		StreamTranscodeStarts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_stream_transcode_starts_total",
				Help: "Transcode session start attempts grouped by outcome.",
			},
			// outcome: "started", "busy", "failed"
			[]string{"outcome"},
		),

		IPTVTransmuxStarts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_iptv_transmux_starts_total",
				Help: "IPTV transmux spawn attempts grouped by outcome (ok, crash, gate_denied, busy).",
			},
			[]string{"outcome"},
		),

		IPTVTransmuxDecodeMode: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_iptv_transmux_decode_mode_total",
				Help: "Decode mode chosen for each spawned transmux session (direct, reencode).",
			},
			[]string{"mode"},
		),

		IPTVTransmuxReencodePromo: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "hubplay_iptv_transmux_reencode_promotions_total",
				Help: "Channels auto-promoted from `-c copy` to re-encode after a codec-related crash.",
			},
		),

		AuthKeyRotations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_auth_key_rotations_total",
				Help: "JWT signing key rotations, grouped by outcome.",
			},
			// outcome: "success", "error"
			[]string{"outcome"},
		),

		FederationHandshakeDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "hubplay_federation_handshake_duration_seconds",
				Help: "Outbound + inbound federation handshake latency, by direction and outcome.",
				// Más amplio que HTTP — handshakes hacen crypto + DB + ≥1
				// round-trip; sub-50ms = peer LAN, multi-segundo = WAN lento.
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			},
			// direction: "outbound" | "inbound"; outcome: "ok" | "error"
			[]string{"direction", "outcome"},
		),

		FederationOutboundRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hubplay_federation_outbound_requests_total",
				Help: "Outbound peer-to-peer requests grouped by call kind and outcome.",
			},
			// kind: "libraries" | "items" | "stream_session" | "stream_proxy"
			// outcome: "ok" | "4xx" | "5xx" | "transport_error"
			[]string{"kind", "outcome"},
		),
	}

	// Registra todo. Fallo (p.ej. choque de nombre en tests) lo devolvemos
	// para que el caller falle al boot.
	for _, c := range []prometheus.Collector{
		m.BuildInfo,
		m.HTTPRequests,
		m.HTTPRequestDuration,
		m.HTTPErrors,
		m.StreamActiveSessions,
		m.StreamTranscodeStarts,
		m.IPTVTransmuxStarts,
		m.IPTVTransmuxDecodeMode,
		m.IPTVTransmuxReencodePromo,
		m.AuthKeyRotations,
		m.FederationHandshakeDuration,
		m.FederationOutboundRequests,
		// Process + Go runtime — gratis y universalmente útil (goroutines, gc
		// pauses, fds).
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}

	// build_info: la presencia es la señal, el valor siempre 1. Particionar
	// por version permite que las alertas de Grafana detecten rollouts.
	m.BuildInfo.WithLabelValues(version, runtime.Version()).Set(1)

	return m, nil
}
