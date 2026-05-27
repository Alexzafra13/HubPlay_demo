package db

import (
	"testing"
	"time"
)

// SetTimeNowForTest swappa la fuente de tiempo del paquete `db`
// durante el test y la restaura automáticamente en t.Cleanup.
// Sólo accesible desde tests del propio paquete `db` (vive en un
// fichero `_test.go`).
//
// No usar con t.Parallel(): timeNow es una variable global y los
// tests paralelos se condicionarían entre sí.
func SetTimeNowForTest(t *testing.T, fn func() time.Time) {
	t.Helper()
	prev := timeNow
	timeNow = fn
	t.Cleanup(func() { timeNow = prev })
}
