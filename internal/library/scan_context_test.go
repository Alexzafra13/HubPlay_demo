package library

import (
	"context"
	"testing"
	"time"
)

// scanContext must default to "no per-scan deadline" so a first scan of a
// very large library is never killed mid-index — it stays bounded only by
// bgCtx (shutdown). Previously a fixed 30-minute cap guaranteed failure on
// big libraries.
func TestScanContext_UnboundedByDefault(t *testing.T) {
	bg, bgCancel := context.WithCancel(context.Background())
	s := &Service{bgCtx: bg}

	sc, cancel := s.scanContext()
	defer cancel()

	if _, ok := sc.Deadline(); ok {
		t.Fatal("default scanContext must not carry a deadline")
	}

	// Cancelling bgCtx (shutdown) must still cancel the scan context.
	bgCancel()
	select {
	case <-sc.Done():
	case <-time.After(time.Second):
		t.Fatal("scanContext not cancelled when bgCtx is cancelled")
	}
}

func TestScanContext_AppliesConfiguredTimeout(t *testing.T) {
	s := &Service{bgCtx: context.Background()}
	s.SetScanTimeout(50 * time.Millisecond)

	sc, cancel := s.scanContext()
	defer cancel()

	if _, ok := sc.Deadline(); !ok {
		t.Fatal("expected a deadline when a scan timeout is configured")
	}
}
