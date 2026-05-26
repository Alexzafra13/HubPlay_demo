package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestDisableWriteDeadline_RecorderReturnsUnsupported pins the
// helper's behaviour against a ResponseWriter that doesn't
// implement SetWriteDeadline. httptest.NewRecorder is exactly
// that case (tests that don't reach the real http.Server). The
// caller can safely ignore the returned error — the worst case
// is "WriteTimeout 30s default applies", which is a sane fallback,
// not a crash.
func TestDisableWriteDeadline_RecorderReturnsUnsupported(t *testing.T) {
	rr := httptest.NewRecorder()
	err := DisableWriteDeadline(rr)
	if err == nil {
		t.Fatal("expected ErrNotSupported from a non-deadline ResponseWriter; got nil")
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("got %v, want errors.ErrUnsupported", err)
	}
}

// TestDisableWriteDeadline_OnRealServer wires the helper into a
// real http.Server with a tight WriteTimeout and verifies the
// handler can write past it. This is the contract the production
// configuration relies on: WriteTimeout: 30s + opt-out on
// streaming handlers = streams aren't killed mid-flight.
func TestDisableWriteDeadline_OnRealServer(t *testing.T) {
	// Server with an aggressively short WriteTimeout to make the
	// test deterministic. If the helper didn't clear the deadline,
	// the second Write would error and the test would notice.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := DisableWriteDeadline(w); err != nil {
			t.Errorf("DisableWriteDeadline: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("first ")); err != nil {
			t.Errorf("first write: %v", err)
			return
		}
		// Sleep LEGÍTIMO (F15-1 batch 4): la unidad bajo test es
		// DisableWriteDeadline. El test debe pasar más tiempo del
		// WriteTimeout (50ms) para verificar que el deadline NO
		// dispara — esa es la aserción central. Sin retraso real no
		// estamos testeando nada.
		time.Sleep(150 * time.Millisecond)
		if _, err := w.Write([]byte("second")); err != nil {
			t.Errorf("second write past short deadline: %v", err)
			return
		}
	}))
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
