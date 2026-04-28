// Package observability owns the Prometheus registry and the metrics exposed
// at /metrics.
//
// Design decisions:
//
//   - A dedicated registry (not prometheus.DefaultRegisterer) keeps tests
//     isolated: each Metrics instance is self-contained so a parallel test
//     cannot accidentally collide with another's counter state.
//   - Labels are kept strictly low-cardinality. The HTTP route label is the
//     chi route pattern ("/libraries/{id}"), never the raw path, because the
//     raw path produces one time series per item id and blows up storage.
//   - Histogram buckets are hand-picked for an HTTP API (sub-5ms up to 10s);
//     defaults (0.005..10) are fine for general RPCs but waste resolution at
//     both ends for our traffic mix (sub-ms health checks vs long transcodes).
//   - Collectors are created at construction time and exposed as typed
//     struct fields — callers call metrics.HTTPRequests.WithLabelValues(...).Inc()
//     directly, no map lookups, no string-keyed indirection.
package observability

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics is the collection of Prometheus collectors used across HubPlay.
// It owns a private registry so tests and production do not share state.
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
}

// NewMetrics creates and registers every collector. It returns an error if
// any registration fails (duplicate name, typically a test misuse).
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
				// Buckets tuned for an HTTP API: very fast (cached health
				// checks) to very slow (first transcode manifest wait ~10s).
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
	}

	// Register everything. Any failure (e.g. name collision in tests) is
	// returned so callers can decide to fail fast at startup.
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
		// Also surface process + Go runtime metrics — free and universally
		// useful (goroutines, gc pauses, fds).
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}

	// Set the build info gauge once; its presence is the signal, the value is
	// always 1. Partitioning by version lets Grafana alerts notice rollouts.
	m.BuildInfo.WithLabelValues(version, runtime.Version()).Set(1)

	return m, nil
}
