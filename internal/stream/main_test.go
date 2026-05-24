package stream_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain corre el package suite bajo `goleak.VerifyTestMain`. Cualquier
// goroutine que sobreviva a la suite (e.g. un `Manager.cleanupLoop` que
// no se haya parado con `Shutdown`, o un transcoder con `ffmpeg` sin
// `Stop`) falla el build. Es la enforcement automática del patrón
// `bgWG + Shutdown` que cierra los olores Y/DD/GGGG/RR del audit
// 2026-05-14 — sin esto, una regresión queda invisible hasta que un
// operador note "sql: database is closed" en logs de producción.
//
// `IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start")` no
// hace falta aquí: el paquete no usa opencensus. Si una dependencia
// futura mete una goroutine permanente del runtime, se añade
// `goleak.IgnoreTopFunction("<pkg>.<func>")` por ese sitio concreto,
// nunca un wildcard global.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
