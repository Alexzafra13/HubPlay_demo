package federation

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// AuditEntry is a single peer-request record. Populated by the
// middleware after the response is written and queued for asynchronous
// persistence. Every field except PeerID, Method, Endpoint, StatusCode,
// and OccurredAt is optional — the audit table tolerates nulls so we
// don't lie when a request didn't touch a particular dimension
// (e.g. a ping has no item_id).
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

// AuditRepo is the slice of database operations the auditor needs.
// Kept narrow so the auditor is testable with an in-memory fake.
type AuditRepo interface {
	InsertAuditEntry(ctx context.Context, entry *AuditEntry) error
}

// auditQueueSize is the maximum number of pending audit entries the
// auditor buffers before it starts dropping. Sized for a small
// home-scale deployment: 256 entries × ~100 bytes ≈ 25 KB on the heap.
// A federation that's hammering us hard enough to fill this is already
// being rate-limited by the token bucket; the auditor dropping a few
// records during the spike is tolerable as long as the drop itself is
// logged loudly.
const auditQueueSize = 256

// auditFlushInterval is the maximum time between batch writes. Even
// at very low traffic the worker wakes up at this cadence to flush
// any pending entries — bounds the loss window in case of a crash.
const auditFlushInterval = 30 * time.Second

// Auditor records federation peer requests asynchronously. The hot
// path (Record) is non-blocking: it does a non-blocking channel send
// and drops on backpressure, logging once per drop so an operator
// never silently loses audit visibility.
//
// A background goroutine drains the channel and persists entries
// either when a batch fills (32 entries) or every flush interval.
//
// Lifecycle: NewAuditor returns an auditor that's already running.
// Close stops the worker, drains pending entries, and waits for the
// last write to settle.
type Auditor struct {
	repo   AuditRepo
	logger *slog.Logger

	queue chan AuditEntry
	done  chan struct{}
	wg    sync.WaitGroup

	mu          sync.Mutex
	lastDropLog time.Time
}

// NewAuditor wires + starts an Auditor. The caller must Close it on
// shutdown to flush the in-flight queue.
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

// Record enqueues an entry for asynchronous persistence. Non-blocking;
// returns immediately. On a full queue the entry is dropped and the
// drop is logged at most once every 5 seconds (so a sustained burst
// doesn't flood the operator's log).
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

// Close stops the worker, flushes pending entries, and waits for
// completion. Safe to call multiple times — subsequent calls are
// no-ops because the channel is already closed.
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
			// Drain any sibling entries that arrived while we were
			// blocked, batch them in this writer iteration. Bounded
			// by queueSize so this loop can't run away.
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

// persist writes a batch one row at a time. This is intentionally
// simple — the hot path (Record) is wait-free; persistence is a
// background concern. If a single insert errors we log and continue
// so a transient SQLite hiccup doesn't lose the rest of the batch.
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
