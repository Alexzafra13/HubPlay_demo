package stream_test

import (
	"testing"

	"hubplay/internal/testutil"
)

// Falla si una goroutine sobrevive a la suite — gate del patrón
// bgWG + Shutdown (olor Y del audit 2026-05-14).
func TestMain(m *testing.M) {
	testutil.RunWithGoleak(m)
}
