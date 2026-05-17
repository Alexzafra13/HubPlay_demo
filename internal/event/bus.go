package event

import (
	"log/slog"
	"sync"
	"time"
)

// slowHandlerThreshold: si un handler tarda más, se loguea aviso. Sólo es
// un chivato; no se cancela nada.
const slowHandlerThreshold = 30 * time.Second

type Type string

// Tipos de eventos del backend. El paréntesis indica el productor (para
// poder grepar el Publish). Añadir tipos nuevos no rompe nada — si nadie
// está suscrito, Publish no hace nada.
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

	// DeviceCodeApproved se emite cuando el admin aprueba un dispositivo
	// nuevo. La pantalla de emparejado (QR + código) reacciona al instante
	// en vez de tener que estar preguntando cada poco.
	DeviceCodeApproved Type = "device_code.approved"

	// ── Detección de segmentos (skip-intro / skip-credits). El panel admin
	// muestra una barra de progreso igual que con los escaneos de biblioteca.
	SegmentDetectStarted   Type = "library.segments.started"
	SegmentDetectProgress  Type = "library.segments.progress"
	SegmentDetectCompleted Type = "library.segments.completed"

	// ── Cambios en el estado de "visto" de un usuario. Sirven para que los
	// otros dispositivos del MISMO usuario reflejen el cambio sin tener que
	// estar consultando. Hay tres tipos en vez de uno único porque el
	// frontend refresca pantallas distintas según qué cambió.
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

// Subscribe devuelve una función para darse de baja. Hay que llamarla
// cuando el suscriptor desaparece, o el handler se queda colgado para
// siempre y cada Publish ejecuta código muerto.
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

// Publish entrega el evento a todos los handlers en paralelo. Cada handler
// va en su propia goroutine, con recuperación de panics.
//
// Regla para quien escriba un handler: no bloquear nunca dentro. Si el
// handler se cuelga, el bus no puede hacer nada por él.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	subs := append([]subscription(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()

	for _, s := range subs {
		go func(handler Handler) {
			done := make(chan struct{})

			// Si el handler tarda más de la cuenta, loguea aviso; siempre
			// termina, aunque el handler se quede colgado.
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

// HandlerCount es sólo para tests y diagnóstico.
func (b *Bus) HandlerCount(eventType Type) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.handlers[eventType])
}
