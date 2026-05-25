package federation_test

import (
	"testing"

	"hubplay/internal/testutil"
)

// Falla si una goroutine sobrevive a la suite — gate del Auditor +
// Manager.Close (olor Y del audit 2026-05-14).
func TestMain(m *testing.M) {
	testutil.RunWithGoleak(m)
}
