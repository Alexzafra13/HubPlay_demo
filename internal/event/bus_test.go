package event

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestBus_PublishSubscribe(t *testing.T) {
	bus := NewBus(slog.Default())

	var received Event
	var mu sync.Mutex
	done := make(chan struct{})

	bus.Subscribe(ItemAdded, func(e Event) {
		mu.Lock()
		received = e
		mu.Unlock()
		close(done)
	})

	bus.Publish(Event{
		Type: ItemAdded,
		Data: map[string]any{"id": "123", "title": "Test Movie"},
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler not called within 1s")
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Type != ItemAdded {
		t.Errorf("expected type %s, got %s", ItemAdded, received.Type)
	}
	if received.Data["id"] != "123" {
		t.Errorf("expected id '123', got %v", received.Data["id"])
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	bus := NewBus(slog.Default())

	var count int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		bus.Subscribe(ItemAdded, func(e Event) {
			mu.Lock()
			count++
			mu.Unlock()
			wg.Done()
		})
	}

	bus.Publish(Event{Type: ItemAdded})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not all handlers called within 1s")
	}

	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Errorf("expected 3 handlers called, got %d", count)
	}
}

func TestBus_NoSubscribers(t *testing.T) {
	bus := NewBus(slog.Default())
	// Should not panic
	bus.Publish(Event{Type: ItemRemoved, Data: map[string]any{"id": "456"}})
}

func TestBus_PanicRecovery(t *testing.T) {
	bus := NewBus(slog.Default())

	done := make(chan struct{})

	// First handler panics
	bus.Subscribe(ItemAdded, func(e Event) {
		panic("test panic")
	})

	// Second handler should still run
	bus.Subscribe(ItemAdded, func(e Event) {
		close(done)
	})

	bus.Publish(Event{Type: ItemAdded})

	select {
	case <-done:
		// Success — second handler ran despite first panicking
	case <-time.After(time.Second):
		t.Fatal("second handler not called after first panicked")
	}
}

func TestBus_Unsubscribe_RemovesHandler(t *testing.T) {
	bus := NewBus(slog.Default())

	var called int
	var mu sync.Mutex
	unsub := bus.Subscribe(ItemAdded, func(e Event) {
		mu.Lock()
		called++
		mu.Unlock()
	})

	// One publish while subscribed → handler runs.
	bus.Publish(Event{Type: ItemAdded})
	time.Sleep(50 * time.Millisecond)

	// Unsubscribe, then publish again → handler must NOT run.
	unsub()
	if bus.HandlerCount(ItemAdded) != 0 {
		t.Fatalf("handler not removed: count=%d", bus.HandlerCount(ItemAdded))
	}
	bus.Publish(Event{Type: ItemAdded})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called != 1 {
		t.Errorf("handler called %d times after unsubscribe; want 1", called)
	}
}

func TestBus_Unsubscribe_IsIdempotent(t *testing.T) {
	bus := NewBus(slog.Default())
	unsub := bus.Subscribe(ItemAdded, func(e Event) {})

	unsub()
	unsub() // must not panic or remove anything else

	if bus.HandlerCount(ItemAdded) != 0 {
		t.Fatalf("count: %d", bus.HandlerCount(ItemAdded))
	}
}

func TestBus_Unsubscribe_DoesNotAffectOtherSubscribers(t *testing.T) {
	bus := NewBus(slog.Default())

	var aCalled, bCalled bool
	var mu sync.Mutex
	unsubA := bus.Subscribe(ItemAdded, func(e Event) {
		mu.Lock()
		aCalled = true
		mu.Unlock()
	})
	bus.Subscribe(ItemAdded, func(e Event) {
		mu.Lock()
		bCalled = true
		mu.Unlock()
	})

	unsubA()
	bus.Publish(Event{Type: ItemAdded})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if aCalled {
		t.Error("A was unsubscribed but still called")
	}
	if !bCalled {
		t.Error("B should still be called")
	}
}

func TestBus_DifferentEventTypes(t *testing.T) {
	bus := NewBus(slog.Default())

	var called bool
	var mu sync.Mutex

	bus.Subscribe(ItemAdded, func(e Event) {
		mu.Lock()
		called = true
		mu.Unlock()
	})

	// Publish a different event type
	bus.Publish(Event{Type: ItemRemoved})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Error("handler should not be called for different event type")
	}
}
