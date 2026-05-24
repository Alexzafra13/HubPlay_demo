package testutil

import (
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// RunWithGoleak corre `m.Run()`, cierra el admin pool de Postgres (un
// singleton process-lifetime que sólo existe en runs con
// `HUBPLAY_TEST_DRIVER=postgres`), y verifica con `goleak.Find` antes
// de salir. Si hay leaks reales falla con exit code 1; si la suite
// falla, propaga el exit code de m.Run.
//
// Cierra el olor Y del audit 2026-05-14 con enforcement automático: si
// un test añade una goroutine de background y se olvida de drenarla,
// el build falla.
//
// Uso:
//
//	func TestMain(m *testing.M) {
//	    testutil.RunWithGoleak(m)
//	}
//
// Sin esto, `goleak.VerifyTestMain` directo fallaría bajo
// `HUBPLAY_TEST_DRIVER=postgres` por el admin pool — visible en CI
// como "Test Backend (Postgres) failure" desde la PR #403.
func RunWithGoleak(m *testing.M) {
	code := m.Run()
	closePostgresAdmin()
	if err := goleak.Find(); err != nil {
		fmt.Fprintln(os.Stderr, "goleak:", err)
		os.Exit(1)
	}
	os.Exit(code)
}
