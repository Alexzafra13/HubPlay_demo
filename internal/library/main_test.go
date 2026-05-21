package library_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain corre el package suite bajo `goleak.VerifyTestMain`. El paquete
// `library` contiene varios subscribers del event bus (`SegmentDetector`,
// `SegmentFingerprinter`, schedulers de scan / image refresh) cada uno
// con su `bgWG` y `unsub`. Si un test crea uno sin defer del unsub, o si
// un `Shutdown` ignora una goroutine, esta enforcement hace que falle el
// build en vez de filtrarse a producción como "sql: database is closed"
// en logs de shutdown (audit olor Y).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
