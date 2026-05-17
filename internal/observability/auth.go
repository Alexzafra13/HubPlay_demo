package observability

import (
	"github.com/prometheus/client_golang/prometheus"
)

// KeyStoreCounts: vista mínima de auth.KeyStore que el metrics layer necesita.
// Declarada aquí para evitar `observability → auth` (sería ciclo inverso).
type KeyStoreCounts interface {
	ActiveCount() int
	RetiredCount() int
}

// RegisterKeyStoreGauges: dos gauges leídos en vivo del keystore en cada
// scrape. GaugeFunc (en vez de Gauge + Set() tras cada mutación) elimina el
// riesgo de drift — un Set() olvidado en un refactor futuro envenenaría el
// dashboard en silencio.
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
