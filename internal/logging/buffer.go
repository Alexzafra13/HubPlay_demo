package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry is the wire-shape we hand to the admin logs surface. Sized
// for tail-style UIs: timestamp + level + free-form message + a
// small map of structured attributes. We deliberately don't surface
// source-file info (slog can produce it) because the admin log
// viewer is for ops debugging at a glance, not stack-trace forensics
// — that's what the JSON stream to stdout is for.
type Entry struct {
	Time    time.Time      `json:"ts"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Buffer is a slog.Handler that keeps the last `capacity` log
// entries in a ring AND fans new entries out to subscribed
// channels. Wraps a delegate handler (typically the JSON/text
// handler that already writes to stdout) so it's a drop-in: the
// existing log destination keeps working, the buffer is purely
// additive.
//
// Concurrency: writes lock; reads (Snapshot) take a read lock and
// copy. Subscribers receive on a buffered channel — slow readers
// drop entries past their channel buffer rather than blocking the
// log path, since blocking inside Handle would stall every goroutine
// that logs. The choice to drop-on-slow over block matches Linux's
// `dmesg` behaviour: an admin watching logs on a laggy connection
// shouldn't be able to deadlock the server's request handlers.
type Buffer struct {
	delegate slog.Handler
	capacity int

	mu      sync.RWMutex
	entries []Entry  // ring; len ≤ capacity
	head    int      // next write index when entries is full
	full    bool

	subsMu sync.Mutex
	subs   map[int]chan Entry
	nextID int
}

// NewBuffer wraps `delegate` so every record is forwarded to it
// AND captured in the ring. Capacity is the max number of entries
// retained — past that the ring overwrites oldest. Set the cap
// generous enough to debug a single incident (a few hundred) but
// small enough that the per-process memory cost stays trivial
// (each entry < 1 KB, 500 = ~500 KB worst case).
func NewBuffer(delegate slog.Handler, capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &Buffer{
		delegate: delegate,
		capacity: capacity,
		entries:  make([]Entry, 0, capacity),
		subs:     make(map[int]chan Entry),
	}
}

// Enabled defers to the delegate so the same level filter applies
// to both the stdout destination and the ring. Without this, the
// admin viewer would show entries the rest of the system threw away.
func (b *Buffer) Enabled(ctx context.Context, level slog.Level) bool {
	return b.delegate.Enabled(ctx, level)
}

// Handle records the entry into the ring, fans it out to
// subscribers, then forwards to the delegate. Order matters: the
// delegate may block briefly on stdout flush, but the ring write
// has to happen first so a Snapshot taken concurrently sees this
// entry before subsequent ones rather than after.
func (b *Buffer) Handle(ctx context.Context, r slog.Record) error {
	entry := Entry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
	}
	if r.NumAttrs() > 0 {
		entry.Attrs = make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			entry.Attrs[a.Key] = a.Value.Any()
			return true
		})
	}

	b.mu.Lock()
	if !b.full {
		b.entries = append(b.entries, entry)
		if len(b.entries) == b.capacity {
			b.full = true
			b.head = 0
		}
	} else {
		b.entries[b.head] = entry
		b.head = (b.head + 1) % b.capacity
	}
	b.mu.Unlock()

	b.fanout(entry)

	return b.delegate.Handle(ctx, r)
}

// WithAttrs / WithGroup just decorate the delegate. The ring
// captures what Handle sees, which already includes the prebound
// attrs — no need to track them separately.
func (b *Buffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Buffer{
		delegate: b.delegate.WithAttrs(attrs),
		capacity: b.capacity,
		entries:  b.entries, // shared ring; this is the "child" handler for With()
		// subs map is shared via pointer; re-pointing here would split
		// fan-out between parents and children. We deliberately don't.
		subs: b.subs,
	}
}

func (b *Buffer) WithGroup(name string) slog.Handler {
	return &Buffer{
		delegate: b.delegate.WithGroup(name),
		capacity: b.capacity,
		entries:  b.entries,
		subs:     b.subs,
	}
}

// Snapshot returns the most-recent entries up to `limit`, oldest
// first. Cheap (one RLock + a copy) so the SSE handler's initial
// payload doesn't need a separate code path.
func (b *Buffer) Snapshot(limit int) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var ordered []Entry
	if !b.full {
		ordered = make([]Entry, len(b.entries))
		copy(ordered, b.entries)
	} else {
		ordered = make([]Entry, 0, b.capacity)
		ordered = append(ordered, b.entries[b.head:]...)
		ordered = append(ordered, b.entries[:b.head]...)
	}
	if limit > 0 && len(ordered) > limit {
		ordered = ordered[len(ordered)-limit:]
	}
	return ordered
}

// Subscribe returns a channel that receives every new entry until
// the returned cancel is called. Buffered (16) so a momentarily
// laggy reader doesn't drop the next dozen entries; past that, the
// fan-out drops rather than blocks (see Buffer doc).
func (b *Buffer) Subscribe() (<-chan Entry, func()) {
	b.subsMu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan Entry, 16)
	b.subs[id] = ch
	b.subsMu.Unlock()

	return ch, func() {
		b.subsMu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing)
		}
		b.subsMu.Unlock()
	}
}

// fanout pushes the entry to every subscriber. Non-blocking:
// subscribers whose channel is full drop the entry on the floor.
// Trade-off documented at the type level — never block a logger.
func (b *Buffer) fanout(e Entry) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// Slow consumer; skip rather than stall the logger.
		}
	}
}
