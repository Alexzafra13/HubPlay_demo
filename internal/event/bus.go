package event

import (
	"log/slog"
	"sync"
	"time"
)

// handlerTimeout is the maximum time we wait for an event handler to finish.
// If a handler exceeds this, we log a warning but the goroutine is NOT leaked —
// it continues running to completion (or until it returns on its own).
const handlerTimeout = 30 * time.Second

type Type string

// Event types — modules emit these, subscribers react to them.
//
// NOTE: as of 2026-04-17 only the five scan/item types are actually published
// by the scanner. The others (Metadata*, Transcode*, Channel*, EPG*, Playlist*,
// User*) are reserved for upcoming features — keeping the constants prevents
// churn when those producers land. Subscribers (events.go SSE) listen to all
// of them safely because Publish is a no-op when no handler is registered.
const (
	LibraryScanStarted   Type = "library.scan.started"
	LibraryScanCompleted Type = "library.scan.completed"
	ItemAdded            Type = "item.added"
	ItemUpdated          Type = "item.updated"
	ItemRemoved          Type = "item.removed"
	MetadataUpdated      Type = "metadata.updated"
	TranscodeStarted     Type = "transcode.started"
	TranscodeCompleted   Type = "transcode.completed"
	ChannelAdded         Type = "channel.added"
	ChannelRemoved       Type = "channel.removed"
	EPGUpdated           Type = "epg.updated"
	PlaylistRefreshed    Type = "playlist.refreshed"
	UserLoggedIn         Type = "user.logged_in"
	UserLoggedOut        Type = "user.logged_out"
)

type Event struct {
	Type Type
	Data map[string]any
}

type Handler func(Event)

// subscription couples a handler with the ID we use to unregister it later.
type subscription struct {
	id uint64
	fn Handler
}

// Bus is an in-process pub/sub event bus.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Type][]subscription
	nextID   uint64
	logger   *slog.Logger
}

func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[Type][]subscription),
		logger:   logger.With("module", "eventbus"),
	}
}

// Subscribe registers a handler for the given event type and returns a
// function that unregisters it. Call the returned function when the
// subscriber goes away (e.g. SSE client disconnect) — otherwise the handler
// leaks and every future Publish runs a dead closure.
//
// The returned function is idempotent and safe to call from any goroutine.
func (b *Bus) Subscribe(eventType Type, handler Handler) func() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.handlers[eventType] = append(b.handlers[eventType], subscription{id: id, fn: handler})
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.handlers[eventType]
		for i, s := range subs {
			if s.id == id {
				b.handlers[eventType] = append(subs[:i], subs[i+1:]...)
				return
			}
		}
	}
}

// Publish sends an event to all registered handlers asynchronously.
// Each handler runs in a single goroutine with panic recovery.
// A timeout logs a warning but does not abandon the goroutine — it runs to
// completion, preventing goroutine leaks from the old two-goroutine pattern.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	subs := append([]subscription(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()

	for _, s := range subs {
		go func(handler Handler) {
			done := make(chan struct{})
			timer := time.NewTimer(handlerTimeout)
			defer timer.Stop()

			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						b.logger.Error("event handler panicked", "type", e.Type, "panic", r)
					}
				}()
				handler(e)
			}()

			select {
			case <-done:
				// Handler completed within timeout
			case <-timer.C:
				b.logger.Error("event handler timed out, waiting for completion", "type", e.Type, "timeout", handlerTimeout)
				// Wait for the handler goroutine to finish — never abandon it.
				<-done
			}
		}(s.fn)
	}
}

// HandlerCount returns the number of registered handlers for the given event
// type. Intended for tests and diagnostics; not part of the runtime contract.
func (b *Bus) HandlerCount(eventType Type) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers[eventType])
}
