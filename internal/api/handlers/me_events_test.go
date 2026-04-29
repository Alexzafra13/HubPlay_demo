package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/event"
	"hubplay/internal/testutil"
)

// me_events tests cover the user-scope filter + the unsubscribe
// lifecycle. The cross-cutting "is the JSON shape right" guarantee is
// the same as the global SSE handler (events_test.go) so we don't
// re-test the wire format here.

func newMeEventsTestServer(t *testing.T, claims *auth.Claims) (*event.Bus, *httptest.Server) {
	t.Helper()
	bus := event.NewBus(testutil.NopLogger())
	h := NewMeEventsHandler(bus, testutil.NopLogger())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject the test claims into the request context — the
		// handler reads them via auth.GetClaims, same path the real
		// middleware fills.
		if claims != nil {
			r = r.WithContext(auth.WithClaims(r.Context(), claims))
		}
		h.Stream(w, r)
	}))
	t.Cleanup(srv.Close)
	return bus, srv
}

func TestMeEvents_RejectsUnauthenticated(t *testing.T) {
	_, srv := newMeEventsTestServer(t, nil) // no claims
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
}

func TestMeEvents_DeliversOwnEvents(t *testing.T) {
	bus, srv := newMeEventsTestServer(t, &auth.Claims{UserID: "u-1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Wait for the handler to register all 3 user-scoped subscriptions.
	waitForHandlerCount(t, bus, event.ProgressUpdated, 1)
	waitForHandlerCount(t, bus, event.PlayedToggled, 1)
	waitForHandlerCount(t, bus, event.FavoriteToggled, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		bus.Publish(event.Event{
			Type: event.ProgressUpdated,
			Data: map[string]any{"user_id": "u-1", "item_id": "it-7", "position_ticks": int64(123)},
		})
	}()

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	var saw string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err == nil {
			saw += line
		}
		if strings.Contains(saw, `"item_id":"it-7"`) {
			return
		}
	}
	t.Fatalf("own event not observed within 2s; buffer: %q", saw)
}

// The user-scope filter is the security boundary. Other users'
// events must NOT show up in the stream — the test deliberately
// publishes one event for u-2 and one for u-1 in that order, then
// asserts the u-2 event is absent and the u-1 event present.
func TestMeEvents_DropsEventsForOtherUsers(t *testing.T) {
	bus, srv := newMeEventsTestServer(t, &auth.Claims{UserID: "u-1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	waitForHandlerCount(t, bus, event.FavoriteToggled, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		// Other user — must be dropped.
		bus.Publish(event.Event{
			Type: event.FavoriteToggled,
			Data: map[string]any{"user_id": "u-2", "item_id": "leak-attempt", "is_favorite": true},
		})
		// Same user — must arrive.
		bus.Publish(event.Event{
			Type: event.FavoriteToggled,
			Data: map[string]any{"user_id": "u-1", "item_id": "ok-event", "is_favorite": true},
		})
	}()

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	var saw string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err == nil {
			saw += line
		}
		if strings.Contains(saw, `"item_id":"ok-event"`) {
			break
		}
	}
	if !strings.Contains(saw, "ok-event") {
		t.Fatalf("own event not seen within 2s; buffer: %q", saw)
	}
	if strings.Contains(saw, "leak-attempt") {
		t.Fatalf("other user's event leaked into stream; buffer: %q", saw)
	}
}

// Nil-Data and missing user_id are defensive checks. A misconfigured
// publisher that forgets to stamp user_id MUST NOT fan out to every
// connected client — the filter rejects the event silently.
func TestMeEvents_DropsEventsWithoutUserID(t *testing.T) {
	bus, srv := newMeEventsTestServer(t, &auth.Claims{UserID: "u-1"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	waitForHandlerCount(t, bus, event.PlayedToggled, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)
		// nil Data, missing user_id, wrong type — all three must drop.
		bus.Publish(event.Event{Type: event.PlayedToggled, Data: nil})
		bus.Publish(event.Event{Type: event.PlayedToggled, Data: map[string]any{"item_id": "no-uid"}})
		bus.Publish(event.Event{Type: event.PlayedToggled, Data: map[string]any{"user_id": 42, "item_id": "wrong-type"}})
		// Then a real one we DO expect to see, so the test has a
		// positive signal to wait for.
		bus.Publish(event.Event{
			Type: event.PlayedToggled,
			Data: map[string]any{"user_id": "u-1", "item_id": "real-event", "played": true},
		})
	}()

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	var saw string
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err == nil {
			saw += line
		}
		if strings.Contains(saw, `"item_id":"real-event"`) {
			break
		}
	}
	if !strings.Contains(saw, "real-event") {
		t.Fatalf("real event not seen; buffer: %q", saw)
	}
	if strings.Contains(saw, "no-uid") || strings.Contains(saw, "wrong-type") {
		t.Fatalf("malformed event leaked: %q", saw)
	}
}

// Disconnect must drop all 3 user-scoped subscriptions (one per
// event type the handler subscribed to). Without the unsubscribe
// loop in the defer, every SSE client would leak 3 handlers per
// connection for the lifetime of the process.
func TestMeEvents_UnsubscribesOnDisconnect(t *testing.T) {
	bus, srv := newMeEventsTestServer(t, &auth.Claims{UserID: "u-1"})

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	waitForHandlerCount(t, bus, event.ProgressUpdated, 1)
	waitForHandlerCount(t, bus, event.PlayedToggled, 1)
	waitForHandlerCount(t, bus, event.FavoriteToggled, 1)

	cancel()
	resp.Body.Close() //nolint:errcheck

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if bus.HandlerCount(event.ProgressUpdated) == 0 &&
			bus.HandlerCount(event.PlayedToggled) == 0 &&
			bus.HandlerCount(event.FavoriteToggled) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("handlers still registered after disconnect: progress=%d played=%d favourite=%d",
		bus.HandlerCount(event.ProgressUpdated),
		bus.HandlerCount(event.PlayedToggled),
		bus.HandlerCount(event.FavoriteToggled))
}
