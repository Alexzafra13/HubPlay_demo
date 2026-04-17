package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/event"
	"hubplay/internal/testutil"
)

// SSE handler is exercised against a real event.Bus because the subscribe /
// unsubscribe lifecycle is the critical behaviour. Mocking the bus would hide
// what we care about: no leaked handlers after the client disconnects.

func newSSETestServer(t *testing.T) (*event.Bus, *httptest.Server) {
	t.Helper()
	bus := event.NewBus(testutil.NopLogger())
	h := NewEventHandler(bus, testutil.NopLogger())
	srv := httptest.NewServer(http.HandlerFunc(h.Stream))
	t.Cleanup(srv.Close)
	return bus, srv
}

func TestEventHandler_Stream_DeliversPublishedEvent(t *testing.T) {
	bus, srv := newSSETestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type: %q", ct)
	}

	// Give the handler time to register its subscriptions, then publish.
	waitForHandlerCount(t, bus, event.ItemAdded, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		bus.Publish(event.Event{Type: event.ItemAdded, Data: map[string]any{"id": "42"}})
	}()

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	var saw string
	for time.Now().Before(deadline) {
		resp.Body = deadlineBody{r: resp.Body, until: time.Now().Add(200 * time.Millisecond)}
		line, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		saw += line
		if strings.Contains(saw, `"id":"42"`) {
			return
		}
	}
	t.Fatalf("published event not observed within 2s; buffer: %q", saw)
}

// deadlineBody is unused but kept as a no-op wrapper; Read() just delegates.
// The actual deadline logic lives in the client.Timeout + ctx cancel above.
type deadlineBody struct {
	r     interface{ Read([]byte) (int, error) }
	until time.Time
}

func (d deadlineBody) Read(p []byte) (int, error) { return d.r.Read(p) }
func (d deadlineBody) Close() error {
	if c, ok := d.r.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

func TestEventHandler_Stream_InitialHelloComment(t *testing.T) {
	_, srv := newSSETestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if n == 0 {
		t.Fatal("no initial bytes")
	}
	if !strings.Contains(string(buf[:n]), ":") {
		t.Errorf("expected SSE comment line starting with ':', got %q", string(buf[:n]))
	}
}

func TestEventHandler_Stream_UnsubscribesOnClientDisconnect(t *testing.T) {
	bus, srv := newSSETestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Handler subscribes to 12 event types.
	waitForHandlerCount(t, bus, event.ItemAdded, 1)
	if got := bus.HandlerCount(event.TranscodeStarted); got != 1 {
		t.Errorf("TranscodeStarted count before cancel: %d", got)
	}

	// Disconnect.
	cancel()
	_ = resp.Body.Close()

	// Wait for the handler goroutine to notice and unregister.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if bus.HandlerCount(event.ItemAdded) == 0 &&
			bus.HandlerCount(event.TranscodeStarted) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("handlers still registered after disconnect: ItemAdded=%d TranscodeStarted=%d",
		bus.HandlerCount(event.ItemAdded), bus.HandlerCount(event.TranscodeStarted))
}

// waitForHandlerCount polls bus.HandlerCount until it matches want, or fails.
func waitForHandlerCount(t *testing.T, bus *event.Bus, et event.Type, want int) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if bus.HandlerCount(et) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited for %s count=%d, got %d", et, want, bus.HandlerCount(et))
}
