package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry: forma wire para el panel admin "Logs". Sin source-file (que slog
// sí puede emitir): el viewer es para debug a golpe de vista, no forense de
// stack trace — para eso está el stream JSON a stdout.
type Entry struct {
	Time    time.Time      `json:"ts"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Buffer: slog.Handler que guarda las últimas `capacity` entradas en un ring
// y hace fan-out a subscribers. Envuelve un delegate (típicamente el JSON/text
// que escribe a stdout) — drop-in puramente aditivo.
//
// Concurrencia: writes con lock; Snapshot con read lock + copia. Subscribers
// reciben por canal buffered — lector lento dropea entradas pasado su buffer
// en vez de bloquear (bloquear en Handle congelaría TODO el que loguea).
// Mismo trade-off que `dmesg` en Linux: un admin con conexión laggy no debe
// poder deadlock-ear los request handlers.
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

// NewBuffer: cada record va al delegate Y al ring. Capacity ≈ entradas
// retenidas; pasado eso, sobreescribe el más viejo. ~1 KB/entry → 500=500 KB
// worst case, lo justo para una incidencia sin inflar el proceso.
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

// Enabled: delega para que el filtro de nivel aplique igual a stdout y al ring
// (si no, el viewer mostraría entradas que el resto del sistema descartó).
func (b *Buffer) Enabled(ctx context.Context, level slog.Level) bool {
	return b.delegate.Enabled(ctx, level)
}

// Handle: ring → fan-out → delegate. El orden importa: el delegate puede
// bloquear brevemente en flush a stdout, pero el ring write tiene que ir
// primero para que un Snapshot concurrente vea esta entrada antes que las
// siguientes, no después.
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

// WithAttrs / WithGroup: decoran sólo el delegate. El ring captura lo que ve
// Handle, que ya trae los prebound attrs — no hace falta trackearlos aparte.
func (b *Buffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Buffer{
		delegate: b.delegate.WithAttrs(attrs),
		capacity: b.capacity,
		entries:  b.entries, // ring compartido — este es el handler "hijo" de With()
		// subs compartido por puntero a propósito: separarlo dividiría el
		// fan-out entre padres e hijos.
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

// Snapshot: hasta `limit` entradas, las más viejas primero. Barato (1 RLock +
// copia) — el SSE lo usa para el payload inicial sin código adicional.
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

// Subscribe: canal con buffer 16 (absorbe lag puntual). Pasado eso, el
// fan-out dropea en vez de bloquear (ver doc del tipo Buffer).
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

// fanout: no-bloqueante — subscriber con canal lleno pierde la entrada.
// Trade-off documentado en el tipo Buffer: nunca bloquear un logger.
func (b *Buffer) fanout(e Entry) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// consumidor lento; saltar antes que parar el logger.
		}
	}
}
