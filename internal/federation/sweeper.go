package federation

import (
	"context"
	"log/slog"
	"time"
)

// StartPendingRequestSweeper lanza una goroutine que cada `interval`
// invoca SweepExpiredPairingRequests. Las peticiones con
// expires_at < ahora pasan a estado 'expired' y dejan de contar
// para el cap defensivo + el badge admin.
//
// Mismo patron que internal/db.StartPeriodicOptimize:
//   - El ticker arranca DESPUES del primer interval (no inmediato);
//     no malgastamos cpu al boot.
//   - El job vive hasta que ctx termina O hasta que el caller invoca
//     el closure de cancel devuelto.
//   - Errores se loguean - no detienen el sweeper.
//
// Llamado en composition root tras NewManager; el defer del cancel
// asegura cleanup en graceful shutdown.
func StartPendingRequestSweeper(ctx context.Context, mgr *Manager, logger *slog.Logger, interval time.Duration) func() {
	if mgr == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = time.Hour
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n, err := mgr.SweepExpiredPairingRequests(ctx)
				if err != nil {
					logger.Warn("federation: pending request sweeper", "err", err)
					continue
				}
				if n > 0 {
					logger.Info("federation: expired pending pairing requests", "count", n)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
