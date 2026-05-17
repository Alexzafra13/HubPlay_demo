package event

import (
	"log/slog"
	"sync"
	"time"
)

// slowHandlerThreshold: watchdog sólo para log — el dispatch sigue cuando el
// handler retorna. Un handler colgado fuga 1 goroutine, y el bus no puede
// recuperarse de eso (sería matar código arbitrario de terceros).
const slowHandlerThreshold = 30 * time.Second

type Type string

// Tipos de eventos publicados por el backend. Cada productor está en
// paréntesis para localizar el Publish con grep. Publish es no-op si nadie
// está suscrito, así que añadir un tipo nuevo no rompe nada.
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

	// DeviceCodeApproved: lo publica DeviceCodeService.ApproveDevice al
	// vincular operador → device-code pendiente. Lo escucha el SSE auth/device
	// para que el UI del pairing (QR + user_code) reaccione al instante en vez
	// de polear /poll (RFC 8628).
	//
	// Data: device_code (string, token opaco — el SSE filtra por este antes de
	// fan-out), user_id (informativo; no se reenvía al cliente).
	DeviceCodeApproved Type = "device_code.approved"

	// ── Detección de segmentos (skip-intro / skip-credits). Las publica el
	// detector al recorrer episodios derivando markers de títulos de capítulos.
	// El SSE admin las usa para banner de progreso, igual que library.scan.*.
	//
	// Data: library_id, library_name (Started/Completed), scanned (Progress),
	// detected (Progress/Completed).
	SegmentDetectStarted   Type = "library.segments.started"
	SegmentDetectProgress  Type = "library.segments.progress"
	SegmentDetectCompleted Type = "library.segments.completed"

	// ── User watch state: publicado por ProgressHandler para que OTROS
	// dispositivos del MISMO user sincronicen UI sin polear.
	//
	// Data: user_id (filtrar contra el authed user antes de fan-out — lo hace
	// el SSE per-user), item_id, position_ticks (ProgressUpdated; ticks =
	// segundos × 10_000_000), completed (Progress/Played), played (Played —
	// true=mark, false=unmark), is_favorite (Favorite).
	//
	// 3 tipos en vez de uno único "user_data.changed" porque el frontend
	// invalida queries distintas según qué cambió (progress→Continue Watching,
	// played→Up Next + CW, favorite→Favorites). Separar por tipo permite a
	// cada subscriber escuchar sólo lo suyo y mantiene el payload mínimo.
	ProgressUpdated  Type = "user.progress.updated"
	PlayedToggled    Type = "user.played.toggled"
	FavoriteToggled  Type = "user.favorite.toggled"
)

type Event struct {
	Type Type
	Data map[string]any
}

type Handler func(Event)

type subscription struct {
	id uint64
	fn Handler
}

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

// Subscribe: devuelve func de unsub. Hay que LLAMARLA cuando el subscriber
// se va (p.ej. cliente SSE desconecta), o el handler fuga y cada Publish
// futuro corre un closure muerto. La func es idempotente y goroutine-safe.
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

// Publish: dispatch async. Cada handler en su goroutine con panic recovery,
// más un watchdog que loguea (no bloquea) si supera slowHandlerThreshold.
//
// Contrato del subscriber: NO bloquear dentro del handler. El SSE in-tree
// hace send no-bloqueante (drop on backpressure); cualquier subscriber nuevo
// debe seguir la misma regla. Handler colgado = 1 goroutine fugada por
// Publish — el bus no intenta recuperarse (no hay forma segura de abortar
// código arbitrario del caller).
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	subs := append([]subscription(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()

	for _, s := range subs {
		go func(handler Handler) {
			done := make(chan struct{})

			// Watchdog: máx. 1 warning por handler lento, y siempre sale —
			// incluso si el handler cuelga. Cero bloqueo en el dispatch.
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

// HandlerCount: para tests y diagnóstico — no es parte del contrato runtime.
func (b *Bus) HandlerCount(eventType Type) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers[eventType])
}
