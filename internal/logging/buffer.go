package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Entry es lo que ve el panel admin "Logs". No incluye el fichero de
// origen porque el panel es para echar un vistazo rápido, no para hacer
// forense — para eso está el log JSON de stdout.
type Entry struct {
	Time    time.Time      `json:"ts"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Buffer guarda las últimas N entradas de log en un anillo y las reparte
// a quien esté suscrito. Envuelve el handler real (JSON o texto a stdout)
// sin reemplazarlo — sigue funcionando todo igual, esto es un añadido.
//
// Si un suscriptor va lento, las entradas se le caen al suelo en vez de
// bloquear; bloquear el logger congelaría toda la aplicación.
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

// NewBuffer: cada log va al destino real Y al anillo. Capacity es el
// número máximo de entradas guardadas; pasado eso se sobreescribe la
// más antigua. 500 entradas ≈ 500 KB, suficiente para una incidencia.
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

// Enabled delega en el destino real para que el filtro de nivel sea el
// mismo en stdout y en el panel.
func (b *Buffer) Enabled(ctx context.Context, level slog.Level) bool {
	return b.delegate.Enabled(ctx, level)
}

// Handle guarda primero en el anillo, después reparte a los suscriptores
// y por último escribe en stdout. El orden importa para que un Snapshot
// concurrente vea las entradas en el orden real, no al revés.
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

// WithAttrs / WithGroup sólo decoran el destino real. El anillo se
// comparte entre el handler padre y los hijos a propósito; si los
// separásemos, los suscriptores no verían los logs del hijo.
func (b *Buffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Buffer{
		delegate: b.delegate.WithAttrs(attrs),
		capacity: b.capacity,
		entries:  b.entries,
		subs:     b.subs,
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

// Snapshot devuelve hasta `limit` entradas, las más antiguas primero.
// Es barato; se usa para mandar el bloque inicial cuando se abre el panel.
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

// Subscribe devuelve un canal con buffer pequeño que absorbe tirones
// puntuales. Si el lector va más lento, las entradas siguientes se
// pierden en vez de bloquear el logger.
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

// fanout reparte la entrada sin bloquear; los suscriptores con el canal
// lleno la pierden, pero el logger nunca se para.
func (b *Buffer) fanout(e Entry) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
			// suscriptor lento, saltamos.
		}
	}
}
