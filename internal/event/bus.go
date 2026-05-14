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
// Tipos de eventos publicados por el backend. Cada productor está
// listado entre paréntesis para localizar el `Publish` real con un
// `grep`. `Publish` es no-op cuando nadie está suscrito, así que
// añadir un tipo nuevo no rompe a nadie aunque tarde en cablearse
// un subscriber.
const (
	LibraryScanStarted   Type = "library.scan.started"   // scanner
	LibraryScanCompleted Type = "library.scan.completed" // scanner
	// LibraryScanProgress se emite cada ~50 ficheros durante el
	// walk. Data: library_id, library_name, scanned (count parcial),
	// current_path. No hay total porque no pre-walk-eamos para
	// contar — el UI muestra "scanned N" + spinner, no porcentaje.
	LibraryScanProgress  Type = "library.scan.progress" // scanner
	ItemAdded            Type = "item.added"            // scanner
	ItemUpdated          Type = "item.updated"          // scanner
	ItemRemoved          Type = "item.removed"          // scanner
	MetadataUpdated      Type = "metadata.updated"      // library (segment detection)
	TranscodeStarted     Type = "transcode.started"     // stream.Manager
	TranscodeCompleted   Type = "transcode.completed"   // stream.Manager
	ChannelAdded         Type = "channel.added"         // iptv (M3U import)
	ChannelRemoved       Type = "channel.removed"       // iptv (M3U import)
	EPGUpdated           Type = "epg.updated"           // iptv (EPG refresh)
	PlaylistRefreshed    Type = "playlist.refreshed"    // iptv (M3U import)
	// PlaylistRefreshFailed se emite cuando un import M3U async se
	// rinde. El handler ya respondió 202, así que la SSE es la
	// única señal de fallo que ve el admin UI — sin esto el
	// spinner queda colgado.
	PlaylistRefreshFailed Type = "playlist.refresh_failed" // iptv
	// ChannelHealthChanged se emite cuando un canal transita entre
	// buckets de salud (ok / degraded / dead). Solo en transición,
	// no en cada probe — evita flood en el SSE del admin.
	ChannelHealthChanged Type = "channel.health.changed" // iptv
	UserLoggedIn         Type = "user.logged_in"
	UserLoggedOut        Type = "user.logged_out"

	// DeviceCodeApproved is published by DeviceCodeService.ApproveDevice
	// when the operator binds their identity to a pending device-code
	// row. The auth/device events SSE stream listens for this so the
	// browser-side pairing UI (showing the QR + user_code) can react
	// instantly instead of polling the RFC 8628 /poll endpoint.
	//
	// Data shape:
	//   device_code — string, the opaque token only the device + server
	//                 know. Used by the SSE handler to filter to "this
	//                 client only" before fan-out.
	//   user_id     — string, who approved (informational; the SSE
	//                 stream does not relay it to the client).
	DeviceCodeApproved Type = "device_code.approved"

	// ── Segment detection (skip-intro / skip-credits).
	// Published by the segment detector as it walks a library's
	// episodes deriving intro/outro/recap markers from chapter
	// titles. Subscribers (the admin SSE stream) surface a small
	// progress banner the same way library.scan.* does.
	//
	// Data shape:
	//   library_id   — string
	//   library_name — string (Started/Completed only)
	//   scanned      — int (Progress only; how many episodes inspected)
	//   detected     — int (Progress/Completed; how many segments written)
	SegmentDetectStarted   Type = "library.segments.started"
	SegmentDetectProgress  Type = "library.segments.progress"
	SegmentDetectCompleted Type = "library.segments.completed"

	// ── User watch state — published by ProgressHandler so other
	// devices owned by the SAME user can sync their UI without polling.
	// `Data` carries:
	//
	//   user_id        — string, MUST be filtered against the authed
	//                    user before fan-out (the per-user SSE
	//                    endpoint does this).
	//   item_id        — string.
	//   position_ticks — int64 (ProgressUpdated only). Backend ticks =
	//                    seconds × 10_000_000.
	//   completed      — bool (ProgressUpdated, PlayedToggled).
	//   played         — bool (PlayedToggled — true on mark-played,
	//                    false on mark-unplayed).
	//   is_favorite    — bool (FavoriteToggled).
	//
	// Why three types instead of one "user_data.changed": the frontend
	// invalidates DIFFERENT queries depending on which thing changed
	// (progress hits Continue Watching; played hits Up Next +
	// Continue Watching; favorite hits Favorites). Splitting at the
	// type level lets each subscriber listen only for what it cares
	// about and keeps the wire payload small.
	ProgressUpdated  Type = "user.progress.updated"
	PlayedToggled    Type = "user.played.toggled"
	FavoriteToggled  Type = "user.favorite.toggled"
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
