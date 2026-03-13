package event

import (
	"log/slog"
	"sync"
)

type Type string

// Event types — modules emit these, subscribers react to them.
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

// Bus is an in-process pub/sub event bus.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Type][]Handler
	logger   *slog.Logger
}

func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		handlers: make(map[Type][]Handler),
		logger:   logger.With("module", "eventbus"),
	}
}

// Subscribe registers a handler for the given event type.
func (b *Bus) Subscribe(eventType Type, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

// Publish sends an event to all registered handlers asynchronously.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := b.handlers[e.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Error("event handler panicked", "type", e.Type, "panic", r)
				}
			}()
			handler(e)
		}(h)
	}
}
