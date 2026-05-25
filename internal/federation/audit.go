package federation

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AuditEntry es un registro de peticion peer. Campos opcionales salvo
// PeerID, Method, Endpoint, StatusCode y OccurredAt (la tabla tolera nulls).
type AuditEntry struct {
	PeerID       string
	RemoteUserID string
	Method       string
	Endpoint     string
	StatusCode   int
	BytesOut     int64
	ItemID       string
	SessionID    string
	ErrorKind    string
	DurationMs   int64
	OccurredAt   time.Time
}

// AuditRepo es la interfaz estrecha de DB que el auditor necesita.
type AuditRepo interface {
	InsertAuditEntry(ctx context.Context, entry *AuditEntry) error
}

// auditQueueSize: buffer maximo antes de descartar. 256 entradas ~25 KB.
// Si se llena es porque el rate-limiter ya esta actuando; perder alguna
// entrada durante el pico es aceptable (se loguea el descarte).
const auditQueueSize = 256

// auditFlushInterval: cadencia maxima entre flush. Acota la ventana
// de perdida ante un crash.
const auditFlushInterval = 30 * time.Second

// Auditor registra peticiones peer asincronamente. Record es non-blocking
// (descarta con backpressure). Un goroutine de fondo drena y persiste
// por batch o cada flush interval. Close drena pendientes y espera.
type Auditor struct {
	repo   AuditRepo
	logger *slog.Logger

	queue chan AuditEntry
	done  chan struct{}
	wg    sync.WaitGroup

	mu          sync.Mutex
	lastDropLog time.Time
}

// NewAuditor crea y arranca un Auditor. El caller debe llamar Close
// en shutdown para vaciar la cola.
func NewAuditor(repo AuditRepo, logger *slog.Logger) *Auditor {
	if logger == nil {
		logger = slog.Default()
	}
	a := &Auditor{
		repo:   repo,
		logger: logger.With("module", "federation.audit"),
		queue:  make(chan AuditEntry, auditQueueSize),
		done:   make(chan struct{}),
	}
	a.wg.Add(1)
	go a.run()
	return a
}

// Record encola una entrada para persistencia asincrona. Non-blocking.
// Si la cola esta llena, descarta y loguea max 1 vez cada 5s.
func (a *Auditor) Record(entry AuditEntry) {
	if a == nil {
		return
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now()
	}
	select {
	case a.queue <- entry:
	default:
		a.logDropOnce()
	}
}

// Close para el worker, vacia pendientes y espera. Idempotente.
func (a *Auditor) Close() {
	if a == nil {
		return
	}
	select {
	case <-a.done:
		return
	default:
	}
	close(a.done)
	a.wg.Wait()
}

func (a *Auditor) run() {
	defer a.wg.Done()
	tick := time.NewTicker(auditFlushInterval)
	defer tick.Stop()

	for {
		select {
		case <-a.done:
			a.flush()
			return
		case <-tick.C:
			a.flush()
		case entry := <-a.queue:
			// Drena entradas hermanas que llegaron mientras estabamos
			// bloqueados. Acotado por queueSize.
			batch := []AuditEntry{entry}
			drained := true
			for drained && len(batch) < auditQueueSize {
				select {
				case more := <-a.queue:
					batch = append(batch, more)
				default:
					drained = false
				}
			}
			a.persist(batch)
		}
	}
}

func (a *Auditor) flush() {
	for {
		select {
		case entry := <-a.queue:
			a.persist([]AuditEntry{entry})
		default:
			return
		}
	}
}

// persist escribe el batch fila a fila. Si un insert falla, loguea y
// continua para no perder el resto del batch por un error transitorio.
func (a *Auditor) persist(batch []AuditEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := range batch {
		if err := a.repo.InsertAuditEntry(ctx, &batch[i]); err != nil {
			a.logger.Warn("audit insert failed",
				"err", err,
				"peer_id", batch[i].PeerID,
				"endpoint", batch[i].Endpoint)
		}
	}
}

func (a *Auditor) logDropOnce() {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.Sub(a.lastDropLog) < 5*time.Second {
		return
	}
	a.lastDropLog = now
	a.logger.Warn("audit queue full — dropping entries", "queue_size", auditQueueSize)
}
