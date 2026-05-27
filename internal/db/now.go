package db

import "time"

// timeNow es la fuente de tiempo del paquete `db`. Producción la
// invoca directamente (equivale a `time.Now`). Tests del paquete
// pueden swappar a un reloj controlado vía SetTimeNowForTest
// (definida en now_helpers_test.go para que el import de testing
// no contamine producción).
//
// Por qué no inyectar clock.Clock en cada constructor de repo:
// `NewRepositories` tiene 33 callsites en el codebase (main + tests
// + benchmarks); cambiar la API sería ruido masivo para resolver un
// problema acotado (algunas queries usan time.Now para
// `created_at`/`updated_at`/`last_probe_at`). El patrón
// "package-level seam" es idiomático en stdlib (`crypto/rand`,
// `os/user`) cuando el coste de DI desborda el beneficio.
//
// Trade-off: no es goroutine-safe. El helper de tests recomienda no
// usarse con t.Parallel(); para tests del paquete `db` no es
// problema porque cada uno crea su propia DB SQLite (los efectos
// sobre datos están aislados aunque la variable sea global).
var timeNow = time.Now
