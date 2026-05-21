package federation_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain corre el package suite bajo `goleak.VerifyTestMain`. El paquete
// `federation` corre el `Auditor` (async writer del audit log) y el peer
// probe en background; ambos se cierran vía `Manager.Close`. goleak
// enforza que los tests llamen Close en vez de filtrar goroutines.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
