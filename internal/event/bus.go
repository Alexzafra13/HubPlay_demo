package event

import (
	"log/slog"
	"sync"
	"time"
)

// slowHandlerThreshold is purely a watchdog for logs. A handler that exceeds
// it gets a warning logged once; the dispatch goroutine still completes when
// the handler returns. Handlers that hang forever leak exactly one goroutine
// each — that is a bug in the handler, not something the bus can recover from.
const slowHandlerThreshold = 30 * time.Second

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
	// ChannelHealthChanged is published when a channel transitions
	// between health buckets (ok / degraded / dead). Emitted only on
	// transition — not on every probe — so subscribers (admin SSE
	// stream) get push notifications without flooding on every tick.
	ChannelHealthChanged Type = "channel.health.changed"
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
//
// Each handler runs in its own goroutine with panic recovery. A separate
// watchdog logs a warning if the handler runs longer than slowHandlerThreshold,
// but never blocks the dispatch goroutine — when the handler returns, both
// goroutines unwind cleanly.
//
// Subscribers are responsible for not blocking inside their handler. The
// in-tree SSE handler does a non-blocking channel send (drops on backpressure);
// any future subscriber must follow the same rule. A handler that hangs
// indefinitely leaks one goroutine per Publish call, and the bus deliberately
// does not try to recover from that — there is no safe way to abort arbitrary
// caller code.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	subs := append([]subscription(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()

	for _, s := range subs {
		go func(handler Handler) {
			done := make(chan struct{})

			// Watchdog. Emits at most one warning per slow handler call and
			// always exits — even if the handler itself hangs forever. No
			// blocking on the dispatch path; no leaked watchdog goroutines.
			go func() {
				timer := time.NewTimer(slowHandlerThreshold)
				defer timer.Stop()
				select {
				case <-done:
				case <-timer.C:
					b.logger.Warn("event handler slow",
						"type", e.Type,
						"threshold", slowHandlerThreshold)
				}
			}()

			defer close(done)
			defer func() {
				if r := recover(); r != nil {
					b.logger.Error("event handler panicked",
						"type", e.Type, "panic", r)
				}
			}()
			handler(e)
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
