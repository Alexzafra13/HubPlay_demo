package iptv_test

import (
	"testing"

	"hubplay/internal/testutil"
)

// Falla si una goroutine sobrevive a la suite — gate del patrón
// SpawnBackground + Shutdown (olores DD/GGGG del audit 2026-05-14).
func TestMain(m *testing.M) {
	testutil.RunWithGoleak(m)
}
