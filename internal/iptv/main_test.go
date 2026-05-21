package iptv_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain corre el package suite bajo `goleak.VerifyTestMain`. El paquete
// `iptv` corre varias goroutines de background: `Service.SpawnBackground`
// (auto-EPG / auto-probe tras import M3U, refresh async desde handlers
// admin), `TransmuxManager` (drainer de sesiones ffmpeg) y `Scheduler`
// (M3U + EPG refresh periódico). Todas drenan en `Shutdown` vía bgWG;
// goleak enforza que ningún test las deje vivas (audit olores DD/GGGG).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
