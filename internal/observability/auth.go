package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// KeyStoreCounts is the minimal view of auth.KeyStore that the metrics
// layer needs to expose gauge values at scrape time. Declaring it here
// avoids pulling `internal/auth` into `internal/observability`, which
// would be a loop in reverse; callers pass their real keystore.
type KeyStoreCounts interface {
	ActiveCount() int
	RetiredCount() int
}

// RegisterKeyStoreGauges attaches two gauges that read live from the
// keystore every time Prometheus scrapes. Using GaugeFunc (rather than a
// regular Gauge we Set() after every mutation) means we cannot drift out
// of sync with reality: the DB is source of truth, and a missed Set()
// during a future refactor would silently poison a dashboard.
//
// Returns an error if registration fails (duplicate metric name).
func RegisterKeyStoreGauges(m *Metrics, ks KeyStoreCounts) error {
	if m == nil || ks == nil {
		return nil
	}

	active := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "hubplay_auth_signing_keys",
			Help:        "Number of JWT signing keys, partitioned by state.",
			ConstLabels: prometheus.Labels{"state": "active"},
		},
		func() float64 { return float64(ks.ActiveCount()) },
	)
	retired := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "hubplay_auth_signing_keys",
			Help:        "Number of JWT signing keys, partitioned by state.",
			ConstLabels: prometheus.Labels{"state": "retired"},
		},
		func() float64 { return float64(ks.RetiredCount()) },
	)

	if err := m.Registry.Register(active); err != nil {
		return err
	}
	if err := m.Registry.Register(retired); err != nil {
		return err
	}
	return nil
}
